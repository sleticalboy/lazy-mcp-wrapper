package wrapper

import (
	"encoding/json"
	"os"
	"testing"
	"time"
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

func TestCacheKeyIncludesHTTPUpstreamMode(t *testing.T) {
	legacy := Config{Name: "remote", URL: "https://example.test/mcp", Protocol: "streamable-http"}
	stateless := legacy
	stateless.UpstreamMode = "stateless"
	if legacy.CacheInfo().Key == stateless.CacheInfo().Key {
		t.Fatalf("cache keys should differ for different upstream modes: %s", legacy.CacheInfo().Key)
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

func TestReadLegacyCachedToolsListRecord(t *testing.T) {
	cfg := Config{Name: "fake", Command: "fake-mcp", CacheDir: t.TempDir()}
	result := json.RawMessage(`{"tools":[{"name":"legacy"}]}`)
	record := cacheRecord{
		Version:   cacheVersion,
		Name:      cfg.Name,
		Key:       cfg.CacheInfo().Key,
		CreatedAt: time.Now(),
		Result:    result,
	}
	writeRawCacheRecord(t, cfg, record)

	msg, ok := cfg.readCachedToolsList()
	if !ok {
		t.Fatal("readCachedToolsList() legacy cache miss")
	}
	if string(msg.Result) != string(result) {
		t.Fatalf("result = %s, want %s", msg.Result, result)
	}
}

func TestCachedToolsListStoresMetadata(t *testing.T) {
	cfg := Config{Name: "fake", Command: "fake-mcp", CacheDir: t.TempDir()}
	result := json.RawMessage(`{"tools":[{"name":"echo"}],"ttlMs":60000,"cacheScope":"global"}`)

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
	info := cfg.CacheInfo()
	if info.TTLMS == nil || *info.TTLMS != 60000 {
		t.Fatalf("TTLMS = %v, want 60000", info.TTLMS)
	}
	if info.CacheScope != "global" {
		t.Fatalf("CacheScope = %q, want global", info.CacheScope)
	}
	if info.ExpiresAt == nil || info.Expired {
		t.Fatalf("unexpected cache expiry info: %#v", info)
	}
}

func TestCachedToolsListExpiresByTTL(t *testing.T) {
	cfg := Config{Name: "fake", Command: "fake-mcp", CacheDir: t.TempDir()}
	ttlMS := int64(1000)
	record := cacheRecord{
		Version:   cacheVersion,
		Name:      cfg.Name,
		Key:       cfg.CacheInfo().Key,
		CreatedAt: time.Now().Add(-2 * time.Second),
		TTLMS:     &ttlMS,
		Result:    json.RawMessage(`{"tools":[{"name":"expired"}],"ttlMs":1000}`),
	}
	writeRawCacheRecord(t, cfg, record)

	if _, ok := cfg.readCachedToolsList(); ok {
		t.Fatal("readCachedToolsList() cache hit, want expired miss")
	}
	if _, err := os.Stat(cfg.CacheInfo().File); !os.IsNotExist(err) {
		t.Fatalf("expired cache file should be removed, stat err=%v", err)
	}
}

func TestCachedToolsListSkipsPrivateScopes(t *testing.T) {
	for _, scope := range []string{"session", "private", "unknown"} {
		t.Run(scope, func(t *testing.T) {
			cfg := Config{Name: "fake-" + scope, Command: "fake-mcp", CacheDir: t.TempDir()}
			result := json.RawMessage(`{"tools":[{"name":"private"}],"cacheScope":"` + scope + `"}`)

			if err := cfg.writeCachedToolsList(result); err != nil {
				t.Fatalf("writeCachedToolsList() error = %v", err)
			}
			if _, ok := cfg.readCachedToolsList(); ok {
				t.Fatal("readCachedToolsList() cache hit, want private-scope miss")
			}
			if _, err := os.Stat(cfg.CacheInfo().File); !os.IsNotExist(err) {
				t.Fatalf("private-scope cache file should not exist, stat err=%v", err)
			}
		})
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

func writeRawCacheRecord(t *testing.T, cfg Config, record cacheRecord) {
	t.Helper()
	info := cfg.CacheInfo()
	if err := os.MkdirAll(info.Dir, 0755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatalf("marshal cache record: %v", err)
	}
	if err := os.WriteFile(info.File, append(data, '\n'), 0644); err != nil {
		t.Fatalf("write cache record: %v", err)
	}
}
