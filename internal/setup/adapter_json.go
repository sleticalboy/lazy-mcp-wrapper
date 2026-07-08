package setup

import (
	"encoding/json"
	"fmt"
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
	Servers    map[string]json.RawMessage `json:"servers"`
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
	_, mcpServers, err := jsonServerSection(data)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(mcpServers))
	for name := range mcpServers {
		names = append(names, name)
	}
	sort.Strings(names)
	servers := make([]RawServer, 0, len(names))
	for _, name := range names {
		var server jsonServer
		if err := json.Unmarshal(mcpServers[name], &server); err != nil {
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
			Raw:           mcpServers[name],
		}
		raw, err = resolveServerPlaceholders(raw)
		if err != nil {
			return nil, err
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
	isJSONC, err := jsonNeedsNormalization(data)
	if err != nil {
		return nil, err
	}
	if isJSONC {
		if hasRewrittenServers(servers) {
			return nil, fmt.Errorf("JSON-with-comments config cannot be rewritten safely; convert %s to standard JSON or remove comments/trailing commas before running setup", path)
		}
		return data, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	serverKey, _, err := jsonServerSection(data)
	if err != nil {
		return nil, err
	}
	mcpServers := map[string]json.RawMessage{}
	for _, server := range servers {
		if !server.IsWrappable && !server.Rewritten && len(server.Raw) > 0 {
			mcpServers[server.Name] = server.Raw
			continue
		}
		data, err := renderJSONServer(server)
		if err != nil {
			return nil, err
		}
		mcpServers[server.Name] = data
	}
	encodedServers, err := json.Marshal(mcpServers)
	if err != nil {
		return nil, err
	}
	doc[serverKey] = encodedServers
	if serverKey == "mcpServers" {
		delete(doc, "servers")
	} else {
		delete(doc, "mcpServers")
	}
	data, err = json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func jsonServerSection(data []byte) (string, map[string]json.RawMessage, error) {
	var cfg jsonConfig
	if _, err := unmarshalJSONConfig(data, &cfg); err != nil {
		return "", nil, err
	}
	if len(cfg.MCPServers) > 0 || cfg.Servers == nil {
		if cfg.MCPServers == nil {
			cfg.MCPServers = map[string]json.RawMessage{}
		}
		return "mcpServers", cfg.MCPServers, nil
	}
	return "servers", cfg.Servers, nil
}

func unmarshalJSONConfig(data []byte, target any) (bool, error) {
	if err := json.Unmarshal(data, target); err == nil {
		return false, nil
	}
	normalized, err := normalizeJSONC(data)
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(normalized, target); err != nil {
		return false, err
	}
	return true, nil
}

func jsonNeedsNormalization(data []byte) (bool, error) {
	var doc any
	return unmarshalJSONConfig(data, &doc)
}

func hasRewrittenServers(servers []RawServer) bool {
	for _, server := range servers {
		if server.Rewritten || server.IsWrappable {
			return true
		}
	}
	return false
}

func normalizeJSONC(data []byte) ([]byte, error) {
	withoutComments, err := stripJSONComments(data)
	if err != nil {
		return nil, err
	}
	return stripJSONTrailingCommas(withoutComments), nil
}

func stripJSONComments(data []byte) ([]byte, error) {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if inString {
			out = append(out, ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out = append(out, ch)
			continue
		}
		if ch == '/' && i+1 < len(data) {
			next := data[i+1]
			switch next {
			case '/':
				i += 2
				for i < len(data) && data[i] != '\n' && data[i] != '\r' {
					i++
				}
				if i < len(data) {
					out = append(out, data[i])
				}
				continue
			case '*':
				i += 2
				closed := false
				for i+1 < len(data) {
					if data[i] == '*' && data[i+1] == '/' {
						closed = true
						i++
						break
					}
					if data[i] == '\n' || data[i] == '\r' {
						out = append(out, data[i])
					}
					i++
				}
				if !closed {
					return nil, fmt.Errorf("unterminated JSON block comment")
				}
				continue
			}
		}
		out = append(out, ch)
	}
	if inString {
		return nil, fmt.Errorf("unterminated JSON string")
	}
	return out, nil
}

func stripJSONTrailingCommas(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if inString {
			out = append(out, ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out = append(out, ch)
			continue
		}
		if ch == ',' {
			j := i + 1
			for j < len(data) && isJSONWhitespace(data[j]) {
				j++
			}
			if j < len(data) && (data[j] == '}' || data[j] == ']') {
				continue
			}
		}
		out = append(out, ch)
	}
	return out
}

func isJSONWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

func renderJSONServer(server RawServer) ([]byte, error) {
	doc := map[string]json.RawMessage{}
	if len(server.Raw) > 0 {
		if err := json.Unmarshal(server.Raw, &doc); err != nil {
			return nil, err
		}
	}
	setJSONField(doc, "type", defaultType(server.Type))
	setJSONField(doc, "auth", server.Auth)
	setJSONField(doc, "oauth_resource", server.OAuthResource)
	if len(server.OAuthScopes) > 0 {
		setJSONField(doc, "scopes", server.OAuthScopes)
	} else {
		delete(doc, "scopes")
	}
	if server.OAuthClientID != "" {
		setJSONField(doc, "oauth", jsonOAuthConfig{ClientID: server.OAuthClientID})
	} else {
		delete(doc, "oauth")
	}
	setJSONField(doc, "command", server.Command)
	if len(server.Args) > 0 {
		setJSONField(doc, "args", server.Args)
	} else {
		delete(doc, "args")
	}
	if len(server.Env) > 0 {
		setJSONField(doc, "env", server.Env)
	} else {
		delete(doc, "env")
	}
	setJSONField(doc, "url", server.URL)
	if len(server.Headers) > 0 {
		setJSONField(doc, "headers", server.Headers)
	} else {
		delete(doc, "headers")
	}
	return json.Marshal(doc)
}

func setJSONField(doc map[string]json.RawMessage, name string, value any) {
	switch v := value.(type) {
	case string:
		if v == "" {
			delete(doc, name)
			return
		}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	doc[name] = data
}

func renderAdapterContent(adapter ClientAdapter, servers []RawServer) ([]byte, error) {
	return renderServersForPath(adapter.ConfigPath(), servers)
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

func isFigmaRemoteMCP(server RawServer) bool {
	parsed, err := url.Parse(strings.TrimSpace(server.URL))
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
