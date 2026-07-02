package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/jsonrpc"
	"github.com/binlee/lazy-mcp-wrapper/internal/wrapper"
)

func TestClientForwardsToSharedDaemon(t *testing.T) {
	tempDir := t.TempDir()
	socketPath := testSocketPath(t)
	fakeMCP := buildFakeMCP(t, tempDir)
	defer os.Remove(socketPath)

	cfg := wrapper.Config{
		Name:    "fake",
		Command: fakeMCP,
	}
	cfg.IdleTimeout.Duration = time.Second
	cfg.StartupTimeout.Duration = 5 * time.Second
	cfg.CallTimeout.Duration = 5 * time.Second

	server, err := NewServer(socketPath, []wrapper.Config{cfg}, map[string]*log.Logger{
		"fake": log.New(bytes.NewBuffer(nil), "", 0),
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- server.Serve(ctx)
	}()
	waitForSocket(t, socketPath, errc)

	var input bytes.Buffer
	writer := jsonrpc.NewJSONLWriter(&input)
	if err := writer.Write(jsonrpc.Message{JSONRPC: "2.0", ID: raw(1), Method: "initialize", Params: raw(map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "daemon-test", "version": "0"},
	})}); err != nil {
		t.Fatalf("write initialize: %v", err)
	}
	if err := writer.Write(jsonrpc.Message{JSONRPC: "2.0", ID: raw(2), Method: "tools/list", Params: raw(map[string]any{})}); err != nil {
		t.Fatalf("write tools/list: %v", err)
	}

	var output bytes.Buffer
	if err := RunClient(socketPath, "fake", &input, &output); err != nil {
		t.Fatalf("RunClient() error = %v", err)
	}

	reader := jsonrpc.NewJSONLReader(&output)
	initResp, err := reader.Read()
	if err != nil {
		t.Fatalf("read initialize response: %v", err)
	}
	if initResp.Error != nil {
		t.Fatalf("initialize error = %#v", initResp.Error)
	}
	listResp, err := reader.Read()
	if err != nil {
		t.Fatalf("read tools/list response: %v", err)
	}
	if listResp.Error != nil {
		t.Fatalf("tools/list error = %#v", listResp.Error)
	}
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(listResp.Result, &result); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	if len(result.Tools) != 1 || result.Tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %#v", result.Tools)
	}

	cancel()
	if err := <-errc; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
}

func TestClientUnknownName(t *testing.T) {
	tempDir := t.TempDir()
	socketPath := testSocketPath(t)
	fakeMCP := buildFakeMCP(t, tempDir)
	defer os.Remove(socketPath)
	cfg := wrapper.Config{Name: "fake", Command: fakeMCP}
	cfg.IdleTimeout.Duration = time.Second
	cfg.StartupTimeout.Duration = 5 * time.Second
	cfg.CallTimeout.Duration = 5 * time.Second

	server, err := NewServer(socketPath, []wrapper.Config{cfg}, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- server.Serve(ctx)
	}()
	waitForSocket(t, socketPath, errc)

	err = RunClient(socketPath, "missing", strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown MCP name") {
		t.Fatalf("RunClient() error = %v, want unknown MCP name", err)
	}

	cancel()
	if err := <-errc; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
}

func TestQueryStatus(t *testing.T) {
	tempDir := t.TempDir()
	socketPath := testSocketPath(t)
	fakeMCP := buildFakeMCP(t, tempDir)
	defer os.Remove(socketPath)
	cfg := wrapper.Config{Name: "fake", Command: fakeMCP}
	cfg.IdleTimeout.Duration = time.Second
	cfg.StartupTimeout.Duration = 5 * time.Second
	cfg.CallTimeout.Duration = 5 * time.Second

	server, err := NewServer(socketPath, []wrapper.Config{cfg}, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- server.Serve(ctx)
	}()
	waitForSocket(t, socketPath, errc)

	status, err := QueryStatus(socketPath)
	if err != nil {
		t.Fatalf("QueryStatus() error = %v", err)
	}
	if status.SocketPath != socketPath {
		t.Fatalf("socket path = %q, want %q", status.SocketPath, socketPath)
	}
	if len(status.Servers) != 1 || status.Servers[0].Name != "fake" {
		t.Fatalf("unexpected servers: %#v", status.Servers)
	}
	if status.DaemonPID <= 0 {
		t.Fatalf("daemon pid = %d, want positive", status.DaemonPID)
	}
	if status.StartedAt.IsZero() {
		t.Fatalf("started_at is zero")
	}
	if status.Uptime == "" {
		t.Fatalf("uptime is empty")
	}
	if status.Servers[0].HasReal {
		t.Fatalf("expected no real MCP before tool call: %#v", status.Servers[0])
	}

	cancel()
	if err := <-errc; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
}

