package wrapper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/jsonrpc"
)

const cacheVersion = 1

type CacheInfo struct {
	Enabled bool   `json:"enabled"`
	Dir     string `json:"dir"`
	File    string `json:"file"`
	Key     string `json:"key"`
	Exists  bool   `json:"exists"`
}

func (c Config) ClearCache() (CacheInfo, error) {
	info := c.CacheInfo()
	if !info.Enabled {
		return info, nil
	}
	if err := os.Remove(info.File); err != nil && !os.IsNotExist(err) {
		return info, err
	}
	info.Exists = false
	return info, nil
}

type cacheRecord struct {
	Version   int             `json:"version"`
	Name      string          `json:"name"`
	Key       string          `json:"key"`
	CreatedAt time.Time       `json:"created_at"`
	Result    json.RawMessage `json:"result"`
}

func (c Config) CacheInfo() CacheInfo {
	info := CacheInfo{Enabled: !c.DisableCache}
	if !info.Enabled {
		return info
	}
	info.Dir = c.CacheDir
	if info.Dir == "" {
		info.Dir = defaultCacheDir()
	}
	info.Key = c.cacheKey()
	info.File = filepath.Join(info.Dir, c.Name+"-"+info.Key+".json")
	_, err := os.Stat(info.File)
	info.Exists = err == nil
	return info
}

func (c Config) readCachedToolsList() (jsonrpc.Message, bool) {
	info := c.CacheInfo()
	if !info.Enabled || info.File == "" {
		return jsonrpc.Message{}, false
	}

	data, err := os.ReadFile(info.File)
	if err != nil {
		return jsonrpc.Message{}, false
	}
	var record cacheRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return jsonrpc.Message{}, false
	}
	if record.Version != cacheVersion || record.Key != info.Key || len(record.Result) == 0 {
		return jsonrpc.Message{}, false
	}
	return jsonrpc.Response(nil, record.Result), true
}

func (c Config) writeCachedToolsList(result json.RawMessage) error {
	info := c.CacheInfo()
	if !info.Enabled {
		return nil
	}
	if err := os.MkdirAll(info.Dir, 0755); err != nil {
		return err
	}
	record := cacheRecord{
		Version:   cacheVersion,
		Name:      c.Name,
		Key:       info.Key,
		CreatedAt: time.Now(),
		Result:    result,
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(info.File, append(data, '\n'), 0644)
}

func (c Config) cacheKey() string {
	framing, _ := c.Framing()
	material := struct {
		Command      string            `json:"command"`
		Args         []string          `json:"args"`
		Env          map[string]string `json:"env"`
		CWD          string            `json:"cwd"`
		RealProtocol string            `json:"real_protocol_version"`
		RealFraming  string            `json:"real_framing"`
		GOOS         string            `json:"goos"`
		GOARCH       string            `json:"goarch"`
	}{
		Command:      c.Command,
		Args:         c.Args,
		Env:          sortedEnv(c.Env),
		CWD:          c.CWD,
		RealProtocol: c.RealProtocol,
		RealFraming:  string(framing),
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
	}
	data, _ := json.Marshal(material)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:16]
}

func sortedEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(env))
	for _, key := range keys {
		out[key] = env[key]
	}
	return out
}

func defaultCacheDir() string {
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "lazy-mcp-wrapper")
	}
	return filepath.Join(os.TempDir(), "lazy-mcp-wrapper-cache")
}

func RefreshToolsCache(ctx context.Context, cfg Config, logger *log.Logger) (CacheInfo, error) {
	if cfg.DisableCache {
		return cfg.CacheInfo(), fmt.Errorf("cache is disabled")
	}
	client, err := startReal(ctx, cfg, logger, initRequest{ProtocolVersion: cfg.RealProtocol})
	if err != nil {
		return cfg.CacheInfo(), err
	}
	defer client.close()

	resp, release, err := client.call(ctx, "tools/list", mustRaw(map[string]any{}))
	if err != nil {
		return cfg.CacheInfo(), err
	}
	release()
	if resp.Error != nil {
		return cfg.CacheInfo(), fmt.Errorf("tools/list failed: %s", resp.Error.Message)
	}
	if err := cfg.writeCachedToolsList(resp.Result); err != nil {
		return cfg.CacheInfo(), err
	}
	return cfg.CacheInfo(), nil
}
