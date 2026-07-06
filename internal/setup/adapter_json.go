package setup

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type jsonAdapter struct {
	kind string
	path string
}

type jsonConfig struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

type jsonServer struct {
	Type          string            `json:"type,omitempty"`
	Auth          string            `json:"auth,omitempty"`
	OAuthResource string            `json:"oauth_resource,omitempty"`
	OAuthScopes   []string          `json:"scopes,omitempty"`
	OAuth         *jsonOAuthConfig  `json:"oauth,omitempty"`
	Command       string            `json:"command,omitempty"`
	Args          []string          `json:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	URL           string            `json:"url,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
}

type jsonOAuthConfig struct {
	ClientID string `json:"client_id,omitempty"`
}

func newJSONAdapter(kind, path string) ClientAdapter {
	return jsonAdapter{kind: kind, path: path}
}

func (a jsonAdapter) Kind() string {
	return a.kind
}

func (a jsonAdapter) ConfigPath() string {
	return a.path
}

func (a jsonAdapter) Installed() bool {
	_, err := os.Stat(a.path)
	return err == nil
}

func (a jsonAdapter) ReadServers() ([]RawServer, error) {
	data, err := os.ReadFile(a.path)
	if err != nil {
		return nil, err
	}
	return parseJSONMCPServers(data)
}

func parseJSONMCPServers(data []byte) ([]RawServer, error) {
	var cfg jsonConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(cfg.MCPServers))
	for name := range cfg.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	servers := make([]RawServer, 0, len(names))
	for _, name := range names {
		var server jsonServer
		if err := json.Unmarshal(cfg.MCPServers[name], &server); err != nil {
			return nil, err
		}
		oauthClientID := ""
		if server.OAuth != nil {
			oauthClientID = server.OAuth.ClientID
		}
		raw := RawServer{
			Name:          name,
			Type:          server.Type,
			Auth:          server.Auth,
			OAuthClientID: oauthClientID,
			OAuthResource: server.OAuthResource,
			OAuthScopes:   server.OAuthScopes,
			Command:       server.Command,
			Args:          server.Args,
			Env:           server.Env,
			URL:           server.URL,
			Headers:       server.Headers,
			Raw:           cfg.MCPServers[name],
		}
		raw.IsWrappable = isWrappable(raw)
		servers = append(servers, raw)
	}
	return servers, nil
}

func (a jsonAdapter) WriteServers(servers []RawServer, backupPath string) error {
	if backupPath != "" {
		data, err := os.ReadFile(a.path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(backupPath, data, 0644); err != nil {
			return err
		}
	}
	content, err := renderJSONConfig(a.path, servers)
	if err != nil {
		return err
	}
	return os.WriteFile(a.path, content, 0644)
}

func renderJSONConfig(path string, servers []RawServer) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	mcpServers := map[string]json.RawMessage{}
	for _, server := range servers {
		if !server.IsWrappable && len(server.Raw) > 0 {
			mcpServers[server.Name] = server.Raw
			continue
		}
		var oauth *jsonOAuthConfig
		if server.OAuthClientID != "" {
			oauth = &jsonOAuthConfig{ClientID: server.OAuthClientID}
		}
		data, err := json.Marshal(jsonServer{
			Type:          defaultType(server.Type),
			Auth:          server.Auth,
			OAuthResource: server.OAuthResource,
			OAuthScopes:   server.OAuthScopes,
			OAuth:         oauth,
			Command:       server.Command,
			Args:          server.Args,
			Env:           server.Env,
			URL:           server.URL,
			Headers:       server.Headers,
		})
		if err != nil {
			return nil, err
		}
		mcpServers[server.Name] = data
	}
	encodedServers, err := json.Marshal(mcpServers)
	if err != nil {
		return nil, err
	}
	doc["mcpServers"] = encodedServers
	data, err = json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func renderAdapterContent(adapter ClientAdapter, servers []RawServer) ([]byte, error) {
	switch adapter.Kind() {
	case "codex":
		data, err := os.ReadFile(adapter.ConfigPath())
		if err != nil {
			return nil, err
		}
		return replaceTOMLMCPServers(data, servers), nil
	default:
		return renderJSONConfig(adapter.ConfigPath(), servers)
	}
}

func defaultType(value string) string {
	if value == "" {
		return "stdio"
	}
	return value
}

func effectiveType(server RawServer) string {
	if server.Type == "" && server.URL != "" {
		return "streamable-http"
	}
	return defaultType(server.Type)
}

func isWrappable(server RawServer) bool {
	if strings.EqualFold(server.Name, "node_repl") || strings.Contains(strings.ToLower(filepath.Base(server.Command)), "node_repl") {
		return false
	}
	if isOAuthManagedRemoteMCP(server) {
		return false
	}
	switch effectiveType(server) {
	case "stdio":
		if server.Command == "" {
			return false
		}
		if filepath.Base(server.Command) == "lazy-mcp-wrapper" {
			return false
		}
		for _, arg := range server.Args {
			if arg == "client" || arg == "--config" {
				return false
			}
		}
		return true
	case "http", "streamable-http":
		if server.URL == "" || isHTTPWrapperRef(server) {
			return false
		}
		if isLocalHTTPMCP(server) {
			return true
		}
		if strings.EqualFold(server.Auth, "none") {
			return true
		}
		return hasExplicitHTTPAuth(server)
	case "sse":
		return false // HTTP+SSE is no longer supported; use streamable-http
	default:
		return false
	}
}

func isLocalHTTPMCP(server RawServer) bool {
	parsed, err := url.Parse(strings.TrimSpace(server.URL))
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	return host == "127.0.0.1" || host == "::1" || strings.EqualFold(host, "localhost")
}

func hasExplicitHTTPAuth(server RawServer) bool {
	for key, value := range server.Headers {
		if strings.TrimSpace(value) == "" {
			continue
		}
		key = strings.ToLower(key)
		if key == "authorization" || key == "x-api-key" || strings.Contains(key, "token") || strings.Contains(key, "api-key") {
			return true
		}
	}
	return false
}

func isOAuthManagedRemoteMCP(server RawServer) bool {
	rawURL := strings.TrimSpace(server.URL)
	if rawURL == "" {
		return false
	}
	if isChatGPTManagedRemoteMCP(server) {
		return false
	}
	if strings.EqualFold(server.Auth, "oauth") {
		return true
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), "mcp.figma.com")
}

func isChatGPTManagedRemoteMCP(server RawServer) bool {
	return strings.TrimSpace(server.URL) != "" && strings.EqualFold(server.Auth, "chatgpt")
}

func isHTTPWrapperRef(server RawServer) bool {
	rawURL := strings.TrimSpace(server.URL)
	if strings.HasPrefix(rawURL, "http://127.0.0.1:") {
		return true
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" && parsed.Hostname() == "127.0.0.1"
}