func TestTotalCallsCountsJSONRPCRequests(t *testing.T) {
	tempDir := t.TempDir()
	socketPath := testSocketPath(t)
	fakeMCP := buildFakeMCP(t, tempDir)
	defer os.Remove(socketPath)
	cfg := wrapper.Config{Name: "fake", Command: fakeMCP}
	cfg.IdleTimeout.Duration = time.Second
	cfg.StartupTimeout.Duration = 5 * time.Second
	cfg.CallTimeout.Duration = 5 * time.Second

	server, err := NewServer(socketPath, []wrapper.Config{cfg}, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- server.Serve(ctx)
	}()
	waitForSocket(t, socketPath, errc)

	var input bytes.Buffer
	writer := jsonrpc.NewJSONLWriter(&input)
	if err := writer.Write(jsonrpc.Message{JSONRPC: "2.0", ID: raw(1), Method: "initialize", Params: raw(map[string]any{})}); err != nil {
		t.Fatalf("write initialize: %v", err)
	}
	if err := writer.Write(jsonrpc.Message{JSONRPC: "2.0", ID: raw(2), Method: "tools/list", Params: raw(map[string]any{})}); err != nil {
		t.Fatalf("write tools/list: %v", err)
	}
	if err := RunClient(socketPath, "fake", &input, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunClient() error = %v", err)
	}

	status, err := QueryStatus(socketPath)
	if err != nil {
		t.Fatalf("QueryStatus() error = %v", err)
	}
	if status.TotalCalls != 1 {
		t.Fatalf("total_calls = %d, want 1", status.TotalCalls)
	}
	if len(status.Servers) != 1 {
		t.Fatalf("servers len = %d, want 1", len(status.Servers))
	}
	if status.Servers[0].Calls != 1 {
		t.Fatalf("server calls = %d, want 1", status.Servers[0].Calls)
	}
	if status.Servers[0].LastMethod != "tools/list" {
		t.Fatalf("last method = %q, want tools/list", status.Servers[0].LastMethod)
	}
	if status.Servers[0].LastCallAt == nil {
		t.Fatalf("last call at is nil")
	}

	cancel()
	if err := <-errc; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
}

func TestControlReloadRequiresDaemonConfig(t *testing.T) {
	socketPath := testSocketPath(t)
	defer os.Remove(socketPath)
	cfg := wrapper.Config{Name: "fake", Command: "fake"}

	server, err := NewServer(socketPath, []wrapper.Config{cfg}, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- server.Serve(ctx)
	}()
	waitForSocket(t, socketPath, errc)

	resp, err := SendControl(socketPath, "reload")
	if err != nil {
		t.Fatalf("SendControl(reload) error = %v", err)
	}
	if resp.OK || !strings.Contains(resp.Error, "requires daemon to start with --daemon-config") {
		t.Fatalf("SendControl(reload) = %#v, want daemon config error", resp)
	}

	cancel()
	if err := <-errc; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
}

