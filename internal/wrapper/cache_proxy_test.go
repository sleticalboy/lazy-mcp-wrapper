package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/jsonrpc"
)

func TestProxyToolsListUsesCache(t *testing.T) {
	dir := t.TempDir()
	fakePath := filepath.Join(dir, "fake-mcp")
	buildFakeMCP(t, fakePath)

	cfg := Config{
		Name:           "fake",
		Command:        fakePath,
		CacheDir:       t.TempDir(),
		IdleTimeout:    Duration{Duration: time.Second},
		StartupTimeout: Duration{Duration: 5 * time.Second},
		CallTimeout:    Duration{Duration: 5 * time.Second},
	}

	var in bytes.Buffer
	clientWriter := jsonrpc.NewWriter(&in)
	writeRequest(t, clientWriter, 1, "initialize", map[string]any{})
	writeRequest(t, clientWriter, 2, "tools/list", map[string]any{})

	var out bytes.Buffer
	proxy := NewProxy(cfg, log.New(testWriter{t: t}, "", 0))
	if err := proxy.Run(context.Background(), &in, &out); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	reader := jsonrpc.NewReader(&out)
	if _, err := reader.Read(); err != nil {
		t.Fatalf("read initialize response: %v", err)
	}
	resp, err := reader.Read()
	if err != nil {
		t.Fatalf("read tools/list response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("tools/list error = %#v", resp.Error)
	}

	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Tools) != 1 || result.Tools[0].Name != "echo" {
		t.Fatalf("tools = %#v", result.Tools)
	}

	if _, err := os.Stat(cfg.CacheInfo().File); err != nil {
		t.Fatalf("expected cache file: %v", err)
	}
}

func TestClearCache(t *testing.T) {
	cfg := Config{Name: "fake", Command: "fake-mcp", CacheDir: t.TempDir()}
	if err := cfg.writeCachedToolsList(json.RawMessage(`{"tools":[]}`)); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	info, err := cfg.ClearCache()
	if err != nil {
		t.Fatalf("ClearCache() error = %v", err)
	}
	if info.Exists {
		t.Fatalf("cache still exists: %#v", info)
	}
}

var fakeBuildCount atomic.Int32

func buildFakeMCP(t *testing.T, output string) {
	t.Helper()
	fakeBuildCount.Add(1)
	cmd := exec.Command("go", "build", "-o", output, "./cmd/fake-mcp")
	cmd.Dir = repoRoot(t)
	cmd.Env = testGoEnv()
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fake MCP: %v\n%s", err, string(data))
	}
}

func testGoEnv() []string {
	env := os.Environ()
	if os.Getenv("GOCACHE") == "" {
		env = append(env, "GOCACHE="+filepath.Join(os.TempDir(), "lazy-mcp-wrapper-gocache"))
	}
	return env
}
