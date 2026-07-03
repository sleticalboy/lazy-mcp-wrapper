package wrapper

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/jsonrpc"
)

type realHTTPClient struct {
	cfg      Config
	log      *log.Logger
	client   *http.Client
	init     initRequest
	callMu   sync.Mutex
	mu       sync.Mutex
	conn     *httpBackendConn
	nextID   int64
	lastUsed time.Time
	timer    *time.Timer
	closed   bool
	done     chan struct{}
	subsMu   sync.Mutex
	subs     []chan jsonrpc.Message
}

type httpBackendConn struct {
	postURL string
	pending map[string]chan httpResponse
	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
}

type httpResponse struct {
	msg jsonrpc.Message
	ack chan struct{}
	err error
}

func startHTTPReal(ctx context.Context, cfg Config, logger *log.Logger, init initRequest) (*realHTTPClient, error) {
	startCtx, cancel := context.WithTimeout(ctx, cfg.StartupTimeout.Duration)
	defer cancel()

	client := &realHTTPClient{
		cfg:    cfg,
		log:    logger,
		client: &http.Client{},
		init:   init,
		done:   make(chan struct{}),
	}
	if err := client.initialize(startCtx, init); err != nil {
		_ = client.close()
		return nil, err
	}
	client.touch()
	logger.Printf("started HTTP MCP %s url=%s protocol=%s", cfg.Name, cfg.URL, cfg.HTTPProtocol())
	return client, nil
}

func (c *realHTTPClient) initialize(ctx context.Context, init initRequest) error {
	if init.ProtocolVersion == "" {
		init.ProtocolVersion = "2024-11-05"
	}
	if init.ClientInfo == nil {
		init.ClientInfo = map[string]any{
			"name":    "lazy-mcp-wrapper",
			"version": "0.1.0",
		}
	}
	if init.Capabilities == nil {
		init.Capabilities = map[string]any{}
	}
	params := map[string]any{
		"protocolVersion": init.ProtocolVersion,
		"clientInfo":      init.ClientInfo,
		"capabilities":    init.Capabilities,
	}
	c.log.Printf("sending initialize to HTTP MCP %s protocol=%s", c.cfg.Name, init.ProtocolVersion)
	_, release, err := c.call(ctx, "initialize", mustRaw(params))
	if err != nil {
		return err
	}
	release()
	c.log.Printf("initialize completed for HTTP MCP %s", c.cfg.Name)
	return c.sendNotification(jsonrpc.Message{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	})
}

func (c *realHTTPClient) call(ctx context.Context, method string, params json.RawMessage) (jsonrpc.Message, func(), error) {
	c.callMu.Lock()
	defer c.callMu.Unlock()

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return jsonrpc.Message{}, nil, fmt.Errorf("HTTP MCP closed")
	}
	c.nextID++
	id := c.nextID
	c.mu.Unlock()

	idRaw := mustRaw(id)
	req := jsonrpc.Message{
		JSONRPC: "2.0",
		ID:      idRaw,
		Method:  method,
		Params:  params,
	}
	if len(req.Params) == 0 {
		req.Params = mustRaw(map[string]any{})
	}

	c.log.Printf("calling HTTP MCP %s method=%s id=%d", c.cfg.Name, method, id)
	var (
		resp    jsonrpc.Message
		release = func() {}
		err     error
	)
	if c.cfg.HTTPProtocol() == "streamable-http" {
		resp, err = c.callStreamableHTTP(ctx, req)
	} else {
		resp, release, err = c.callSSE(ctx, req)
	}
	if err != nil {
		return jsonrpc.Message{}, nil, err
	}
	c.touch()
	c.log.Printf("HTTP MCP %s responded method=%s id=%d has_error=%v", c.cfg.Name, method, id, resp.Error != nil)
	if resp.JSONRPC == "" {
		resp.JSONRPC = "2.0"
	}
	return resp, release, nil
}

