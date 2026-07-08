package setup

import (
	"strings"
	"testing"
)

func TestParseJSONMCPServersResolvesPlaceholders(t *testing.T) {
	t.Setenv("MCP_BIN", "/opt/bin/context7")
	t.Setenv("MCP_TOKEN", "secret")
	servers, err := parseJSONMCPServers([]byte(`{
  "mcpServers": {
    "context7": {
      "type": "stdio",
      "command": "${env:MCP_BIN}",
      "args": ["--path=${MCP_BIN}", "--sep=${pathSeparator}"],
      "env": {
        "TOKEN": "${MCP_TOKEN}"
      },
      "headers": {
        "Authorization": "Bearer ${env:MCP_TOKEN}"
      }
    }
  }
}`))
	if err != nil {
		t.Fatalf("parseJSONMCPServers() error = %v", err)
	}
	server := servers[0]
	if server.Command != "/opt/bin/context7" {
		t.Fatalf("command = %q", server.Command)
	}
	if server.Args[0] != "--path=/opt/bin/context7" || !strings.HasPrefix(server.Args[1], "--sep=") {
		t.Fatalf("args = %#v", server.Args)
	}
	if server.Env["TOKEN"] != "secret" || server.Headers["Authorization"] != "Bearer secret" {
		t.Fatalf("env/headers = %#v %#v", server.Env, server.Headers)
	}
}

func TestParseTOMLMCPServersResolvesPlaceholders(t *testing.T) {
	t.Setenv("MCP_BIN", "/opt/bin/context7")
	servers, err := parseTOMLMCPServers([]byte(`[mcp_servers.context7]
type = "stdio"
command = "${env:MCP_BIN}"
args = ["--home=${userHome}"]
`))
	if err != nil {
		t.Fatalf("parseTOMLMCPServers() error = %v", err)
	}
	if servers[0].Command != "/opt/bin/context7" || !strings.HasPrefix(servers[0].Args[0], "--home=") {
		t.Fatalf("server = %#v", servers[0])
	}
}

func TestParseJSONMCPServersRejectsCommandPlaceholder(t *testing.T) {
	_, err := parseJSONMCPServers([]byte(`{
  "mcpServers": {
    "context7": {
      "type": "stdio",
      "command": "${cmd: echo context7}"
    }
  }
}`))
	if err == nil || !strings.Contains(err.Error(), "command placeholder") {
		t.Fatalf("parseJSONMCPServers() error = %v", err)
	}
}

func TestParseJSONMCPServersRejectsWorkspacePlaceholder(t *testing.T) {
	_, err := parseJSONMCPServers([]byte(`{
  "mcpServers": {
    "context7": {
      "type": "stdio",
      "command": "${workspaceFolder}/bin/context7"
    }
  }
}`))
	if err == nil || !strings.Contains(err.Error(), "workspaceFolder is not available") {
		t.Fatalf("parseJSONMCPServers() error = %v", err)
	}
}
