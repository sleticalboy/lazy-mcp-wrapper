package oauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

type LoginOptions struct {
	Name         string
	ServerURL    string
	ClientID     string
	TokenURL     string
	Resource     string
	Scopes       []string
	Store        *FileStore
	CallbackPort int
	OpenBrowser  bool
	OpenURL      func(string) error
	HTTPClient   *http.Client
	Out          io.Writer
}

func Login(ctx context.Context, opts LoginOptions) (Status, error) {
	if strings.TrimSpace(opts.Name) == "" {
		return Status{}, fmt.Errorf("name is required")
	}
	if strings.TrimSpace(opts.ServerURL) == "" {
		return Status{}, fmt.Errorf("server url is required")
	}
	if opts.Store == nil {
		opts.Store = NewFileStore("")
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.OpenURL == nil {
		opts.OpenURL = openBrowser
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}

	receiver, err := newCodeReceiver(opts.CallbackPort)
	if err != nil {
		return Status{}, err
	}
	receiver.out = opts.Out
	if opts.OpenBrowser {
		receiver.openURL = opts.OpenURL
	}
	defer receiver.close()

	redirectURL := receiver.redirectURL()
	authHandler, err := newAuthorizationCodeHandler(opts, redirectURL, receiver.fetch)
	if err != nil {
		return Status{}, err
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "lazy-mcp-wrapper", Version: "0.1.0"}, &mcp.ClientOptions{
		Capabilities: &mcp.ClientCapabilities{},
	})
	transport := &mcp.StreamableClientTransport{
		Endpoint:     opts.ServerURL,
		HTTPClient:   opts.HTTPClient,
		OAuthHandler: authHandler,
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return Status{}, err
	}
	defer session.Close()
	if _, err := session.ListTools(ctx, nil); err != nil {
		return Status{}, err
	}

	tokenSource, err := authHandler.TokenSource(ctx)
	if err != nil {
		return Status{}, err
	}
	if tokenSource == nil {
		return Status{}, fmt.Errorf("authorization completed without token source")
	}
	token, err := tokenSource.Token()
	if err != nil {
		return Status{}, err
	}
	if token == nil || token.AccessToken == "" {
		return Status{}, fmt.Errorf("authorization completed without access token")
	}

	cred := Credential{
		Name:         opts.Name,
		ServerURL:    opts.ServerURL,
		ClientID:     opts.ClientID,
		AuthURL:      receiver.authURL(),
		TokenURL:     firstNonEmpty(opts.TokenURL, inferTokenURL(receiver.authURL())),
		TokenAuth:    tokenAuthStyleName(0),
		Resource:     opts.Resource,
		Scopes:       opts.Scopes,
		TokenType:    token.TokenType,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
	}
	if err := opts.Store.Save(cred); err != nil {
		return Status{}, err
	}
	return opts.Store.Status(opts.Name)
}

func newAuthorizationCodeHandler(opts LoginOptions, redirectURL string, fetcher mcpauth.AuthorizationCodeFetcher) (*mcpauth.AuthorizationCodeHandler, error) {
	cfg := &mcpauth.AuthorizationCodeHandlerConfig{
		RedirectURL:              redirectURL,
		AuthorizationCodeFetcher: fetcher,
		Client:                   opts.HTTPClient,
	}
	if opts.ClientID != "" {
		cfg.PreregisteredClient = &oauthex.ClientCredentials{ClientID: opts.ClientID}
		return mcpauth.NewAuthorizationCodeHandler(cfg)
	}
	scope := strings.Join(opts.Scopes, " ")
	cfg.DynamicClientRegistrationConfig = &mcpauth.DynamicClientRegistrationConfig{
		Metadata: &oauthex.ClientRegistrationMetadata{
			ClientName:   "lazy-mcp-wrapper",
			RedirectURIs: []string{redirectURL},
			Scope:        scope,
		},
	}
	return mcpauth.NewAuthorizationCodeHandler(cfg)
}

type codeReceiver struct {
	listener        net.Listener
	server          *http.Server
	results         chan *mcpauth.AuthorizationResult
	errors          chan error
	openURL         func(string) error
	out             io.Writer
	capturedAuthURL string
}

func newCodeReceiver(port int) (*codeReceiver, error) {
	addr := "127.0.0.1:0"
	if port > 0 {
		addr = fmt.Sprintf("127.0.0.1:%d", port)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen for OAuth callback: %w", err)
	}
	r := &codeReceiver{
		listener: listener,
		results:  make(chan *mcpauth.AuthorizationResult, 1),
		errors:   make(chan error, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", r.handleCallback)
	r.server = &http.Server{Handler: mux}
	go func() {
		if err := r.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.errors <- err
		}
	}()
	return r, nil
}

func (r *codeReceiver) redirectURL() string {
	return "http://" + r.listener.Addr().String() + "/callback"
}

func (r *codeReceiver) fetch(ctx context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
	r.capturedAuthURL = args.URL
	if r.openURL != nil {
		if err := r.openURL(args.URL); err != nil {
			fmt.Fprintf(r.out, "Open this URL to authorize:\n%s\n", args.URL)
		}
	} else {
		fmt.Fprintf(r.out, "Open this URL to authorize:\n%s\n", args.URL)
	}
	select {
	case result := <-r.results:
		return result, nil
	case err := <-r.errors:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *codeReceiver) authURL() string {
	return r.capturedAuthURL
}

func (r *codeReceiver) handleCallback(w http.ResponseWriter, req *http.Request) {
	if errMsg := req.URL.Query().Get("error"); errMsg != "" {
		r.errors <- fmt.Errorf("authorization failed: %s", errMsg)
		http.Error(w, "Authorization failed", http.StatusBadRequest)
		return
	}
	result := &mcpauth.AuthorizationResult{
		Code:  req.URL.Query().Get("code"),
		State: req.URL.Query().Get("state"),
	}
	if result.Code == "" || result.State == "" {
		http.Error(w, "Missing OAuth code or state", http.StatusBadRequest)
		return
	}
	r.results <- result
	fmt.Fprint(w, "Authentication successful. You can close this window.")
}

func (r *codeReceiver) close() {
	if r.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = r.server.Shutdown(ctx)
}

func openBrowser(rawURL string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", rawURL).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	default:
		return exec.Command("xdg-open", rawURL).Start()
	}
}

func inferTokenURL(authURL string) string {
	if authURL == "" {
		return ""
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		return ""
	}
	if strings.HasSuffix(parsed.Path, "/authorize") {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/authorize") + "/token"
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
