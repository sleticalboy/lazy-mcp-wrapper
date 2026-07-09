package wrapper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/jsonrpc"
)

const (
	errParse      = -32700
	errInvalidReq = -32600
	errMethod     = -32601
	errInternal   = -32603
)

type Proxy struct {
	cfg            Config
	log            *log.Logger
	opts           ProxyOptions
	client         realBackend
	clientProtocol string
	clientInfo     map[string]any
	clientCaps     map[string]any
	mu             sync.Mutex
}

type realBackend interface {
	call(ctx context.Context, method string, params json.RawMessage) (jsonrpc.Message, func(), error)
	sendNotification(msg jsonrpc.Message) error
	subscribe() chan jsonrpc.Message
	unsubscribe(ch chan jsonrpc.Message)
	alive() bool
	touch()
	close() error
	pid() int
	lastUsedAt() time.Time
}

type ProxyOptions struct {
	KeepRealOnClientClose bool
	OnRequest             func(method string)
	OnResponse            func(method string, duration time.Duration, hasError bool, errorMessage string)
}

type ProxyStatus struct {
	Name       string     `json:"name"`
	HasReal    bool       `json:"has_real"`
	RealPID    int        `json:"real_pid,omitempty"`
	RealAlive  bool       `json:"real_alive"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func NewProxy(cfg Config, logger *log.Logger) *Proxy {
	return &Proxy{cfg: cfg, log: logger}
}

func NewProxyWithOptions(cfg Config, logger *log.Logger, opts ProxyOptions) *Proxy {
	return &Proxy{cfg: cfg, log: logger, opts: opts}
}

func (p *Proxy) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	reader := jsonrpc.NewReader(in)
	writer := newLockedWriter(out)
	notifUpdates := make(chan chan jsonrpc.Message, 4)
	notifDone := make(chan struct{})
	go p.notifForwarder(writer, notifUpdates, notifDone)

	var currentClient realBackend
	var currentSub chan jsonrpc.Message
	syncSubscription := func(client realBackend) {
		if client == nil || client == currentClient {
			return
		}
		if currentClient != nil && currentSub != nil {
			currentClient.unsubscribe(currentSub)
		}
		currentClient = client
		currentSub = client.subscribe()
		notifUpdates <- currentSub
	}
	cleanup := func() {
		close(notifUpdates)
		<-notifDone
		if currentClient != nil && currentSub != nil {
			currentClient.unsubscribe(currentSub)
		}
	}
	defer cleanup()

	for {
		msg, err := reader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if !p.opts.KeepRealOnClientClose {
					p.stopReal()
				}
				return nil
			}
			p.log.Printf("read client message failed: %v", err)
			return writer.Write(jsonrpc.ErrorResponse(nil, errParse, err.Error()))
		}

		if msg.IsNotification() {
			if err := p.handleNotification(ctx, msg, syncSubscription); err != nil {
				p.log.Printf("notification %s failed: %v", msg.Method, err)
			}
			continue
		}
		if !msg.IsRequest() {
			if reader.SeenFraming() {
				writer.upgradeFraming(out, reader.Framing())
			}
			_ = writer.Write(jsonrpc.ErrorResponse(msg.ID, errInvalidReq, "invalid JSON-RPC message"))
			continue
		}

		if p.opts.OnRequest != nil {
			p.opts.OnRequest(msg.Method)
		}
		var afterWrite func()
		start := time.Now()
		resp := p.handleRequest(ctx, msg, syncSubscription, &afterWrite)
		if p.opts.OnResponse != nil {
			errorMessage := ""
			if resp.Error != nil {
				errorMessage = resp.Error.Message
			}
			p.opts.OnResponse(msg.Method, time.Since(start), resp.Error != nil, errorMessage)
		}
		if reader.SeenFraming() {
			writer.upgradeFraming(out, reader.Framing())
		}
		if err := writer.Write(resp); err != nil {
			if afterWrite != nil {
				afterWrite()
			}
			if !p.opts.KeepRealOnClientClose {
				p.stopReal()
			}
			return err
		}
		if afterWrite != nil {
			afterWrite()
		}
	}
}

type lockedWriter struct {
	mu sync.Mutex
	w  *jsonrpc.Writer
}

func newLockedWriter(out io.Writer) *lockedWriter {
	return &lockedWriter{w: jsonrpc.NewWriter(out)}
}

func (lw *lockedWriter) Write(msg jsonrpc.Message) error {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.w.Write(msg)
}

func (lw *lockedWriter) upgradeFraming(out io.Writer, framing jsonrpc.Framing) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	lw.w = jsonrpc.NewWriterWithFraming(out, framing)
}

func (p *Proxy) notifForwarder(writer *lockedWriter, updates <-chan chan jsonrpc.Message, done chan<- struct{}) {
	defer close(done)
	var current chan jsonrpc.Message
	for {
		if current == nil {
			ch, ok := <-updates
			if !ok {
				return
			}
			current = ch
			continue
		}

		select {
		case ch, ok := <-updates:
			if !ok {
				return
			}
			current = ch
		case msg, ok := <-current:
			if !ok {
				current = nil
				continue
			}
			p.handleServerNotification(msg)
			if err := writer.Write(msg); err != nil {
				p.log.Printf("forward server notification failed method=%s error=%v", msg.Method, err)
			}
		}
	}
}

func (p *Proxy) handleServerNotification(msg jsonrpc.Message) {
	if msg.Method != "notifications/tools/list_changed" {
		return
	}
	if err := p.cfg.invalidateCachedToolsList(); err != nil {
		p.log.Printf("tools/list cache invalidation failed name=%s error=%v", p.cfg.Name, err)
		return
	}
	p.log.Printf("tools/list cache invalidated name=%s reason=server_notification", p.cfg.Name)
}

func (p *Proxy) handleRequest(ctx context.Context, msg jsonrpc.Message, onRealClient func(realBackend), afterWrite *func()) jsonrpc.Message {
	switch msg.Method {
	case "initialize":
		p.captureInitialize(msg.Params)
		return jsonrpc.Response(msg.ID, map[string]any{
			"protocolVersion": p.protocolVersion(),
			"serverInfo": map[string]any{
				"name":    "lazy-mcp-wrapper/" + p.cfg.Name,
				"version": "0.1.0",
			},
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"prompts":   map[string]any{},
				"resources": map[string]any{},
			},
		})
	case "ping":
		return jsonrpc.Response(msg.ID, map[string]any{})
	case "server/discover":
		return jsonrpc.Response(msg.ID, p.discovery())
	case "tools/list":
		return p.toolsList(ctx, msg, onRealClient, afterWrite)
	case "tools/call", "prompts/list", "prompts/get", "resources/list", "resources/read", "resources/templates/list":
		return p.forward(ctx, msg, onRealClient, afterWrite)
	case "notifications/initialized", "notifications/cancelled":
		return jsonrpc.Response(msg.ID, map[string]any{})
	default:
		return jsonrpc.ErrorResponse(msg.ID, errMethod, fmt.Sprintf("method not found: %s", msg.Method))
	}
}

func (p *Proxy) toolsList(ctx context.Context, msg jsonrpc.Message, onRealClient func(realBackend), afterWrite *func()) jsonrpc.Message {
	if cached, ok := p.cfg.readCachedToolsList(); ok {
		cached.ID = msg.ID
		p.log.Printf("tools/list cache hit name=%s", p.cfg.Name)
		return cached
	}

	resp := p.forward(ctx, msg, onRealClient, afterWrite)
	if resp.Error == nil && len(resp.Result) > 0 {
		if err := p.cfg.writeCachedToolsList(resp.Result); err != nil {
			p.log.Printf("tools/list cache write failed name=%s error=%v", p.cfg.Name, err)
		} else {
			p.log.Printf("tools/list cache refreshed name=%s", p.cfg.Name)
		}
	}
	return resp
}

func (p *Proxy) captureInitialize(params json.RawMessage) {
	var initParams struct {
		ProtocolVersion string         `json:"protocolVersion"`
		ClientInfo      map[string]any `json:"clientInfo"`
		Capabilities    map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal(params, &initParams); err != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clientProtocol = initParams.ProtocolVersion
	p.clientInfo = initParams.ClientInfo
	p.clientCaps = initParams.Capabilities
}

func (p *Proxy) protocolVersion() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.clientProtocol != "" {
		return p.clientProtocol
	}
	return "2024-11-05"
}

func (p *Proxy) handleNotification(ctx context.Context, msg jsonrpc.Message, onRealClient func(realBackend)) error {
	switch msg.Method {
	case "notifications/initialized", "notifications/cancelled":
		return nil
	default:
		client, err := p.ensureReal(ctx)
		if err != nil {
			return err
		}
		onRealClient(client)
		return client.sendNotification(msg)
	}
}

func (p *Proxy) forward(ctx context.Context, msg jsonrpc.Message, onRealClient func(realBackend), afterWrite *func()) jsonrpc.Message {
	client, err := p.ensureReal(ctx)
	if err != nil {
		return jsonrpc.ErrorResponse(msg.ID, errInternal, err.Error())
	}
	onRealClient(client)

	callCtx, cancel := context.WithTimeout(ctx, p.cfg.CallTimeout.Duration)
	defer cancel()

	resp, release, err := client.call(callCtx, msg.Method, msg.Params)
	if err != nil {
		return jsonrpc.ErrorResponse(msg.ID, errInternal, err.Error())
	}
	*afterWrite = release
	resp.ID = msg.ID
	if resp.JSONRPC == "" {
		resp.JSONRPC = "2.0"
	}
	return resp
}

func (p *Proxy) ensureReal(ctx context.Context) (realBackend, error) {
	if p.cfg.URL != "" {
		return p.ensureHTTPReal(ctx)
	}
	return p.ensureStdioReal(ctx)
}

func (p *Proxy) ensureStdioReal(ctx context.Context) (realBackend, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.client != nil && p.client.alive() {
		p.client.touch()
		return p.client, nil
	}
	if p.client != nil {
		_ = p.client.close()
		p.client = nil
	}

	init := initRequest{
		ProtocolVersion: p.clientProtocol,
		ClientInfo:      p.clientInfo,
		Capabilities:    p.clientCaps,
	}
	if p.cfg.RealProtocol != "" {
		init.ProtocolVersion = p.cfg.RealProtocol
	}
	if init.ProtocolVersion == "" {
		init.ProtocolVersion = "2024-11-05"
	}
	client, err := startReal(ctx, p.cfg, p.log, init)
	if err != nil {
		if p.cfg.LogFile != "" {
			return nil, fmt.Errorf("%w\n  check logs: tail -f %s", err, p.cfg.LogFile)
		}
		return nil, err
	}
	p.client = client
	return client, nil
}

func (p *Proxy) ensureHTTPReal(ctx context.Context) (realBackend, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.client != nil && p.client.alive() {
		p.client.touch()
		return p.client, nil
	}
	if p.client != nil {
		_ = p.client.close()
		p.client = nil
	}

	init := initRequest{
		ProtocolVersion: p.clientProtocol,
		ClientInfo:      p.clientInfo,
		Capabilities:    p.clientCaps,
	}
	if p.cfg.RealProtocol != "" {
		init.ProtocolVersion = p.cfg.RealProtocol
	}
	if init.ProtocolVersion == "" {
		init.ProtocolVersion = "2024-11-05"
	}
	var (
		client realBackend
		err    error
	)
	if p.cfg.StatelessHTTPUpstream() && p.cfg.UseSDKHTTPBackend() {
		return nil, fmt.Errorf("stateless HTTP upstream requires native http_backend and auth none")
	}
	if p.cfg.UseSDKHTTPBackend() {
		client, err = startSDKHTTPReal(ctx, p.cfg, p.log, init)
	} else {
		client, err = startHTTPReal(ctx, p.cfg, p.log, init)
	}
	if err != nil {
		if p.cfg.LogFile != "" {
			return nil, fmt.Errorf("%w\n  check logs: tail -f %s", err, p.cfg.LogFile)
		}
		return nil, err
	}
	p.client = client
	return client, nil
}

func (p *Proxy) stopReal() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client != nil {
		_ = p.client.close()
		p.client = nil
	}
}

func (p *Proxy) Close() {
	p.stopReal()
}

func (p *Proxy) Status() ProxyStatus {
	p.mu.Lock()
	defer p.mu.Unlock()

	status := ProxyStatus{Name: p.cfg.Name}
	if p.client == nil {
		return status
	}
	if !p.client.alive() {
		_ = p.client.close()
		p.client = nil
		return status
	}
	status.HasReal = true
	status.RealAlive = true
	status.RealPID = p.client.pid()
	if lastUsedAt := p.client.lastUsedAt(); !lastUsedAt.IsZero() {
		status.LastUsedAt = &lastUsedAt
	}
	return status
}

type realClient struct {
	cfg       Config
	log       *log.Logger
	cmd       *exec.Cmd
	writer    *jsonrpc.Writer
	framing   jsonrpc.Framing
	responses chan realResponse
	done      chan struct{}
	callMu    sync.Mutex
	mu        sync.Mutex
	nextID    int64
	timer     *time.Timer
	lastUsed  time.Time
	subsMu    sync.Mutex
	subs      []chan jsonrpc.Message
}

type realResponse struct {
	msg jsonrpc.Message
	ack chan struct{}
}

type initRequest struct {
	ProtocolVersion string
	ClientInfo      map[string]any
	Capabilities    map[string]any
}

func startReal(ctx context.Context, cfg Config, logger *log.Logger, init initRequest) (*realClient, error) {
	startCtx, cancel := context.WithTimeout(ctx, cfg.StartupTimeout.Duration)
	defer cancel()

	framing, err := cfg.Framing()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(cfg.Command, cfg.Args...)
	if cfg.CWD != "" {
		cmd.Dir = cfg.CWD
	}
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = logWriter{log: logger, prefix: "real stderr: "}

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	logger.Printf("spawned real MCP %s pid=%d command=%s args=%v", cfg.Name, cmd.Process.Pid, cfg.Command, redactArgs(cfg.Args))

	client := &realClient{
		cfg:       cfg,
		log:       logger,
		cmd:       cmd,
		writer:    jsonrpc.NewWriterWithFraming(stdin, framing),
		framing:   framing,
		responses: make(chan realResponse, 64),
		done:      make(chan struct{}),
	}

	go client.readLoop(stdout)
	go client.waitLoop()

	if err := client.initialize(startCtx, init); err != nil {
		_ = client.close()
		return nil, err
	}
	client.touch()
	logger.Printf("started real MCP %s pid=%d", cfg.Name, cmd.Process.Pid)
	return client, nil
}

func (c *realClient) initialize(ctx context.Context, init initRequest) error {
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
	c.log.Printf("sending initialize to real MCP %s protocol=%s", c.cfg.Name, init.ProtocolVersion)
	_, release, err := c.call(ctx, "initialize", mustRaw(params))
	if err != nil {
		return err
	}
	release()
	c.log.Printf("initialize completed for real MCP %s", c.cfg.Name)
	return c.sendNotification(jsonrpc.Message{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	})
}

func (c *realClient) call(ctx context.Context, method string, params json.RawMessage) (jsonrpc.Message, func(), error) {
	c.callMu.Lock()
	defer c.callMu.Unlock()

	c.mu.Lock()
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

	c.log.Printf("calling real MCP %s method=%s id=%d", c.cfg.Name, method, id)
	if err := c.writer.Write(req); err != nil {
		return jsonrpc.Message{}, nil, err
	}

	for {
		select {
		case <-ctx.Done():
			return jsonrpc.Message{}, nil, ctx.Err()
		case <-c.done:
			return jsonrpc.Message{}, nil, fmt.Errorf("real MCP exited")
		case realResp := <-c.responses:
			resp := realResp.msg
			if string(resp.ID) == string(idRaw) {
				c.touch()
				c.log.Printf("real MCP %s responded method=%s id=%d has_error=%v", c.cfg.Name, method, id, resp.Error != nil)
				return resp, func() { close(realResp.ack) }, nil
			}
			close(realResp.ack)
			c.log.Printf("dropping unmatched response id=%s", string(resp.ID))
		}
	}
}

func (c *realClient) sendNotification(msg jsonrpc.Message) error {
	msg.ID = nil
	if msg.JSONRPC == "" {
		msg.JSONRPC = "2.0"
	}
	c.touch()
	return c.writer.Write(msg)
}

func (c *realClient) readLoop(stdout io.Reader) {
	reader := jsonrpc.NewReaderWithFraming(stdout, c.framing)
	for {
		msg, err := reader.Read()
		if err != nil {
			c.log.Printf("real read loop stopped: %v", err)
			return
		}
		if msg.IsRequest() {
			c.log.Printf("warning: ignoring server-initiated request method=%s id=%s", msg.Method, string(msg.ID))
			continue
		}
		if msg.IsNotification() {
			c.broadcastNotification(msg)
			continue
		}
		ack := make(chan struct{})
		select {
		case c.responses <- realResponse{msg: msg, ack: ack}:
			select {
			case <-ack:
			case <-c.done:
				return
			case <-time.After(5 * time.Second):
				c.log.Printf("warning: timed out waiting for response write ack id=%s", string(msg.ID))
			}
		case <-c.done:
			return
		}
	}
}

func (c *realClient) subscribe() chan jsonrpc.Message {
	ch := make(chan jsonrpc.Message, 16)
	c.subsMu.Lock()
	c.subs = append(c.subs, ch)
	c.subsMu.Unlock()
	return ch
}

func (c *realClient) unsubscribe(ch chan jsonrpc.Message) {
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

func (c *realClient) broadcastNotification(msg jsonrpc.Message) {
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
			c.log.Printf("dropping server notification for slow subscriber method=%s", msg.Method)
		}
	}
}

func (c *realClient) waitLoop() {
	err := c.cmd.Wait()
	c.log.Printf("real MCP %s exited: %v", c.cfg.Name, err)
	close(c.done)
}

func (c *realClient) touch() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.lastUsed = time.Now()
	if c.timer == nil {
		c.timer = time.AfterFunc(c.cfg.IdleTimeout.Duration, func() {
			c.log.Printf("idle timeout reached, stopping real MCP %s", c.cfg.Name)
			_ = c.close()
		})
		return
	}
	c.timer.Reset(c.cfg.IdleTimeout.Duration)
}

func (c *realClient) alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

func (c *realClient) pid() int {
	if c.cmd == nil || c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}

func (c *realClient) lastUsedAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastUsed
}

func (c *realClient) close() error {
	c.mu.Lock()
	if c.timer != nil {
		c.timer.Stop()
	}
	c.mu.Unlock()

	if c.cmd.Process == nil {
		return nil
	}
	if err := c.signalStop(); err != nil {
		_ = c.cmd.Process.Kill()
		return err
	}

	select {
	case <-c.done:
		return nil
	case <-time.After(2 * time.Second):
		return c.cmd.Process.Kill()
	}
}

func mustRaw(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}

type logWriter struct {
	log    *log.Logger
	prefix string
}

func (w logWriter) Write(p []byte) (int, error) {
	w.log.Printf("%s%s", w.prefix, string(p))
	return len(p), nil
}
