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
