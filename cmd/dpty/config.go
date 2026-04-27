package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is the persisted CLI configuration. New keys may be added here
// over time; missing fields decode to zero values.
type Config struct {
	Broker string `json:"broker,omitempty"`
}

func configDir() (string, error) {
	if d := os.Getenv("DPTY_CONFIG_DIR"); d != "" {
		return d, nil
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "dpty"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "dpty"), nil
}

func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// loadConfig reads the config file. A missing or empty file returns the
// zero Config with no error.
func loadConfig() (Config, error) {
	var c Config
	p, err := configPath()
	if err != nil {
		return c, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if len(data) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("parse %s: %w", p, err)
	}
	return c, nil
}

// saveConfig atomically writes the config file, creating its directory
// if necessary.
func saveConfig(c Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".config.json.*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// effectiveBrokerURL returns the broker URL the CLI should connect to:
// the configured value if set, otherwise the built-in default.
func effectiveBrokerURL() string {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: reading config: %v\n", err)
	}
	if cfg.Broker != "" {
		return cfg.Broker
	}
	return "http://localhost:" + strconv.Itoa(defaultBrokerPort)
}

// ---- config subcommand ----

func cmdConfig(args []string) int {
	if len(args) == 0 {
		configUsage(os.Stderr)
		return 2
	}
	switch args[0] {
	case "get":
		return cmdConfigGet(args[1:])
	case "set":
		return cmdConfigSet(args[1:])
	case "-h", "--help", "help":
		configUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown config subcommand: %s\n", args[0])
		configUsage(os.Stderr)
		return 2
	}
}

func configUsage(w *os.File) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  dpty config get <key>")
	fmt.Fprintln(w, "  dpty config set <key> <value>")
	fmt.Fprintln(w, "Keys:")
	fmt.Fprintln(w, "  broker   URL the CLI uses to reach the broker (e.g. http://localhost:5127)")
}

func cmdConfigGet(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: dpty config get <key>")
		return 2
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	switch args[0] {
	case "broker":
		fmt.Println(cfg.Broker)
	default:
		fmt.Fprintf(os.Stderr, "Unknown config key: %s\n", args[0])
		return 2
	}
	return 0
}

func cmdConfigSet(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: dpty config set <key> <value>")
		return 2
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	value := strings.TrimSpace(args[1])
	switch args[0] {
	case "broker":
		cfg.Broker = value
	default:
		fmt.Fprintf(os.Stderr, "Unknown config key: %s\n", args[0])
		return 2
	}
	if err := saveConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	return 0
}
