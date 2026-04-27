package dpty

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// brokerStateFileName is the name of the on-disk file used to persist the
// Broker's known (id -> address) entries.
const brokerStateFileName = "state.txt"

// readBrokerState reads (id -> address) pairs from a state file in dir.
//
// A missing file returns an empty map and no error. Blank lines and lines
// starting with '#' are ignored. Malformed lines are silently skipped.
func readBrokerState(dir string) (map[string]string, error) {
	out := map[string]string{}

	f, err := os.Open(filepath.Join(dir, brokerStateFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		out[parts[0]] = parts[1]
	}
	return out, sc.Err()
}

// writeBrokerState atomically writes the (id -> address) map to a state
// file in dir, creating the directory if needed. The write uses a
// write-temp-then-rename so a partial write can never be observed.
func writeBrokerState(dir string, entries map[string]string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, brokerStateFileName)
	tmp := path + ".tmp"

	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for id, addr := range entries {
		if _, err := fmt.Fprintf(w, "%s\t%s\n", id, addr); err != nil {
			_ = f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DefaultBrokerStateDir returns the directory the [Broker] uses for
// persistent state when [BrokerConfig.StateDir] is empty.
//
// Resolution order:
//  1. $DPTY_BROKER_STATE_DIR if set;
//  2. $HOME/.config/dpty/broker if a home directory is available;
//  3. /tmp/dpty.broker as a last resort.
func DefaultBrokerStateDir() string {
	if d := os.Getenv("DPTY_BROKER_STATE_DIR"); d != "" {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "dpty", "broker")
	}
	return "/tmp/dpty.broker"
}
