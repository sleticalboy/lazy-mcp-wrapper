package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseJSONMCPServersOAuthFields(t *testing.T) {
	servers, err := parseJSONMCPServers([]byte(`{
  "mcpServers": {
    "figma": {
      "type": "streamable-http",
      "url": "https://mcp.figma.com/mcp",
      "auth": "oauth",
      "oauth_resource": "https://mcp.figma.com",
      "scopes": ["tools:read"],
      "oauth": {
        "client_id": "figma-client"
      }
    }
  }
}`))
	if err != nil {
		t.Fatalf("parseJSONMCPServers() error = %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("servers = %#v", servers)
	}
	server := servers[0]
	if server.OAuthClientID != "figma-client" || server.OAuthResource != "https://mcp.figma.com" {
		t.Fatalf("oauth fields = %#v", server)
	}
	if len(server.OAuthScopes) != 1 || server.OAuthScopes[0] != "tools:read" {
		t.Fatalf("oauth scopes = %#v", server.OAuthScopes)
	}
}

func TestParseJSONMCPServersAcceptsVSCodeServersKey(t *testing.T) {
	servers, err := parseJSONMCPServers([]byte(`{
  "servers": {
    "github": {
      "url": "https://api.githubcopilot.com/mcp/",
      "headers": {
        "Authorization": "Bearer token"
      }
    }
  }
}`))
	if err != nil {
		t.Fatalf("parseJSONMCPServers() error = %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("servers = %#v", servers)
	}
	server := servers[0]
	if server.Name != "github" || server.URL != "https://api.githubcopilot.com/mcp/" {
		t.Fatalf("server = %#v", server)
	}
	if !server.IsWrappable {
		t.Fatalf("server should be wrappable with explicit auth header: %#v", server)
	}
}

func TestParseJSONMCPServersAcceptsJSONWithComments(t *testing.T) {
	servers, err := parseJSONMCPServers([]byte(`{
  // VS Code-style config with comments and trailing commas.
  "servers": {
    "context7": {
      "type": "stdio",
      "command": "npx",
      "args": ["@upstash/context7-mcp",],
    },
  },
}`))
	if err != nil {
		t.Fatalf("parseJSONMCPServers() error = %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("servers = %#v", servers)
	}
	server := servers[0]
	if server.Name != "context7" || server.Command != "npx" || len(server.Args) != 1 {
		t.Fatalf("server = %#v", server)
	}
}

func TestRenderJSONConfigPreservesVSCodeServersKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{
  "servers": {
    "context7": {
      "type": "stdio",
      "command": "npx",
      "args": ["@upstash/context7-mcp"]
    }
  },
  "other": true
}`), 0644); err != nil {
		t.Fatal(err)
	}
	servers, err := parseJSONMCPServers(mustReadFile(t, path))
	if err != nil {
		t.Fatalf("parseJSONMCPServers() error = %v", err)
	}
	servers[0].Command = "/bin/lazy-mcp-wrapper"
	servers[0].Args = []string{"client", "--socket", "/tmp/sock", "--name", "context7"}
	servers[0].Rewritten = true
	out, err := renderJSONConfig(path, servers)
	if err != nil {
		t.Fatalf("renderJSONConfig() error = %v", err)
	}
	if strings.Contains(string(out), `"mcpServers"`) {
		t.Fatalf("rendered config should keep servers key:\n%s", out)
	}
	if !strings.Contains(string(out), `"servers"`) || !strings.Contains(string(out), `"other": true`) {
		t.Fatalf("rendered config lost expected fields:\n%s", out)
	}
}

func TestRenderJSONConfigLeavesJSONWithCommentsUntouchedWithoutRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	original := []byte(`{
  // keep this comment
  "servers": {
    "remote": {
      "url": "https://example.test/mcp",
    },
  },
}
`)
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	servers, err := parseJSONMCPServers(original)
	if err != nil {
		t.Fatalf("parseJSONMCPServers() error = %v", err)
	}
	out, err := renderJSONConfig(path, servers)
	if err != nil {
		t.Fatalf("renderJSONConfig() error = %v", err)
	}
	if string(out) != string(original) {
		t.Fatalf("JSONC without rewrite should be preserved:\n%s", out)
	}
}

func TestRenderJSONConfigRejectsJSONWithCommentsWhenRewriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	original := []byte(`{
  // keep this comment
  "servers": {
    "context7": {
      "type": "stdio",
      "command": "npx",
      "args": ["@upstash/context7-mcp",],
    },
  },
}
`)
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	servers, err := parseJSONMCPServers(original)
	if err != nil {
		t.Fatalf("parseJSONMCPServers() error = %v", err)
	}
	next := replaceWithWrapperRefs(servers, "/bin/lazy-mcp-wrapper", "/tmp/lazy.sock", nil)
	_, err = renderJSONConfig(path, next)
	if err == nil || !strings.Contains(err.Error(), "JSON-with-comments config cannot be rewritten safely") {
		t.Fatalf("renderJSONConfig() error = %v", err)
	}
}

func TestRenderJSONConfigPreservesUnknownServerFieldsWhenRewriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{
  "mcpServers": {
    "custom": {
      "type": "stdio",
      "command": "npx",
      "args": ["custom-mcp"],
      "description": "keep me",
      "disabled": false
    }
  }
}`), 0644); err != nil {
		t.Fatal(err)
	}
	servers, err := parseJSONMCPServers(mustReadFile(t, path))
	if err != nil {
		t.Fatalf("parseJSONMCPServers() error = %v", err)
	}
	next := replaceWithWrapperRefs(servers, "/bin/lazy-mcp-wrapper", "/tmp/lazy.sock", nil)
	out, err := renderJSONConfig(path, next)
	if err != nil {
		t.Fatalf("renderJSONConfig() error = %v", err)
	}
	var doc struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("rendered JSON invalid: %v\n%s", err, out)
	}
	server := doc.MCPServers["custom"]
	if server["description"] != "keep me" || server["disabled"] != false {
		t.Fatalf("unknown fields not preserved: %#v", server)
	}
	if server["command"] != "/bin/lazy-mcp-wrapper" {
		t.Fatalf("command not rewritten: %#v", server)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
