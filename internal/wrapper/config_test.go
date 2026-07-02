package wrapper

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context7.json")
	if err := os.WriteFile(path, []byte(`{"name":"context7","command":"npx"}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.IdleTimeout.Duration != 30*time.Second {
		t.Fatalf("IdleTimeout = %s", cfg.IdleTimeout.Duration)
	}
	if cfg.StartupTimeout.Duration != 20*time.Second {
		t.Fatalf("StartupTimeout = %s", cfg.StartupTimeout.Duration)
	}
	if cfg.CallTimeout.Duration != 2*time.Minute {
		t.Fatalf("CallTimeout = %s", cfg.CallTimeout.Duration)
	}
	if cfg.Sharing != "shared" {
		t.Fatalf("Sharing = %s, want shared", cfg.Sharing)
	}
}

func TestLoadConfigSharing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "playwright.json")
	if err := os.WriteFile(path, []byte(`{"name":"playwright","sharing":"session","command":"npx"}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Sharing != "session" {
		t.Fatalf("Sharing = %s, want session", cfg.Sharing)
	}
}

func TestLoadConfigInvalidSharing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"name":"bad","sharing":"global","command":"npx"}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := LoadConfig(path); err == nil {
		t.Fatalf("LoadConfig() error = nil, want invalid sharing")
	}
}