func TestControlReloadFromDaemonConfig(t *testing.T) {
	tempDir := t.TempDir()
	socketPath := testSocketPath(t)
	defer os.Remove(socketPath)
	fakeMCP := buildFakeMCP(t, tempDir)

	firstConfigPath := writeWrapperConfig(t, tempDir, wrapper.Config{Name: "first", Command: fakeMCP})
	daemonConfigPath := writeDaemonConfig(t, tempDir, socketPath, []string{firstConfigPath})
	server, err := NewServerFromConfig(daemonConfigPath)
	if err != nil {
		t.Fatalf("NewServerFromConfig() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- server.Serve(ctx)
	}()
	waitForSocket(t, socketPath, errc)

	status, err := QueryStatus(socketPath)
	if err != nil {
		t.Fatalf("QueryStatus() error = %v", err)
	}
	if len(status.Servers) != 1 || status.Servers[0].Name != "first" {
		t.Fatalf("unexpected servers before reload: %#v", status.Servers)
	}
	if status.DaemonConfigPath != daemonConfigPath {
		t.Fatalf("daemon config path = %q, want %q", status.DaemonConfigPath, daemonConfigPath)
	}

	secondConfigPath := writeWrapperConfig(t, tempDir, wrapper.Config{Name: "second", Command: fakeMCP})
	writeDaemonConfig(t, tempDir, socketPath, []string{secondConfigPath})
	resp, err := SendControl(socketPath, "reload")
	if err != nil {
		t.Fatalf("SendControl(reload) error = %v", err)
	}
	if !resp.OK {
		t.Fatalf("SendControl(reload) = %#v, want ok", resp)
	}

	status, err = QueryStatus(socketPath)
	if err != nil {
		t.Fatalf("QueryStatus() error = %v", err)
	}
	if len(status.Servers) != 1 || status.Servers[0].Name != "second" {
		t.Fatalf("unexpected servers after reload: %#v", status.Servers)
	}
	if status.Servers[0].LastReloadedAt == nil {
		t.Fatalf("last_reloaded_at is nil after reload")
	}

	cancel()
	if err := <-errc; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
}

func TestControlStop(t *testing.T) {
	socketPath := testSocketPath(t)
	defer os.Remove(socketPath)
	cfg := wrapper.Config{Name: "fake", Command: "fake"}

	server, err := NewServer(socketPath, []wrapper.Config{cfg}, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- server.Serve(ctx)
	}()
	waitForSocket(t, socketPath, errc)

	resp, err := SendControl(socketPath, "stop")
	if err != nil {
		t.Fatalf("SendControl(stop) error = %v", err)
	}
	if !resp.OK {
		t.Fatalf("SendControl(stop) = %#v, want ok", resp)
	}

	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("server did not stop")
	}
}

func buildFakeMCP(t *testing.T, tempDir string) string {
	t.Helper()
	path := filepath.Join(tempDir, "fake-mcp")
	cmd := testCommand(t, "go", "build", "-o", path, "../../cmd/fake-mcp")
	cmd.Env = append(os.Environ(), "GOCACHE=/private/tmp/lazy-mcp-wrapper-gocache")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fake-mcp: %v\n%s", err, string(out))
	}
	return path
}

func testCommand(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()
	return exec.Command(name, args...)
}

func testSocketPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(os.TempDir(), "lmcp-"+strings.ReplaceAll(t.Name(), "/", "-")+".sock")
}

func writeWrapperConfig(t *testing.T, dir string, cfg wrapper.Config) string {
	t.Helper()
	if cfg.IdleTimeout.Duration == 0 {
		cfg.IdleTimeout.Duration = time.Second
	}
	if cfg.StartupTimeout.Duration == 0 {
		cfg.StartupTimeout.Duration = 5 * time.Second
	}
	if cfg.CallTimeout.Duration == 0 {
		cfg.CallTimeout.Duration = 5 * time.Second
	}
	path := filepath.Join(dir, cfg.Name+".json")
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal wrapper config: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write wrapper config: %v", err)
	}
	return path
}

func writeDaemonConfig(t *testing.T, dir, socketPath string, configPaths []string) string {
	t.Helper()
	path := filepath.Join(dir, "daemon.json")
	data, err := json.Marshal(Config{SocketPath: socketPath, ConfigPaths: configPaths})
	if err != nil {
		t.Fatalf("marshal daemon config: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write daemon config: %v", err)
	}
	return path
}

func waitForSocket(t *testing.T, socketPath string, errc <-chan error) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			return
		}
		select {
		case err := <-errc:
			t.Fatalf("Serve() exited before socket was created: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("socket not created: %s", socketPath)
}

func raw(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}
