package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/jsonrpc"
)

func TestProxyForwardsToolsList(t *testing.T) {
	dir := t.TempDir()
	fakePath := filepath.Join(dir, "fake-mcp")
	buildFakeMCP(t, fakePath)

	cfg := Config{
		Name:           "fake",
		Command:        fakePath,
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
}

func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("go.mod not found")
		}
		dir = next
	}
}

func writeRequest(t *testing.T, writer *jsonrpc.Writer, id int, method string, params any) {
	t.Helper()
	if err := writer.Write(jsonrpc.Message{
		JSONRPC: "2.0",
		ID:      mustRaw(id),
		Method:  method,
		Params:  mustRaw(params),
	}); err != nil {
		t.Fatalf("write request %s: %v", method, err)
	}
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s", string(p))
	return len(p), nil
}
