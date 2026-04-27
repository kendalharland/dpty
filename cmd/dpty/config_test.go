package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigMissingReturnsZero(t *testing.T) {
	t.Setenv("DPTY_CONFIG_DIR", t.TempDir())

	got, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if got.Broker != "" {
		t.Errorf("Broker = %q, want empty", got.Broker)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DPTY_CONFIG_DIR", dir)

	want := Config{Broker: "http://broker.example:5127"}
	if err := saveConfig(want); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Fatalf("config.json not written: %v", err)
	}

	got, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if got != want {
		t.Errorf("loadConfig = %+v, want %+v", got, want)
	}
}

func TestLoadConfigInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DPTY_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestEffectiveBrokerURLFallsBackToDefault(t *testing.T) {
	t.Setenv("DPTY_CONFIG_DIR", t.TempDir())
	got := effectiveBrokerURL()
	if got != "http://localhost:5127" {
		t.Errorf("effectiveBrokerURL = %q, want default http://localhost:5127", got)
	}
}

func TestEffectiveBrokerURLUsesConfig(t *testing.T) {
	t.Setenv("DPTY_CONFIG_DIR", t.TempDir())
	if err := saveConfig(Config{Broker: "http://other.example:9999"}); err != nil {
		t.Fatal(err)
	}
	got := effectiveBrokerURL()
	if got != "http://other.example:9999" {
		t.Errorf("effectiveBrokerURL = %q, want http://other.example:9999", got)
	}
}

func TestCmdConfigSetThenGet(t *testing.T) {
	t.Setenv("DPTY_CONFIG_DIR", t.TempDir())
	if rc := cmdConfigSet([]string{"broker", "http://saved.example:1234"}); rc != 0 {
		t.Fatalf("cmdConfigSet rc = %d, want 0", rc)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Broker != "http://saved.example:1234" {
		t.Errorf("Broker = %q, want http://saved.example:1234", cfg.Broker)
	}
}

func TestCmdConfigUnknownKey(t *testing.T) {
	t.Setenv("DPTY_CONFIG_DIR", t.TempDir())
	if rc := cmdConfigSet([]string{"nope", "x"}); rc != 2 {
		t.Errorf("cmdConfigSet unknown key rc = %d, want 2", rc)
	}
	if rc := cmdConfigGet([]string{"nope"}); rc != 2 {
		t.Errorf("cmdConfigGet unknown key rc = %d, want 2", rc)
	}
}
