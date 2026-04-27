// Command dpty is a thin CLI built on the kjh.dev/dpty library.
//
// Subcommands:
//
//	dpty broker                       - run the broker
//	dpty serve [-shell] [-arg ...]    - run a PTY server (registers with broker)
//	dpty list  [servers|sessions]     - list state via the broker (default: sessions)
//	dpty create [-name N] [-server ID] [-shell ...] [-arg ...] [-env ...]
//	                                  - create a new PTY through the broker
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"kjh.dev/dpty"
)

const (
	defaultBrokerPort = 5127
	defaultServerPort = 5137
)

func main() {
	if len(os.Args) < 2 {
		usageAndExit(1)
	}
	switch os.Args[1] {
	case "broker":
		os.Exit(cmdBroker(os.Args[2:]))
	case "serve":
		os.Exit(cmdServe(os.Args[2:]))
	case "list":
		os.Exit(cmdList(os.Args[2:]))
	case "create":
		os.Exit(cmdCreate(os.Args[2:]))
	case "-h", "--help", "help":
		usageAndExit(0)
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: %s\n\n", os.Args[1])
		usageAndExit(1)
	}
}

func usageAndExit(code int) {
	fmt.Fprintln(os.Stderr, "Usage: dpty {broker|serve|list|create} [options...]")
	os.Exit(code)
}

// ---- broker ----

func cmdBroker(args []string) int {
	fs := flag.NewFlagSet("broker", flag.ExitOnError)
	port := fs.Int("port", defaultBrokerPort, "Broker listen port")
	stateDir := fs.String("state-dir", "", "Override state directory (default: $HOME/.config/dpty/broker)")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	b := dpty.NewBroker(dpty.BrokerConfig{
		Addr:     ":" + strconv.Itoa(*port),
		StateDir: *stateDir,
	})

	ctx, cancel := signalContext()
	defer cancel()

	if err := b.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "broker: %v\n", err)
		return 1
	}
	return 0
}

// ---- serve ----

func cmdServe(args []string) int {
	var shellArgs stringSliceFlag
	var envs stringSliceFlag

	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", defaultServerPort, "Server listen port")
	id := fs.String("id", "", "Server ID (default: hostname:port)")
	shell := fs.String("shell", "/bin/bash", "Default shell when /pty omits Shell")
	brokerURL := fs.String("broker", "http://localhost:"+strconv.Itoa(defaultBrokerPort), "Broker URL to register with")
	advertise := fs.String("advertise", "", "URL the broker should use to reach this server (default: http://localhost:port)")
	fs.Var(&shellArgs, "arg", "Default arg appended when /pty omits Args (repeatable)")
	fs.Var(&envs, "env", "Default env (KEY=VALUE) appended when /pty omits Env (repeatable)")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	srv := dpty.NewServer(dpty.ServerConfig{
		Addr:         ":" + strconv.Itoa(*port),
		ID:           *id,
		AdvertiseURL: *advertise,
		DefaultShell: *shell,
		DefaultArgs:  []string(shellArgs),
		DefaultEnv:   []string(envs),
	})

	ctx, cancel := signalContext()
	defer cancel()

	go func() {
		// Best-effort registration; the broker may not be up yet.
		_ = srv.RegisterWith(ctx, *brokerURL)
	}()

	if err := srv.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		return 1
	}
	return 0
}

// ---- list ----

func cmdList(args []string) int {
	target := "sessions"
	if len(args) > 0 {
		target = args[0]
	}
	c := dpty.NewClient("http://localhost:" + strconv.Itoa(defaultBrokerPort))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch target {
	case "servers":
		servers, err := c.ListServers(ctx)
		if err != nil {
			fmt.Printf("Error querying broker: %v\n", err)
			return 1
		}
		if len(servers) == 0 {
			fmt.Println("No registered servers.")
			return 0
		}
		fmt.Printf("%-32s  %-12s  %5s\n", "ID", "STATUS", "LOAD")
		for _, s := range servers {
			fmt.Printf("%-32s  %-12s  %5d\n", s.ID, s.Status, s.Load)
		}
	case "sessions":
		sess, err := c.ListSessions(ctx)
		if err != nil {
			fmt.Printf("Error querying broker: %v\n", err)
			return 1
		}
		if len(sess) == 0 {
			fmt.Println("No active sessions.")
			return 0
		}
		fmt.Printf("%-60s  %-12s  %s\n", "URL", "CMD", "CREATED")
		for _, s := range sess {
			wsURL := dpty.AttachWebSocketURL(s.ServerAddress, s.Alias)
			fmt.Printf("%-60s  %-12s  %s\n", wsURL, s.Shell, s.CreatedAt.Format(time.RFC3339))
		}
	default:
		fmt.Printf("Unknown list target: %s (expected 'servers' or 'sessions')\n", target)
		return 1
	}
	return 0
}

// ---- create ----

func cmdCreate(args []string) int {
	fs := flag.NewFlagSet("create", flag.ExitOnError)

	var shellArgs stringSliceFlag
	var envs stringSliceFlag

	serverID := fs.String("server", "", "Server ID to create on (default: pick lowest-load AVAILABLE)")
	name := fs.String("name", "", "Optional session name; must be unique on the chosen server")
	shell := fs.String("shell", "/bin/bash", "Shell to run inside PTY")
	fs.Var(&shellArgs, "arg", "Argument to pass to shell (repeatable)")
	fs.Var(&envs, "env", "KEY=VALUE env entry (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	c := dpty.NewClient("http://localhost:" + strconv.Itoa(defaultBrokerPort))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	target, err := pickServer(ctx, c, *serverID)
	if err != nil {
		fmt.Println(err)
		return 1
	}

	alias, err := c.CreatePTY(ctx, target.Address, dpty.CreateOptions{
		Name:  *name,
		Shell: *shell,
		Args:  []string(shellArgs),
		Env:   []string(envs),
	})
	switch err {
	case nil:
	case dpty.ErrSessionExists:
		fmt.Printf("Session name %q already exists on server %s\n", *name, target.ID)
		return 1
	case dpty.ErrInvalidName:
		fmt.Printf("Session name %q is invalid (use 1-64 chars from [A-Za-z0-9._-])\n", *name)
		return 1
	default:
		fmt.Printf("Error creating PTY on %s: %v\n", target.ID, err)
		return 1
	}

	wsURL := dpty.AttachWebSocketURL(target.Address, alias)
	fmt.Println(wsURL)
	return 0
}

func pickServer(ctx context.Context, c *dpty.Client, id string) (*dpty.ServerStatus, error) {
	if id == "" {
		s, err := c.PickAvailableServer(ctx)
		if err != nil {
			return nil, fmt.Errorf("pick server: %w", err)
		}
		return s, nil
	}
	servers, err := c.ListServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}
	for i := range servers {
		if servers[i].ID == id {
			return &servers[i], nil
		}
	}
	return nil, fmt.Errorf("no server with ID %q", id)
}

// ---- helpers ----

type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// signalContext returns a context cancelled on SIGINT/SIGTERM.
func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		cancel()
	}()
	return ctx, cancel
}
