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
	"strings"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/jsonrpc"
)

const cacheVersion = 1

type CacheInfo struct {
	Enabled       bool       `json:"enabled"`
	Dir           string     `json:"dir"`
	File          string     `json:"file"`
	Key           string     `json:"key"`
	Exists        bool       `json:"exists"`
	TTLMS         *int64     `json:"ttlMs,omitempty"`
	CacheScope    string     `json:"cacheScope,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	Expired       bool       `json:"expired,omitempty"`
	Invalidated   bool       `json:"invalidated,omitempty"`
	InvalidatedAt *time.Time `json:"invalidated_at,omitempty"`
}

func (c Config) ClearCache() (CacheInfo, error) {
	info := c.CacheInfo()
	if !info.Enabled {
		return info, nil
	}
	if err := os.Remove(info.File); err != nil && !os.IsNotExist(err) {
		return info, err
	}
	if err := os.Remove(cacheInvalidationFile(info)); err != nil && !os.IsNotExist(err) {
		return info, err
	}
	info.Exists = false
	info.Invalidated = false
	info.InvalidatedAt = nil
	return info, nil
}

type cacheRecord struct {
	Version    int             `json:"version"`
	Name       string          `json:"name"`
	Key        string          `json:"key"`
	CreatedAt  time.Time       `json:"created_at"`
	TTLMS      *int64          `json:"ttlMs,omitempty"`
	CacheScope string          `json:"cacheScope,omitempty"`
	Result     json.RawMessage `json:"result"`
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
	if info.Exists {
		if record, ok := readCacheRecord(info.File); ok {
			info.TTLMS = record.TTLMS
			info.CacheScope = record.CacheScope
			if expiresAt, ok := record.expiresAt(); ok {
				info.ExpiresAt = &expiresAt
				info.Expired = time.Now().After(expiresAt)
			}
		}
	}
	if invalidatedAt, ok := readCacheInvalidation(cacheInvalidationFile(info)); ok {
		info.Invalidated = true
		info.InvalidatedAt = &invalidatedAt
	}
	return info
}

func (c Config) readCachedToolsList() (jsonrpc.Message, bool) {
	info := c.CacheInfo()
	if !info.Enabled || info.File == "" {
		return jsonrpc.Message{}, false
	}

	record, ok := readCacheRecord(info.File)
	if !ok {
		return jsonrpc.Message{}, false
	}
	if record.Version != cacheVersion || record.Key != info.Key || len(record.Result) == 0 {
		return jsonrpc.Message{}, false
	}
	if !record.cacheScopeReusable() {
		return jsonrpc.Message{}, false
	}
	if record.expired(time.Now()) {
		_ = os.Remove(info.File)
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
	metadata := cacheMetadataFromResult(result)
	if metadata.TTLMS != nil && *metadata.TTLMS <= 0 {
		_ = os.Remove(info.File)
		_ = os.Remove(cacheInvalidationFile(info))
		return nil
	}
	record := cacheRecord{
		Version:    cacheVersion,
		Name:       c.Name,
		Key:        info.Key,
		CreatedAt:  time.Now(),
		TTLMS:      metadata.TTLMS,
		CacheScope: metadata.CacheScope,
		Result:     result,
	}
	if !record.cacheScopeReusable() {
		_ = os.Remove(info.File)
		_ = os.Remove(cacheInvalidationFile(info))
		return nil
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	_ = os.Remove(cacheInvalidationFile(info))
	return os.WriteFile(info.File, append(data, '\n'), 0644)
}

func readCacheRecord(path string) (cacheRecord, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cacheRecord{}, false
	}
	var record cacheRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return cacheRecord{}, false
	}
	return record, true
}

type cacheMetadata struct {
	TTLMS      *int64 `json:"ttlMs"`
	CacheScope string `json:"cacheScope"`
}

func cacheMetadataFromResult(result json.RawMessage) cacheMetadata {
	var metadata cacheMetadata
	_ = json.Unmarshal(result, &metadata)
	metadata.CacheScope = strings.ToLower(strings.TrimSpace(metadata.CacheScope))
	return metadata
}

func (r cacheRecord) cacheScopeReusable() bool {
	switch strings.ToLower(strings.TrimSpace(r.CacheScope)) {
	case "", "global", "shared", "public", "user":
		return true
	case "session", "private":
		return false
	default:
		return false
	}
}

func (r cacheRecord) expiresAt() (time.Time, bool) {
	if r.TTLMS == nil {
		return time.Time{}, false
	}
	return r.CreatedAt.Add(time.Duration(*r.TTLMS) * time.Millisecond), true
}

func (r cacheRecord) expired(now time.Time) bool {
	expiresAt, ok := r.expiresAt()
	return ok && !now.Before(expiresAt)
}

func (c Config) invalidateCachedToolsList() error {
	info := c.CacheInfo()
	if !info.Enabled {
		return nil
	}
	if err := os.MkdirAll(info.Dir, 0755); err != nil {
		return err
	}
	if err := os.Remove(info.File); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(cacheInvalidationFile(info), []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0644)
}

func cacheInvalidationFile(info CacheInfo) string {
	if info.File == "" {
		return ""
	}
	return info.File + ".invalidated"
}

func readCacheInvalidation(path string) (time.Time, bool) {
	if path == "" {
		return time.Time{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(data)))
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func (c Config) cacheKey() string {
	framing, _ := c.Framing()
	material := struct {
		Command       string            `json:"command"`
		Args          []string          `json:"args"`
		Env           map[string]string `json:"env"`
		CWD           string            `json:"cwd"`
		URL           string            `json:"url"`
		Protocol      string            `json:"protocol"`
		HTTPBackend   string            `json:"http_backend"`
		UpstreamMode  string            `json:"upstream_protocol_mode"`
		Auth          string            `json:"auth"`
		OAuthResource string            `json:"oauth_resource"`
		Headers       map[string]string `json:"headers"`
		RealProtocol  string            `json:"real_protocol_version"`
		RealFraming   string            `json:"real_framing"`
		GOOS          string            `json:"goos"`
		GOARCH        string            `json:"goarch"`
	}{
		Command:       c.Command,
		Args:          c.Args,
		Env:           sortedEnv(c.Env),
		CWD:           c.CWD,
		URL:           c.URL,
		Protocol:      c.HTTPProtocol(),
		HTTPBackend:   c.HTTPBackend,
		UpstreamMode:  c.UpstreamProtocolMode(),
		Auth:          c.Auth,
		OAuthResource: c.OAuthResource,
		Headers:       sortedEnv(c.Headers),
		RealProtocol:  c.RealProtocol,
		RealFraming:   string(framing),
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
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