func (c *realHTTPClient) sendNotification(msg jsonrpc.Message) error {
	msg.ID = nil
	if msg.JSONRPC == "" {
		msg.JSONRPC = "2.0"
	}
	var err error
	if c.cfg.HTTPProtocol() == "streamable-http" {
		err = c.postStreamableHTTP(context.Background(), msg)
	} else {
		err = c.postSSENotification(context.Background(), msg)
	}
	if err != nil {
		return err
	}
	c.touch()
	return nil
}

func (c *realHTTPClient) callSSE(ctx context.Context, req jsonrpc.Message) (jsonrpc.Message, func(), error) {
	conn, err := c.ensureSSEConn(ctx)
	if err != nil {
		return jsonrpc.Message{}, nil, err
	}

	respCh := make(chan httpResponse, 1)
	id := string(req.ID)
	conn.mu.Lock()
	conn.pending[id] = respCh
	conn.mu.Unlock()

	removePending := func() {
		conn.mu.Lock()
		delete(conn.pending, id)
		conn.mu.Unlock()
	}
	if err := c.postJSON(ctx, conn.postURL, req); err != nil {
		removePending()
		return jsonrpc.Message{}, nil, err
	}

	select {
	case <-ctx.Done():
		removePending()
		return jsonrpc.Message{}, nil, ctx.Err()
	case <-c.done:
		removePending()
		return jsonrpc.Message{}, nil, fmt.Errorf("HTTP MCP closed")
	case resp := <-respCh:
		if resp.err != nil {
			return jsonrpc.Message{}, nil, resp.err
		}
		release := func() {}
		if resp.ack != nil {
			release = func() { close(resp.ack) }
		}
		return resp.msg, release, nil
	}
}

func (c *realHTTPClient) postSSENotification(ctx context.Context, msg jsonrpc.Message) error {
	conn, err := c.ensureSSEConn(ctx)
	if err != nil {
		return err
	}
	return c.postJSON(ctx, conn.postURL, msg)
}

func (c *realHTTPClient) ensureSSEConn(ctx context.Context) (*httpBackendConn, error) {
	c.mu.Lock()
	if c.conn != nil {
		conn := c.conn
		c.mu.Unlock()
		return conn, nil
	}
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("HTTP MCP closed")
	}
	connCtx, cancel := context.WithCancel(context.Background())
	conn := &httpBackendConn{
		pending: make(map[string]chan httpResponse),
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	c.conn = conn
	c.mu.Unlock()

	postURL, body, err := c.openSSE(ctx, connCtx)
	if err != nil {
		cancel()
		c.mu.Lock()
		if c.conn == conn {
			c.conn = nil
		}
		c.mu.Unlock()
		return nil, err
	}
	conn.postURL = postURL
	go c.readSSELoop(conn, body)
	return conn, nil
}

func (c *realHTTPClient) openSSE(ctx, connCtx context.Context) (string, io.ReadCloser, error) {
	sseURL, err := joinURLPath(c.cfg.URL, "sse")
	if err != nil {
		return "", nil, err
	}
	openCtx, openCancel := context.WithCancel(connCtx)
	endpointReady := make(chan struct{})
	defer close(endpointReady)
	go func() {
		select {
		case <-ctx.Done():
			openCancel()
		case <-endpointReady:
		case <-connCtx.Done():
		}
	}()

	req, err := http.NewRequestWithContext(openCtx, http.MethodGet, sseURL, nil)
	if err != nil {
		return "", nil, err
	}
	c.applyHeaders(req)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return "", nil, fmt.Errorf("SSE connect failed: %s", resp.Status)
	}
	reader := newSSEReader(resp.Body)
	for {
		event, err := reader.Read()
		if err != nil {
			openCancel()
			_ = resp.Body.Close()
			if ctx.Err() != nil {
				return "", nil, ctx.Err()
			}
			return "", nil, err
		}
		if event.Event != "endpoint" {
			continue
		}
		postURL, err := resolveURL(c.cfg.URL, strings.TrimSpace(event.Data))
		if err != nil {
			openCancel()
			_ = resp.Body.Close()
			return "", nil, err
		}
		return postURL, resp.Body, nil
	}
}

