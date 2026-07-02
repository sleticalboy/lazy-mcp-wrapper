package setup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatusReportsWrappersDaemonAndClients(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(home, socketRel)
	if err := os.WriteFile(codexPath, []byte(`[mcp_servers.context7]
type = "stdio"
command = "/bin/lazy-mcp-wrapper"
args = ["client","--socket","`+socketPath+`","--name","context7"]

[mcp_servers.raw]
type = "stdio"
command = "npx"
args = ["raw"]
`), 0644); err != nil {
		t.Fatal(err)
	}
	wrapperDir := filepath.Join(home, wrappersRel)
	if err := os.MkdirAll(wrapperDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wrapperDir, "context7.json"), []byte(`{
  "name": "context7",
  "sharing": "shared",
  "command": "npx",
  "args": ["-y", "@upstash/context7-mcp"],
  "real_framing": "jsonl"
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	report := Status(Options{Home: home})
	if report.WrapperCount != 1 {
		t.Fatalf("WrapperCount = %d", report.WrapperCount)
	}
	if report.DaemonSocket != socketPath {
		t.Fatalf("DaemonSocket = %s", report.DaemonSocket)
	}
	var codex ClientStatus
	for _, client := range report.Clients {
		if client.Kind == "codex" {
			codex = client
		}
	}
	if !codex.Installed || codex.WrappedCount != 1 || codex.TotalCount != 2 {
		t.Fatalf("codex status = %#v", codex)
	}
}
