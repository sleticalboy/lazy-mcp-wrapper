package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexAdapterReadWriteServers(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`[mcp_servers.context7]
type = "stdio"
command = "npx"
args = ["-y","@upstash/context7-mcp"]
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	adapter := newCodexAdapter(home)
	servers, err := adapter.ReadServers()
	if err != nil {
		t.Fatalf("ReadServers() error = %v", err)
	}
	if len(servers) != 1 || !servers[0].IsWrappable {
		t.Fatalf("servers = %#v", servers)
	}

	servers[0].Command = "/tmp/lazy-mcp-wrapper"
	servers[0].Args = []string{"client", "--socket", "/tmp/sock", "--name", "context7"}
	if err := adapter.WriteServers(servers, path+".bak"); err != nil {
		t.Fatalf("WriteServers() error = %v", err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `/tmp/lazy-mcp-wrapper`) {
		t.Fatalf("updated config missing wrapper:\n%s", string(data))
	}
}