func (c *realHTTPClient) readSSELoop(conn *httpBackendConn, body io.ReadCloser) {
	defer body.Close()
	defer close(conn.done)
	defer c.dropConn(conn, fmt.Errorf("SSE connection closed"))

	reader := newSSEReader(body)
	for {
		event, err := reader.Read()
		if err != nil {
			if !errorsIsClosed(c.done) {
				c.log.Printf("HTTP MCP SSE read loop stopped: %v", err)
			}
			return
		}
		if strings.TrimSpace(event.Data) == "" {
			continue
		}
		var msg jsonrpc.Message
		if err := json.Unmarshal([]byte(event.Data), &msg); err != nil {
			c.log.Printf("HTTP MCP SSE invalid JSON event=%s error=%v", event.Event, err)
			continue
		}
		c.handleServerMessage(conn, msg)
	}
}

func (c *realHTTPClient) handleServerMessage(conn *httpBackendConn, msg jsonrpc.Message) {
	if msg.IsRequest() {
		c.log.Printf("warning: ignoring HTTP server-initiated request method=%s id=%s", msg.Method, string(msg.ID))
		return
	}
	if msg.IsNotification() {
		c.broadcastNotification(msg)
		return
	}
	id := string(msg.ID)
	conn.mu.Lock()
	ch := conn.pending[id]
	delete(conn.pending, id)
	conn.mu.Unlock()
	if ch == nil {
		c.log.Printf("dropping unmatched HTTP response id=%s", id)
		return
	}
	ack := make(chan struct{})
	ch <- httpResponse{msg: msg, ack: ack}
	select {
	case <-ack:
	case <-c.done:
	case <-time.After(5 * time.Second):
		c.log.Printf("warning: timed out waiting for HTTP response write ack id=%s", id)
	}
}

func (c *realHTTPClient) dropConn(conn *httpBackendConn, err error) {
	c.mu.Lock()
	if c.conn == conn {
		c.conn = nil
	}
	closed := c.closed
	c.mu.Unlock()

	conn.mu.Lock()
	pending := conn.pending
	conn.pending = map[string]chan httpResponse{}
	conn.mu.Unlock()

	if closed {
		err = fmt.Errorf("HTTP MCP closed")
	}
	for _, ch := range pending {
		select {
		case ch <- httpResponse{err: err}:
		default:
		}
	}
}

func (c *realHTTPClient) callStreamableHTTP(ctx context.Context, req jsonrpc.Message) (jsonrpc.Message, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return jsonrpc.Message{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return jsonrpc.Message{}, err
	}
	c.applyHeaders(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return jsonrpc.Message{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return jsonrpc.Message{}, fmt.Errorf("HTTP request failed: %s", resp.Status)
	}

	contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if contentType == "text/event-stream" {
		return c.readStreamableSSEResponse(ctx, resp.Body, string(req.ID))
	}
	var msg jsonrpc.Message
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return jsonrpc.Message{}, err
	}
	return msg, nil
}

func (c *realHTTPClient) postStreamableHTTP(ctx context.Context, msg jsonrpc.Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	c.applyHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP notification failed: %s", resp.Status)
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *realHTTPClient) readStreamableSSEResponse(ctx context.Context, body io.Reader, wantID string) (jsonrpc.Message, error) {
	reader := newSSEReader(body)
	for {
		select {
		case <-ctx.Done():
			return jsonrpc.Message{}, ctx.Err()
		default:
		}
		event, err := reader.Read()
		if err != nil {
			return jsonrpc.Message{}, err
		}
		if strings.TrimSpace(event.Data) == "" {
			continue
		}
		var msg jsonrpc.Message
		if err := json.Unmarshal([]byte(event.Data), &msg); err != nil {
			return jsonrpc.Message{}, err
		}
		if msg.IsRequest() {
			c.log.Printf("warning: ignoring HTTP server-initiated request method=%s id=%s", msg.Method, string(msg.ID))
			continue
		}
		if msg.IsNotification() {
			c.broadcastNotification(msg)
			continue
		}
		if string(msg.ID) == wantID {
			return msg, nil
		}
		c.log.Printf("dropping unmatched HTTP response id=%s", string(msg.ID))
	}
}

