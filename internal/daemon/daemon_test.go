package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	if status.Servers[0].LastLatencyMS <= 0 {
		t.Fatalf("last latency ms = %d, want positive", status.Servers[0].LastLatencyMS)
	}
	if status.Servers[0].AvgLatencyMS <= 0 {
		t.Fatalf("avg latency ms = %d, want positive", status.Servers[0].AvgLatencyMS)
	}
	if status.Servers[0].MaxLatencyMS <= 0 {
		t.Fatalf("max latency ms = %d, want positive", status.Servers[0].MaxLatencyMS)
	}

	cancel()
	if err := <-errc; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
}

func TestStatusReportsActiveClients(t *testing.T) {
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

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	defer conn.Close()
	bind, _ := json.Marshal(BindRequest{Name: "fake"})
	if _, err := conn.Write(append(bind, '\n')); err != nil {
		t.Fatalf("write bind: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read bind response: %v", err)
	}
	var bindResp BindResponse
	if err := json.Unmarshal(line, &bindResp); err != nil {
		t.Fatalf("decode bind response: %v", err)
	}
	if !bindResp.OK {
		t.Fatalf("bind response = %#v, want ok", bindResp)
	}

	status, err := QueryStatus(socketPath)
	if err != nil {
		t.Fatalf("QueryStatus() error = %v", err)
	}
	if status.Clients != 1 {
		t.Fatalf("clients = %d, want 1", status.Clients)
	}
	if len(status.ActiveClients) != 1 {
		t.Fatalf("active clients = %#v, want one", status.ActiveClients)
	}
	if status.ActiveClients[0].ID == "" {
		t.Fatalf("active client id is empty")
	}
	if status.ActiveClients[0].Name != "fake" {
		t.Fatalf("active client name = %q, want fake", status.ActiveClients[0].Name)
	}
	if status.ActiveClients[0].ConnectedAt.IsZero() {
		t.Fatalf("active client connected_at is zero")
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

func TestControlReloadBusyRequiresForce(t *testing.T) {
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

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	defer conn.Close()
	bind, _ := json.Marshal(BindRequest{Name: "first"})
	if _, err := conn.Write(append(bind, '\n')); err != nil {
		t.Fatalf("write bind: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read bind response: %v", err)
	}
	var bindResp BindResponse
	if err := json.Unmarshal(line, &bindResp); err != nil {
		t.Fatalf("decode bind response: %v", err)
	}
	if !bindResp.OK {
		t.Fatalf("bind response = %#v, want ok", bindResp)
	}

	secondConfigPath := writeWrapperConfig(t, tempDir, wrapper.Config{Name: "second", Command: fakeMCP})
	writeDaemonConfig(t, tempDir, socketPath, []string{secondConfigPath})

	resp, err := SendControl(socketPath, "reload")
	if err != nil {
		t.Fatalf("SendControl(reload) error = %v", err)
	}
	if resp.OK || !strings.Contains(resp.Error, "reload busy") {
		t.Fatalf("SendControl(reload) = %#v, want busy", resp)
	}

	status, err := QueryStatus(socketPath)
	if err != nil {
		t.Fatalf("QueryStatus() error = %v", err)
	}
	if len(status.Servers) != 1 || status.Servers[0].Name != "first" {
		t.Fatalf("unexpected servers after busy reload: %#v", status.Servers)
	}

	resp, err = SendControl(socketPath, "reload", ControlOptions{Force: true})
	if err != nil {
		t.Fatalf("SendControl(force reload) error = %v", err)
	}
	if !resp.OK {
		t.Fatalf("SendControl(force reload) = %#v, want ok", resp)
	}

	status, err = QueryStatus(socketPath)
	if err != nil {
		t.Fatalf("QueryStatus() error = %v", err)
	}
	if len(status.Servers) != 1 || status.Servers[0].Name != "second" {
		t.Fatalf("unexpected servers after force reload: %#v", status.Servers)
	}

	cancel()
	if err := <-errc; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
}

func TestControlReloadGracefulRetainsActiveClientProxy(t *testing.T) {
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

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	bind, _ := json.Marshal(BindRequest{Name: "first"})
	if _, err := conn.Write(append(bind, '\n')); err != nil {
		t.Fatalf("write bind: %v", err)
	}
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read bind response: %v", err)
	}
	var bindResp BindResponse
	if err := json.Unmarshal(line, &bindResp); err != nil {
		t.Fatalf("decode bind response: %v", err)
	}
	if !bindResp.OK {
		t.Fatalf("bind response = %#v, want ok", bindResp)
	}

	secondConfigPath := writeWrapperConfig(t, tempDir, wrapper.Config{Name: "second", Command: fakeMCP})
	writeDaemonConfig(t, tempDir, socketPath, []string{secondConfigPath})
	resp, err := SendControl(socketPath, "reload", ControlOptions{Graceful: true})
	if err != nil {
		t.Fatalf("SendControl(graceful reload) error = %v", err)
	}
	if !resp.OK {
		t.Fatalf("SendControl(graceful reload) = %#v, want ok", resp)
	}

	status, err := QueryStatus(socketPath)
	if err != nil {
		t.Fatalf("QueryStatus() error = %v", err)
	}
	if len(status.Servers) != 1 || status.Servers[0].Name != "second" {
		t.Fatalf("unexpected servers after graceful reload: %#v", status.Servers)
	}
	if len(status.ActiveClients) != 1 || status.ActiveClients[0].Name != "first" {
		t.Fatalf("unexpected active clients after graceful reload: %#v", status.ActiveClients)
	}

	writer := jsonrpc.NewJSONLWriter(conn)
	if err := writer.Write(jsonrpc.Message{JSONRPC: "2.0", ID: raw(1), Method: "initialize", Params: raw(map[string]any{})}); err != nil {
		t.Fatalf("write initialize on old client: %v", err)
	}
	if err := writer.Write(jsonrpc.Message{JSONRPC: "2.0", ID: raw(2), Method: "tools/list", Params: raw(map[string]any{})}); err != nil {
		t.Fatalf("write tools/list on old client: %v", err)
	}
	mcpReader := jsonrpc.NewJSONLReader(reader)
	if _, err := mcpReader.Read(); err != nil {
		t.Fatalf("read initialize response on old client: %v", err)
	}
	listResp, err := mcpReader.Read()
	if err != nil {
		t.Fatalf("read tools/list response on old client: %v", err)
	}
	if listResp.Error != nil {
		t.Fatalf("tools/list on old client error = %#v", listResp.Error)
	}

	_ = conn.Close()
	eventually(t, 2*time.Second, func() bool {
		status, err := QueryStatus(socketPath)
		return err == nil && len(status.ActiveClients) == 0
	})

	cancel()
	if err := <-errc; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
}

func TestControlReloadRejectsForceAndGracefulTogether(t *testing.T) {
	tempDir := t.TempDir()
	socketPath := testSocketPath(t)
	defer os.Remove(socketPath)
	fakeMCP := buildFakeMCP(t, tempDir)

	configPath := writeWrapperConfig(t, tempDir, wrapper.Config{Name: "fake", Command: fakeMCP})
	daemonConfigPath := writeDaemonConfig(t, tempDir, socketPath, []string{configPath})
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

	resp, err := SendControl(socketPath, "reload", ControlOptions{Force: true, Graceful: true})
	if err != nil {
		t.Fatalf("SendControl(reload) error = %v", err)
	}
	if resp.OK || !strings.Contains(resp.Error, "cannot be used together") {
		t.Fatalf("SendControl(reload) = %#v, want conflict", resp)
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
	return filepath.Join(os.TempDir(), "lmcp-"+strconv.FormatInt(time.Now().UnixNano(), 36)+".sock")
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

func eventually(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition was not met within %s", timeout)
}

func raw(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}
