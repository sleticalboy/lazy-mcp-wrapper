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
}
