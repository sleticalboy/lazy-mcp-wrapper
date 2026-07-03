package wrapper

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/jsonrpc"
)

func TestHTTPProxyForwardsToolsListOverSSE(t *testing.T) {
	backend := newTestHTTPMCP(t, "sse")
	defer backend.Close()

	cfg := httpTestConfig("fake", backend.URL, "sse")
	cfg.Headers = map[string]string{"Authorization": "Bearer test-token"}
	proxy := NewProxy(cfg, log.New(testWriter{t: t}, "", 0))
	session := startProxySession(t, proxy)
	defer session.closeInput()

	session.writeRequest(1, "initialize", map[string]any{})
	readResponse(t, session.reader, "initialize")
	session.writeRequest(2, "tools/list", map[string]any{})
	resp := readResponse(t, session.reader, "tools/list")
	assertEchoTool(t, resp)
	if got := backend.header("Authorization"); got != "Bearer test-token" {
		t.Fatalf("Authorization header = %q", got)
	}
	session.closeInput()
	session.wait()
}

func TestSSEProxyForwardsServerNotification(t *testing.T) {
	backend := newTestHTTPMCP(t, "sse")
	backend.notifyToolsChanged = true
	defer backend.Close()

	cfg := httpTestConfig("fake", backend.URL, "sse")
	cfg.DisableCache = true
	proxy := NewProxyWithOptions(cfg, log.New(testWriter{t: t}, "", 0), ProxyOptions{KeepRealOnClientClose: false})
	session := startProxySession(t, proxy)
	defer session.closeInput()

	session.writeRequest(1, "initialize", map[string]any{})
	readResponse(t, session.reader, "initialize")
	session.writeRequest(2, "tools/list", map[string]any{})
	readResponse(t, session.reader, "tools/list")
	notif := readMessage(t, session.reader, "server notification")
	if notif.Method != "notifications/tools/list_changed" {
		t.Fatalf("notification method = %q", notif.Method)
	}
	session.closeInput()
	session.wait()
}

func TestStreamableHTTPProxyForwardsToolsListJSON(t *testing.T) {
	backend := newTestHTTPMCP(t, "streamable-json")
	defer backend.Close()

	cfg := httpTestConfig("fake", backend.URL, "streamable-http")
	proxy := NewProxy(cfg, log.New(testWriter{t: t}, "", 0))
	session := startProxySession(t, proxy)
	defer session.closeInput()

	session.writeRequest(1, "initialize", map[string]any{})
	readResponse(t, session.reader, "initialize")
	session.writeRequest(2, "tools/list", map[string]any{})
	assertEchoTool(t, readResponse(t, session.reader, "tools/list"))
	session.closeInput()
	session.wait()
}

func TestStreamableHTTPProxyForwardsToolsListSSE(t *testing.T) {
	backend := newTestHTTPMCP(t, "streamable-sse")
	defer backend.Close()

	cfg := httpTestConfig("fake", backend.URL, "streamable-http")
	proxy := NewProxy(cfg, log.New(testWriter{t: t}, "", 0))
	session := startProxySession(t, proxy)
	defer session.closeInput()

	session.writeRequest(1, "initialize", map[string]any{})
	readResponse(t, session.reader, "initialize")
	session.writeRequest(2, "tools/list", map[string]any{})
	assertEchoTool(t, readResponse(t, session.reader, "tools/list"))
	session.closeInput()
	session.wait()
}

