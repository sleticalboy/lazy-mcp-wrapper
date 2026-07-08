package wrapper

import (
	"encoding/json"
	"testing"
)

func TestCacheInfoDefaults(t *testing.T) {
	cfg := Config{Name: "fake", Command: "fake-mcp"}
	info := cfg.CacheInfo()
	if !info.Enabled {
		t.Fatal("cache should be enabled by default")
	}
	if info.Dir == "" || info.File == "" || info.Key == "" {
		t.Fatalf("incomplete cache info: %#v", info)
	}
}

func TestReadWriteCachedToolsList(t *testing.T) {
	cfg := Config{Name: "fake", Command: "fake-mcp", CacheDir: t.TempDir()}
	result := json.RawMessage(`{"tools":[{"name":"echo"}]}`)

	if err := cfg.writeCachedToolsList(result); err != nil {
		t.Fatalf("writeCachedToolsList() error = %v", err)
	}
	msg, ok := cfg.readCachedToolsList()
	if !ok {
		t.Fatal("readCachedToolsList() cache miss")
	}
	if string(msg.Result) != string(result) {
		t.Fatalf("result = %s, want %s", msg.Result, result)
	}
}

func TestWriteCachedToolsListClearsInvalidationStatus(t *testing.T) {
	cfg := Config{Name: "fake", Command: "fake-mcp", CacheDir: t.TempDir()}
	if err := cfg.invalidateCachedToolsList(); err != nil {
		t.Fatalf("invalidateCachedToolsList() error = %v", err)
	}
	info := cfg.CacheInfo()
	if !info.Invalidated {
		t.Fatalf("cache should be marked invalidated: %#v", info)
	}
	if err := cfg.writeCachedToolsList(json.RawMessage(`{"tools":[]}`)); err != nil {
		t.Fatalf("writeCachedToolsList() error = %v", err)
	}
	info = cfg.CacheInfo()
	if info.Invalidated || info.InvalidatedAt != nil {
		t.Fatalf("cache invalidation should be cleared after write: %#v", info)
	}
}
