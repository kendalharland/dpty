package dpty

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBrokerState_writeReadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	in := map[string]string{
		"host-a:5137": "http://10.0.0.1:5137",
		"host-b:5137": "http://10.0.0.2:5137",
	}
	if err := writeBrokerState(dir, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readBrokerState(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != len(in) {
		t.Fatalf("got %d entries, want %d", len(got), len(in))
	}
	for k, v := range in {
		if got[k] != v {
			t.Errorf("got[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestBrokerState_readMissingFileIsEmpty(t *testing.T) {
	got, err := readBrokerState(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

func TestBrokerState_writeIsAtomic(t *testing.T) {
	dir := t.TempDir()
	if err := writeBrokerState(dir, map[string]string{"a": "b"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	// No leftover .tmp file after a successful write.
	tmp := filepath.Join(dir, brokerStateFileName+".tmp")
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("temp file should not exist: %v", err)
	}
}

func TestBrokerState_skipsBlanksAndComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, brokerStateFileName)
	contents := "" +
		"# a comment\n" +
		"\n" +
		"good\thttp://x:1\n" +
		"  \n" +
		"malformed_line_no_tab\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readBrokerState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["good"] != "http://x:1" {
		t.Errorf("got %v", got)
	}
}
