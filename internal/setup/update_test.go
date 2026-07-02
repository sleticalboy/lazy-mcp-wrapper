package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateAddsAndRemovesWrapperConfigs(t *testing.T) {
	home := t.TempDir()
	socketPath := filepath.Join(home, socketRel)
	cursorPath := filepath.Join(home, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(cursorPath), 0755); err != nil {
		t.Fatal(err)
	}
	originalClientConfig := []byte(`{
  "mcpServers": {
    "context7": {
      "type": "stdio",
      "command": "/bin/lazy-mcp-wrapper",
      "args": ["client", "--socket", "` + socketPath + `", "--name", "context7"]
    },
    "new-tool": {
      "type": "stdio",
      "command": "npx",
      "args": ["new-tool"]
    }
  }
}
`)
	if err := os.WriteFile(cursorPath, originalClientConfig, 0644); err != nil {
		t.Fatal(err)
	}
	wrapperDir := filepath.Join(home, wrappersRel)
	if err := os.MkdirAll(wrapperDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeWrapper := func(name string) {
		t.Helper()
		data := []byte(`{
  "name": "` + name + `",
  "sharing": "shared",
  "command": "npx",
  "args": ["` + name + `"],
  "real_framing": "jsonl"
}
`)
		if err := os.WriteFile(filepath.Join(wrapperDir, name+".json"), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	writeWrapper("context7")
	writeWrapper("old-tool")

	if err := Update(Options{Home: home, YesAll: true}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(wrapperDir, "new-tool.json")); err != nil {
		t.Fatalf("new wrapper missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wrapperDir, "old-tool.json")); !os.IsNotExist(err) {
		t.Fatalf("old wrapper still exists: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, daemonRel))
	if err != nil {
		t.Fatalf("daemon config missing: %v", err)
	}
	var daemonConfig struct {
		Socket  string   `json:"socket"`
		Configs []string `json:"configs"`
	}
	if err := json.Unmarshal(data, &daemonConfig); err != nil {
		t.Fatalf("daemon config JSON: %v", err)
	}
	if daemonConfig.Socket != socketPath {
		t.Fatalf("socket = %s", daemonConfig.Socket)
	}
	if len(daemonConfig.Configs) != 2 {
		t.Fatalf("configs = %#v", daemonConfig.Configs)
	}
	afterClientConfig, err := os.ReadFile(cursorPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterClientConfig) != string(originalClientConfig) {
		t.Fatalf("client config changed:\n%s", string(afterClientConfig))
	}
}

func TestUpdateBlocksWhenNoWrapperConfigsRemain(t *testing.T) {
	home := t.TempDir()
	wrapperDir := filepath.Join(home, wrappersRel)
	if err := os.MkdirAll(wrapperDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wrapperDir, "old-tool.json"), []byte(`{
  "name": "old-tool",
  "sharing": "shared",
  "command": "npx",
  "args": ["old-tool"],
  "real_framing": "jsonl"
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	err := Update(Options{Home: home, YesAll: true})
	if err == nil {
		t.Fatal("Update() error is nil, want blocker")
	}
	if _, statErr := os.Stat(filepath.Join(wrapperDir, "old-tool.json")); statErr != nil {
		t.Fatalf("old wrapper should not be deleted when blocked: %v", statErr)
	}
}
