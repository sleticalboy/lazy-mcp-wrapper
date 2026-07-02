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
