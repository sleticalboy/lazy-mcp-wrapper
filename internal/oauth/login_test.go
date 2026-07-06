package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

func TestLoginAuthorizationCodeFlowStoresToken(t *testing.T) {
	var (
		mu          sync.Mutex
		sawBearer   string
		issuedCodes = map[string]bool{}
	)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := server.URL
		switch r.URL.Path {
		case "/mcp":
			if auth := r.Header.Get("Authorization"); auth != "Bearer access-token" {
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource"`, base))
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			mu.Lock()
			sawBearer = r.Header.Get("Authorization")
			mu.Unlock()
			var req struct {
				ID     any    `json:"id"`
				Method string `json:"method"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			switch req.Method {
			case "initialize":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result": map[string]any{
						"protocolVersion": "2025-06-18",
						"capabilities":    map[string]any{"tools": map[string]any{}},
						"serverInfo":      map[string]any{"name": "fake", "version": "0.1.0"},
					},
				})
			case "tools/list":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result":  map[string]any{"tools": []map[string]any{{"name": "echo", "inputSchema": map[string]any{"type": "object"}}}},
				})
			default:
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32601, "message": "not found"}})
			}
		case "/.well-known/oauth-protected-resource":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resource":              base + "/mcp",
				"authorization_servers": []string{base},
				"scopes_supported":      []string{"tools"},
			})
		case "/.well-known/oauth-authorization-server":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                                base,
				"authorization_endpoint":                base + "/authorize",
				"token_endpoint":                        base + "/token",
				"token_endpoint_auth_methods_supported": []string{"client_secret_post", "none"},
				"code_challenge_methods_supported":      []string{"S256"},
			})
		case "/authorize":
			redirectURI := r.URL.Query().Get("redirect_uri")
			state := r.URL.Query().Get("state")
			if redirectURI == "" || state == "" {
				http.Error(w, "bad authorization request", http.StatusBadRequest)
				return
			}
			code := "auth-code"
			mu.Lock()
			issuedCodes[code] = true
			mu.Unlock()
			callback, _ := url.Parse(redirectURI)
			values := callback.Query()
			values.Set("code", code)
			values.Set("state", state)
			callback.RawQuery = values.Encode()
			http.Redirect(w, r, callback.String(), http.StatusFound)
		case "/token":
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			code := r.Form.Get("code")
			mu.Lock()
			validCode := issuedCodes[code]
			mu.Unlock()
			if !validCode || r.Form.Get("code_verifier") == "" {
				http.Error(w, "invalid code", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-token",
				"refresh_token": "refresh-token",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	httpClient := server.Client()
	store := &FileStore{Dir: t.TempDir()}
	status, err := Login(context.Background(), LoginOptions{
		Name:        "remote",
		ServerURL:   server.URL + "/mcp",
		ClientID:    "test-client",
		Scopes:      []string{"tools"},
		Store:       store,
		HTTPClient:  httpClient,
		OpenBrowser: true,
		OpenURL: func(rawURL string) error {
			resp, err := httpClient.Get(rawURL)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if !status.Authenticated || !status.HasAccessToken || !status.HasRefreshToken || status.Expired {
		t.Fatalf("status = %#v", status)
	}
	if status.Expiry == nil || !status.Expiry.After(time.Now()) {
		t.Fatalf("expiry = %v", status.Expiry)
	}
	cred, err := store.Load("remote")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cred.AccessToken != "access-token" || cred.RefreshToken != "refresh-token" {
		t.Fatalf("credential token fields = %#v", cred)
	}
	if cred.TokenURL != server.URL+"/token" {
		t.Fatalf("TokenURL = %q", cred.TokenURL)
	}
	mu.Lock()
	gotBearer := sawBearer
	mu.Unlock()
	if gotBearer != "Bearer access-token" {
		t.Fatalf("MCP Authorization header = %q", gotBearer)
	}
}
