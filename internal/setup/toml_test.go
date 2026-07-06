package setup

import (
	"strings"
	"testing"
)

func TestParseAndReplaceTOMLMCPServers(t *testing.T) {
	input := []byte(`model = "gpt"

[mcp_servers.context7]
type = "stdio"
command = "npx"
args = ["-y","@upstash/context7-mcp"]

[mcp_servers.playwright]
type = "stdio"
command = "npx"
args = ["@playwright/mcp@latest"]
[mcp_servers.playwright.env]
NODE_ENV = "test"

[tui]
theme = "dark"
`)

	servers, err := parseTOMLMCPServers(input)
	if err != nil {
		t.Fatalf("parseTOMLMCPServers() error = %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("servers = %#v", servers)
	}
	if servers[1].Env["NODE_ENV"] != "test" {
		t.Fatalf("env = %#v", servers[1].Env)
	}

	servers[0].Command = "/bin/lazy-mcp-wrapper"
	servers[0].Args = []string{"client", "--name", "context7"}
	out := string(replaceTOMLMCPServers(input, servers))
	if !strings.Contains(out, `model = "gpt"`) || !strings.Contains(out, `[tui]`) {
		t.Fatalf("non-MCP content not preserved:\n%s", out)
	}
	if !strings.Contains(out, `command = "/bin/lazy-mcp-wrapper"`) {
		t.Fatalf("replacement missing:\n%s", out)
	}
	if strings.Count(out, "[mcp_servers.context7]") != 1 {
		t.Fatalf("duplicate context7 section:\n%s", out)
	}
}

func TestReplaceTOMLPreservesNonWrappableServerBlock(t *testing.T) {
	input := []byte(`model = "gpt"

[mcp_servers.context7]
command = "npx"
args = ["-y","@upstash/context7-mcp"]

[mcp_servers.node_repl]
command = "/Applications/Codex.app/Contents/Resources/cua_node/bin/node_repl"
startup_timeout_sec = 120.0

[mcp_servers.node_repl.env]
CODEX_HOME = "/Users/binlee/.codex"

[notice]
hide_full_access_warning = true
`)

	servers, err := parseTOMLMCPServers(input)
	if err != nil {
		t.Fatalf("parseTOMLMCPServers() error = %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("servers = %#v", servers)
	}
	if servers[1].Name != "node_repl" || servers[1].IsWrappable {
		t.Fatalf("node_repl should not be wrappable: %#v", servers[1])
	}

	servers[0].Command = "/bin/lazy-mcp-wrapper"
	servers[0].Args = []string{"client", "--name", "context7"}
	servers[0].Raw = nil
	out := string(replaceTOMLMCPServers(input, servers))
	for _, want := range []string{
		`[mcp_servers.node_repl]`,
		`command = "/Applications/Codex.app/Contents/Resources/cua_node/bin/node_repl"`,
		`startup_timeout_sec = 120.0`,
		`[mcp_servers.node_repl.env]`,
		`CODEX_HOME = "/Users/binlee/.codex"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "args = null") {
		t.Fatalf("output should not contain args = null:\n%s", out)
	}
}

func TestParseAndRenderTOMLOAuthFields(t *testing.T) {
	input := []byte(`[mcp_servers.figma]
url = "https://mcp.figma.com/mcp"
auth = "oauth"
oauth_resource = "https://mcp.figma.com"
scopes = ["tools:read","files:read"]

[mcp_servers.figma.oauth]
client_id = "figma-client"
`)

	servers, err := parseTOMLMCPServers(input)
	if err != nil {
		t.Fatalf("parseTOMLMCPServers() error = %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("servers = %#v", servers)
	}
	server := servers[0]
	if server.Auth != "oauth" || server.OAuthClientID != "figma-client" || server.OAuthResource != "https://mcp.figma.com" {
		t.Fatalf("oauth fields = %#v", server)
	}
	if len(server.OAuthScopes) != 2 || server.OAuthScopes[0] != "tools:read" || server.OAuthScopes[1] != "files:read" {
		t.Fatalf("scopes = %#v", server.OAuthScopes)
	}

	server.Raw = nil
	out := strings.Join(renderTOMLMCPServers([]RawServer{server}), "\n")
	for _, want := range []string{
		`auth = "oauth"`,
		`oauth_resource = "https://mcp.figma.com"`,
		`scopes = ["tools:read","files:read"]`,
		`[mcp_servers.figma.oauth]`,
		`client_id = "figma-client"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}