func (c *realHTTPClient) postJSON(ctx context.Context, target string, msg jsonrpc.Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	c.applyHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP POST failed: %s", resp.Status)
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *realHTTPClient) applyHeaders(req *http.Request) {
	for key, value := range c.cfg.Headers {
		req.Header.Set(key, value)
	}
}

func (c *realHTTPClient) subscribe() chan jsonrpc.Message {
	ch := make(chan jsonrpc.Message, 16)
	c.subsMu.Lock()
	c.subs = append(c.subs, ch)
	c.subsMu.Unlock()
	return ch
}

func (c *realHTTPClient) unsubscribe(ch chan jsonrpc.Message) {
	c.subsMu.Lock()
	defer c.subsMu.Unlock()
	for i, sub := range c.subs {
		if sub != ch {
			continue
		}
		c.subs = append(c.subs[:i], c.subs[i+1:]...)
		close(ch)
		return
	}
}

func (c *realHTTPClient) broadcastNotification(msg jsonrpc.Message) {
	if msg.JSONRPC == "" {
		msg.JSONRPC = "2.0"
	}
	msg.ID = nil

	c.subsMu.Lock()
	defer c.subsMu.Unlock()
	for _, sub := range c.subs {
		select {
		case sub <- msg:
		default:
			c.log.Printf("dropping HTTP server notification for slow subscriber method=%s", msg.Method)
		}
	}
}

func (c *realHTTPClient) alive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed
}

func (c *realHTTPClient) touch() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}
	c.lastUsed = time.Now()
	if c.timer == nil {
		c.timer = time.AfterFunc(c.cfg.IdleTimeout.Duration, func() {
			c.log.Printf("idle timeout reached, closing HTTP MCP connection %s", c.cfg.Name)
			c.closeSSEConn()
		})
		return
	}
	c.timer.Reset(c.cfg.IdleTimeout.Duration)
}

func (c *realHTTPClient) closeSSEConn() {
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()
	if conn != nil {
		conn.cancel()
	}
}

func (c *realHTTPClient) close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	if c.timer != nil {
		c.timer.Stop()
	}
	conn := c.conn
	c.conn = nil
	close(c.done)
	c.mu.Unlock()

	if conn != nil {
		conn.cancel()
		select {
		case <-conn.done:
		case <-time.After(2 * time.Second):
		}
	}
	return nil
}

func (c *realHTTPClient) pid() int {
	return 0
}

func (c *realHTTPClient) lastUsedAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastUsed
}

type sseEvent struct {
	Event string
	Data  string
}

type sseReader struct {
	r *bufio.Reader
}

func newSSEReader(r io.Reader) *sseReader {
	return &sseReader{r: bufio.NewReader(r)}
}

func (r *sseReader) Read() (sseEvent, error) {
	var event sseEvent
	var data []string
	for {
		line, err := r.r.ReadString('\n')
		if err != nil {
			return sseEvent{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if event.Event == "" && len(data) == 0 {
				continue
			}
			event.Data = strings.Join(data, "\n")
			return event, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch name {
		case "event":
			event.Event = value
		case "data":
			data = append(data, value)
		}
	}
}

func joinURLPath(base, elem string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	ref, err := url.Parse(elem)
	if err != nil {
		return "", err
	}
	return u.ResolveReference(ref).String(), nil
}

func resolveURL(base, value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if u.IsAbs() {
		return u.String(), nil
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	return baseURL.ResolveReference(u).String(), nil
}

func errorsIsClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}
