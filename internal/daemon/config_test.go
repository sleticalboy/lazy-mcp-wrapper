package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.json")
	data := []byte(`{
  "socket": "/tmp/lazy-mcpd.sock",
  "configs": ["/tmp/context7.json", "/tmp/mastergo.json"]
}`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.SocketPath != "/tmp/lazy-mcpd.sock" {
		t.Fatalf("socket = %q", cfg.SocketPath)
	}
	if len(cfg.ConfigPaths) != 2 || cfg.ConfigPaths[0] != "/tmp/context7.json" {
		t.Fatalf("configs = %#v", cfg.ConfigPaths)
	}
}

func TestLoadConfigRequiresSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.json")
	if err := os.WriteFile(path, []byte(`{"configs":["/tmp/context7.json"]}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := LoadConfig(path); err == nil {
		t.Fatal("LoadConfig() error is nil, want socket validation error")
	}
}
