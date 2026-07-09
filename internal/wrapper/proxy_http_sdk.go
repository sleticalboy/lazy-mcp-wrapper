package wrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/jsonrpc"
	oauthstore "github.com/binlee/lazy-mcp-wrapper/internal/oauth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type realSDKHTTPClient struct {
	cfg     Config
	log     *log.Logger
	session *mcp.ClientSession
	done    chan struct{}

	mu       sync.Mutex
	lastUsed time.Time
	timer    *time.Timer
	closed   bool

	subsMu sync.Mutex
	subs   []chan jsonrpc.Message
}

func startSDKHTTPReal(ctx context.Context, cfg Config, logger *log.Logger, init initRequest) (*realSDKHTTPClient, error) {
	startCtx, cancel := context.WithTimeout(ctx, cfg.StartupTimeout.Duration)
	defer cancel()

	impl := implementationFromInit(init)
	real := &realSDKHTTPClient{
		cfg:  cfg,
		log:  logger,
		done: make(chan struct{}),
	}
	client := mcp.NewClient(impl, &mcp.ClientOptions{
		Capabilities: &mcp.ClientCapabilities{},
		ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
			real.broadcastSDKNotification("notifications/tools/list_changed", nil)
		},
		PromptListChangedHandler: func(context.Context, *mcp.PromptListChangedRequest) {
			real.broadcastSDKNotification("notifications/prompts/list_changed", nil)
		},
		ResourceListChangedHandler: func(context.Context, *mcp.ResourceListChangedRequest) {
			real.broadcastSDKNotification("notifications/resources/list_changed", nil)
		},
		ResourceUpdatedHandler: func(_ context.Context, req *mcp.ResourceUpdatedNotificationRequest) {
			real.broadcastSDKNotification("notifications/resources/updated", req.Params)
		},
		LoggingMessageHandler: func(_ context.Context, req *mcp.LoggingMessageRequest) {
			real.broadcastSDKNotification("notifications/message", req.Params)
		},
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			real.broadcastSDKNotification("notifications/progress", req.Params)
		},
	})
	transport := &mcp.StreamableClientTransport{
		Endpoint:   cfg.URL,
		HTTPClient: &http.Client{Transport: headerRoundTripper{base: http.DefaultTransport, headers: cfg.Headers}},
	}
	if cfg.RequiresOAuth() {
		storeDir := cfg.OAuthStoreDir
		if storeDir == "" {
			storeDir = oauthstore.DefaultDir("")
		}
		transport.OAuthHandler = oauthstore.NewStoredTokenHandlerWithBinding(&oauthstore.FileStore{Dir: storeDir}, cfg.Name, oauthstore.CredentialBinding{
			ServerURL: cfg.URL,
			ClientID:  cfg.OAuthClientID,
			Resource:  cfg.OAuthResource,
			Scopes:    cfg.OAuthScopes,
		})
	}
	session, err := client.Connect(startCtx, transport, nil)
	if err != nil {
		return nil, err
	}
	real.session = session
	go func() {
		err := session.Wait()
		logger.Printf("SDK HTTP MCP %s exited: %v", cfg.Name, err)
		close(real.done)
	}()
	real.touch()
	logger.Printf("started SDK HTTP MCP %s url=%s", cfg.Name, cfg.URL)
	return real, nil
}

func implementationFromInit(init initRequest) *mcp.Implementation {
	impl := &mcp.Implementation{Name: "lazy-mcp-wrapper", Version: "0.1.0"}
	if init.ClientInfo == nil {
		return impl
	}
	if name, ok := init.ClientInfo["name"].(string); ok && name != "" {
		impl.Name = name
	}
	if version, ok := init.ClientInfo["version"].(string); ok && version != "" {
		impl.Version = version
	}
	return impl
}

type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (rt headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.base == nil {
		rt.base = http.DefaultTransport
	}
	if len(rt.headers) > 0 {
		req = req.Clone(req.Context())
		for key, value := range rt.headers {
			req.Header.Set(key, value)
		}
	}
	return rt.base.RoundTrip(req)
}

func (c *realSDKHTTPClient) call(ctx context.Context, method string, params json.RawMessage) (jsonrpc.Message, func(), error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return jsonrpc.Message{}, nil, fmt.Errorf("SDK HTTP MCP closed")
	}
	session := c.session
	c.mu.Unlock()

	c.log.Printf("calling SDK HTTP MCP %s method=%s", c.cfg.Name, method)
	result, errResp, err := c.callTyped(ctx, session, method, params)
	if err != nil {
		return jsonrpc.Message{}, nil, err
	}
	c.touch()
	if errResp != nil {
		return *errResp, func() {}, nil
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return jsonrpc.Message{}, nil, err
	}
	return jsonrpc.Response(nil, json.RawMessage(raw)), func() {}, nil
}

