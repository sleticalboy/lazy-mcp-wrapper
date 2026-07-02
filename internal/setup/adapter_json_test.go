package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONAdapterReadWriteServers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{
  "other": true,
  "mcpServers": {
    "context7": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@upstash/context7-mcp"]
    },
    "remote": {
      "type": "sse",
      "url": "https://example.test/sse",
      "headers": {"Authorization": "Bearer token"}
    }
  }
}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	adapter := newJSONAdapter("cursor", path)
	servers, err := adapter.ReadServers()
	if err != nil {
		t.Fatalf("ReadServers() error = %v", err)
	}
	if len(servers) != 2 || !servers[0].IsWrappable || servers[1].IsWrappable {
		t.Fatalf("servers = %#v", servers)
	}

	servers[0].Command = "/tmp/lazy-mcp-wrapper"
	servers[0].Args = []string{"client", "--socket", "/tmp/sock", "--name", "context7"}
	if err := adapter.WriteServers(servers, path+".bak"); err != nil {
		t.Fatalf("WriteServers() error = %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"other": true`) || !strings.Contains(string(data), `/tmp/lazy-mcp-wrapper`) || !strings.Contains(string(data), `"url": "https://example.test/sse"`) || !strings.Contains(string(data), `"headers"`) {
		t.Fatalf("updated JSON unexpected:\n%s", string(data))
	}
}
