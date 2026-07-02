package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
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

func TestProxyForwardsServerNotification(t *testing.T) {
	dir := t.TempDir()
	fakePath := filepath.Join(dir, "fake-mcp")
	buildFakeMCP(t, fakePath)

	cfg := Config{
		Name:           "fake",
		Command:        fakePath,
		Args:           []string{"--notify-tools-changed"},
		DisableCache:   true,
		IdleTimeout:    Duration{Duration: time.Second},
		StartupTimeout: Duration{Duration: 5 * time.Second},
		CallTimeout:    Duration{Duration: 5 * time.Second},
	}

	proxy := NewProxyWithOptions(cfg, log.New(testWriter{t: t}, "", 0), ProxyOptions{KeepRealOnClientClose: false})
	session := startProxySession(t, proxy)
	defer session.closeInput()

	session.writeRequest(1, "initialize", map[string]any{})
	readResponse(t, session.reader, "initialize")
	session.writeRequest(2, "tools/list", map[string]any{})
	readResponse(t, session.reader, "tools/list")
	notif := readMessage(t, session.reader, "server notification")
	if notif.Method != "notifications/tools/list_changed" {
		t.Fatalf("notification method = %q, want notifications/tools/list_changed", notif.Method)
	}
	if len(notif.ID) != 0 {
		t.Fatalf("notification ID = %s, want empty", string(notif.ID))
	}
	session.closeInput()
	session.wait()
}

func TestProxyBroadcastsNotificationToSharedClients(t *testing.T) {
	dir := t.TempDir()
	fakePath := filepath.Join(dir, "fake-mcp")
	buildFakeMCP(t, fakePath)

	cfg := Config{
		Name:           "fake",
		Command:        fakePath,
		Args:           []string{"--notify-tools-changed"},
		DisableCache:   true,
		IdleTimeout:    Duration{Duration: time.Second},
		StartupTimeout: Duration{Duration: 5 * time.Second},
		CallTimeout:    Duration{Duration: 5 * time.Second},
	}

	proxy := NewProxyWithOptions(cfg, log.New(testWriter{t: t}, "", 0), ProxyOptions{KeepRealOnClientClose: true})
	defer proxy.Close()

	first := startProxySession(t, proxy)
	defer first.closeInput()
	second := startProxySession(t, proxy)
	defer second.closeInput()

	first.writeRequest(1, "initialize", map[string]any{})
	readResponse(t, first.reader, "first initialize")
	second.writeRequest(1, "initialize", map[string]any{})
	readResponse(t, second.reader, "second initialize")

	first.writeRequest(2, "tools/call", map[string]any{})
	readResponse(t, first.reader, "first tools/call")
	second.writeRequest(2, "tools/call", map[string]any{})
	readResponse(t, second.reader, "second tools/call")

	first.writeRequest(3, "tools/list", map[string]any{})
	readResponse(t, first.reader, "first tools/list")

	notif1 := readMessage(t, first.reader, "first notification")
	if notif1.Method != "notifications/tools/list_changed" {
		t.Fatalf("first notification method = %q", notif1.Method)
	}
	notif2 := readMessage(t, second.reader, "second notification")
	if notif2.Method != "notifications/tools/list_changed" {
		t.Fatalf("second notification method = %q", notif2.Method)
	}

	first.closeInput()
	second.closeInput()
	first.wait()
	second.wait()
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

func readResponse(t *testing.T, reader *jsonrpc.Reader, label string) jsonrpc.Message {
	t.Helper()
	msg := readMessage(t, reader, label)
	if msg.Error != nil {
		t.Fatalf("%s error = %#v", label, msg.Error)
	}
	if len(msg.ID) == 0 {
		t.Fatalf("%s ID is empty, got notification method=%s", label, msg.Method)
	}
	return msg
}

func readMessage(t *testing.T, reader *jsonrpc.Reader, label string) jsonrpc.Message {
	t.Helper()
	msg, err := reader.Read()
	if err != nil {
		t.Fatalf("read %s: %v", label, err)
	}
	return msg
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

type proxySession struct {
	t      *testing.T
	inR    *io.PipeReader
	inW    *io.PipeWriter
	outR   *io.PipeReader
	outW   *io.PipeWriter
	writer *jsonrpc.Writer
	reader *jsonrpc.Reader
	errc   chan error
	once   sync.Once
}

func startProxySession(t *testing.T, proxy *Proxy) *proxySession {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	session := &proxySession{
		t:      t,
		inR:    inR,
		inW:    inW,
		outR:   outR,
		outW:   outW,
		writer: jsonrpc.NewWriter(inW),
		reader: jsonrpc.NewReader(outR),
		errc:   make(chan error, 1),
	}
	go func() {
		session.errc <- proxy.Run(context.Background(), inR, outW)
		_ = outW.Close()
	}()
	return session
}

func (s *proxySession) writeRequest(id int, method string, params any) {
	s.t.Helper()
	writeRequest(s.t, s.writer, id, method, params)
}

func (s *proxySession) closeInput() {
	s.once.Do(func() {
		_ = s.inW.Close()
	})
}

func (s *proxySession) wait() {
	s.t.Helper()
	select {
	case err := <-s.errc:
		if err != nil {
			s.t.Fatalf("proxy session error: %v", err)
		}
	case <-time.After(2 * time.Second):
		s.t.Fatalf("proxy session did not stop")
	}
	_ = s.inR.Close()
	_ = s.outR.Close()
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s", string(p))
	return len(p), nil
}