func (c *realSDKHTTPClient) callTyped(ctx context.Context, session *mcp.ClientSession, method string, params json.RawMessage) (any, *jsonrpc.Message, error) {
	switch method {
	case "ping":
		if err := session.Ping(ctx, decodeParams[mcp.PingParams](params)); err != nil {
			return nil, nil, err
		}
		return map[string]any{}, nil, nil
	case "tools/list":
		return sdkResult(session.ListTools(ctx, decodeParams[mcp.ListToolsParams](params)))
	case "tools/call":
		return sdkResult(session.CallTool(ctx, decodeParams[mcp.CallToolParams](params)))
	case "prompts/list":
		return sdkResult(session.ListPrompts(ctx, decodeParams[mcp.ListPromptsParams](params)))
	case "prompts/get":
		return sdkResult(session.GetPrompt(ctx, decodeParams[mcp.GetPromptParams](params)))
	case "resources/list":
		return sdkResult(session.ListResources(ctx, decodeParams[mcp.ListResourcesParams](params)))
	case "resources/read":
		return sdkResult(session.ReadResource(ctx, decodeParams[mcp.ReadResourceParams](params)))
	case "resources/templates/list":
		return sdkResult(session.ListResourceTemplates(ctx, decodeParams[mcp.ListResourceTemplatesParams](params)))
	case "completion/complete":
		return sdkResult(session.Complete(ctx, decodeParams[mcp.CompleteParams](params)))
	case "logging/setLevel":
		if err := session.SetLoggingLevel(ctx, decodeParams[mcp.SetLoggingLevelParams](params)); err != nil {
			return nil, nil, err
		}
		return map[string]any{}, nil, nil
	case "resources/subscribe":
		if err := session.Subscribe(ctx, decodeParams[mcp.SubscribeParams](params)); err != nil {
			return nil, nil, err
		}
		return map[string]any{}, nil, nil
	case "resources/unsubscribe":
		if err := session.Unsubscribe(ctx, decodeParams[mcp.UnsubscribeParams](params)); err != nil {
			return nil, nil, err
		}
		return map[string]any{}, nil, nil
	default:
		resp := jsonrpc.ErrorResponse(nil, errMethod, fmt.Sprintf("method not found: %s", method))
		return nil, &resp, nil
	}
}

func sdkResult[T any](result *T, err error) (any, *jsonrpc.Message, error) {
	if err != nil {
		return nil, nil, err
	}
	return result, nil, nil
}

func decodeParams[T any](raw json.RawMessage) *T {
	var params T
	if len(raw) == 0 {
		return &params
	}
	_ = json.Unmarshal(raw, &params)
	return &params
}

func (c *realSDKHTTPClient) sendNotification(msg jsonrpc.Message) error {
	switch msg.Method {
	case "notifications/initialized", "notifications/cancelled":
		c.touch()
		return nil
	default:
		return fmt.Errorf("SDK HTTP backend does not support client notification %s", msg.Method)
	}
}

func (c *realSDKHTTPClient) subscribe() chan jsonrpc.Message {
	ch := make(chan jsonrpc.Message, 16)
	c.subsMu.Lock()
	c.subs = append(c.subs, ch)
	c.subsMu.Unlock()
	return ch
}

func (c *realSDKHTTPClient) unsubscribe(ch chan jsonrpc.Message) {
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

func (c *realSDKHTTPClient) broadcastSDKNotification(method string, params any) {
	msg := jsonrpc.Message{JSONRPC: "2.0", Method: method}
	if params != nil {
		msg.Params = mustRaw(params)
	}
	c.broadcastNotification(msg)
}

func (c *realSDKHTTPClient) broadcastNotification(msg jsonrpc.Message) {
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
			c.log.Printf("dropping SDK HTTP server notification for slow subscriber method=%s", msg.Method)
		}
	}
}

func (c *realSDKHTTPClient) alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

func (c *realSDKHTTPClient) touch() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastUsed = time.Now()
	if c.timer == nil {
		c.timer = time.AfterFunc(c.cfg.IdleTimeout.Duration, func() {
			c.log.Printf("idle timeout reached, stopping SDK HTTP MCP %s", c.cfg.Name)
			_ = c.close()
		})
		return
	}
	c.timer.Reset(c.cfg.IdleTimeout.Duration)
}

func (c *realSDKHTTPClient) close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	if c.timer != nil {
		c.timer.Stop()
	}
	session := c.session
	c.mu.Unlock()

	if session == nil {
		return nil
	}
	err := session.Close()
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
	}
	return err
}

func (c *realSDKHTTPClient) pid() int {
	return 0
}

func (c *realSDKHTTPClient) lastUsedAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastUsed
}