func TestHTTPServerPostBridgesToProxy(t *testing.T) {
	backend := newTestHTTPMCP(t, "streamable-json")
	defer backend.Close()

	proxy := NewProxy(httpTestConfig("fake", backend.URL, "streamable-http"), log.New(testWriter{t: t}, "", 0))
	server := NewProxyHTTPServer(proxy, "127.0.0.1:0")
	if err := server.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer server.Stop()

	resp, err := http.Post("http://"+server.Addr()+"/", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	if err != nil {
		t.Fatalf("POST proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %s", resp.Status)
	}
	var msg jsonrpc.Message
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assertEchoTool(t, msg)
}

func TestHTTPServerSSEBridgesMessagesToProxy(t *testing.T) {
	backend := newTestHTTPMCP(t, "streamable-json")
	defer backend.Close()

	proxy := NewProxy(httpTestConfig("fake", backend.URL, "streamable-http"), log.New(testWriter{t: t}, "", 0))
	server := NewProxyHTTPServer(proxy, "127.0.0.1:0")
	if err := server.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer server.Stop()

	req, err := http.NewRequest(http.MethodGet, "http://"+server.Addr()+"/sse", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /sse: %v", err)
	}
	defer resp.Body.Close()

	reader := newSSEReader(resp.Body)
	endpoint, err := reader.Read()
	if err != nil {
		t.Fatalf("read endpoint: %v", err)
	}
	if endpoint.Event != "endpoint" {
		t.Fatalf("endpoint event = %q", endpoint.Event)
	}
	postURL := "http://" + server.Addr() + endpoint.Data
	postResp, err := http.Post(postURL, "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	if err != nil {
		t.Fatalf("POST session: %v", err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST session status = %s", postResp.Status)
	}
	event, err := reader.Read()
	if err != nil {
		t.Fatalf("read response event: %v", err)
	}
	var msg jsonrpc.Message
	if err := json.Unmarshal([]byte(event.Data), &msg); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	assertEchoTool(t, msg)
}

func httpTestConfig(name, targetURL, protocol string) Config {
	return Config{
		Name:           name,
		URL:            targetURL,
		Protocol:       protocol,
		DisableCache:   true,
		IdleTimeout:    Duration{Duration: time.Second},
		StartupTimeout: Duration{Duration: 5 * time.Second},
		CallTimeout:    Duration{Duration: 5 * time.Second},
	}
}

func assertEchoTool(t *testing.T, msg jsonrpc.Message) {
	t.Helper()
	if msg.Error != nil {
		t.Fatalf("response error = %#v", msg.Error)
	}
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(msg.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Tools) != 1 || result.Tools[0].Name != "echo" {
		t.Fatalf("tools = %#v", result.Tools)
	}
}

type testHTTPMCP struct {
	*httptest.Server
	mode               string
	notifyToolsChanged bool
	mu                 sync.Mutex
	headers            map[string]string
	sessions           map[string]chan jsonrpc.Message
}

func newTestHTTPMCP(t *testing.T, mode string) *testHTTPMCP {
	t.Helper()
	m := &testHTTPMCP{
		mode:     mode,
		headers:  map[string]string{},
		sessions: map[string]chan jsonrpc.Message{},
	}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

func (m *testHTTPMCP) handle(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	for key, values := range r.Header {
		if len(values) > 0 {
			m.headers[key] = values[0]
		}
	}
	m.mu.Unlock()

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/sse":
		m.handleSSE(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/messages":
		m.handleSSEPost(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/":
		m.handleStreamable(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (m *testHTTPMCP) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	sessionID := fmt.Sprintf("%d", time.Now().UnixNano())
	ch := make(chan jsonrpc.Message, 16)
	m.mu.Lock()
	m.sessions[sessionID] = ch
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.sessions, sessionID)
		m.mu.Unlock()
	}()
	fmt.Fprintf(w, "event: endpoint\ndata: /messages?sessionId=%s\n\n", sessionID)
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			writeSSEEvent(w, flusher, msg)
		}
	}
}

func (m *testHTTPMCP) handleSSEPost(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	m.mu.Lock()
	ch := m.sessions[sessionID]
	m.mu.Unlock()
	if ch == nil {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}
	var msg jsonrpc.Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if msg.IsNotification() {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	resp, notify := testHTTPResponse(msg, m.notifyToolsChanged)
	ch <- resp
	if notify != nil {
		ch <- *notify
	}
	w.WriteHeader(http.StatusAccepted)
}

func (m *testHTTPMCP) handleStreamable(w http.ResponseWriter, r *http.Request) {
	var msg jsonrpc.Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if msg.IsNotification() {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	resp, notify := testHTTPResponse(msg, m.notifyToolsChanged)
	if m.mode == "streamable-sse" {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEEvent(w, flusher, resp)
		if notify != nil {
			writeSSEEvent(w, flusher, *notify)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (m *testHTTPMCP) header(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.headers[name]
}

func testHTTPResponse(msg jsonrpc.Message, notifyToolsChanged bool) (jsonrpc.Message, *jsonrpc.Message) {
	switch msg.Method {
	case "initialize":
		return jsonrpc.Response(msg.ID, map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{"name": "fake-mcp", "version": "0.1.0"},
		}), nil
	case "tools/list":
		resp := jsonrpc.Response(msg.ID, map[string]any{
			"tools": []map[string]any{
				{"name": "echo", "description": "Echo test tool", "inputSchema": map[string]any{"type": "object"}},
			},
		})
		if notifyToolsChanged {
			notify := jsonrpc.Message{JSONRPC: "2.0", Method: "notifications/tools/list_changed"}
			return resp, &notify
		}
		return resp, nil
	default:
		return jsonrpc.ErrorResponse(msg.ID, -32601, "method not found"), nil
	}
}

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, msg jsonrpc.Message) {
	data, _ := json.Marshal(msg)
	fmt.Fprintf(w, "event: message\ndata: %s\n\n", strings.ReplaceAll(string(data), "\n", "\ndata: "))
	flusher.Flush()
}
