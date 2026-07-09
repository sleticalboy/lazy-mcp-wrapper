package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	oauthstore "github.com/binlee/lazy-mcp-wrapper/internal/oauth"
)

func TestUpdateAddsAndRemovesWrapperConfigs(t *testing.T) {
	home := t.TempDir()
	socketPath := socketPath(home)
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
	wrapperDir := wrappersDir(home)
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

	if err := Update(Options{Home: home, BinaryPath: "/bin/lazy-mcp-wrapper", YesAll: true}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(wrapperDir, "new-tool.json")); err != nil {
		t.Fatalf("new wrapper missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wrapperDir, "old-tool.json")); !os.IsNotExist(err) {
		t.Fatalf("old wrapper still exists: %v", err)
	}
	data, err := os.ReadFile(daemonConfigPath(home))
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
	if string(afterClientConfig) == string(originalClientConfig) {
		t.Fatal("client config was not updated")
	}
	if !strings.Contains(string(afterClientConfig), `"--name"`) || !strings.Contains(string(afterClientConfig), `"new-tool"`) {
		t.Fatalf("client config missing new wrapper ref:\n%s", string(afterClientConfig))
	}
}

func TestUpdateBlocksWhenNoWrapperConfigsRemain(t *testing.T) {
	home := t.TempDir()
	wrapperDir := wrappersDir(home)
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

	err := Update(Options{Home: home, BinaryPath: "/bin/lazy-mcp-wrapper", YesAll: true})
	if err == nil {
		t.Fatal("Update() error is nil, want blocker")
	}
	if _, statErr := os.Stat(filepath.Join(wrapperDir, "old-tool.json")); statErr != nil {
		t.Fatalf("old wrapper should not be deleted when blocked: %v", statErr)
	}
}

func TestUpdateAddsFigmaWrapperWhenOAuthCredentialExists(t *testing.T) {
	home := t.TempDir()
	cursorPath := filepath.Join(home, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(cursorPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cursorPath, []byte(`{
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
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := oauthstore.NewFileStore(home).Save(oauthstore.Credential{
		Name:        "figma",
		ServerURL:   "https://mcp.figma.com/mcp",
		ClientID:    "figma-client",
		Resource:    "https://mcp.figma.com",
		Scopes:      []string{"tools:read"},
		AccessToken: "stored-token",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	plan, err := NewUpdatePlan(Options{Home: home, BinaryPath: "/bin/lazy-mcp-wrapper"})
	if err != nil {
		t.Fatalf("NewUpdatePlan() error = %v", err)
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("blockers = %#v", plan.Blockers)
	}
	if len(plan.AddedWrappers) != 1 {
		t.Fatalf("added wrappers = %#v", plan.AddedWrappers)
	}
	cfg := plan.AddedWrappers[0].Content
	if cfg.Name != "figma" || cfg.Auth != "oauth" || cfg.Sharing != "session" || cfg.LocalPort == 0 {
		t.Fatalf("figma wrapper config = %#v", cfg)
	}
	if cfg.OAuthClientID != "figma-client" || cfg.OAuthResource != "https://mcp.figma.com" {
		t.Fatalf("figma oauth fields = %#v", cfg)
	}
	if len(cfg.OAuthScopes) != 1 || cfg.OAuthScopes[0] != "tools:read" {
		t.Fatalf("figma oauth scopes = %#v", cfg.OAuthScopes)
	}
}

func TestUpdateDoesNotRewriteRemovedHTTPWrapperRefs(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(`[mcp_servers.context7]
type = "stdio"
command = "/bin/lazy-mcp-wrapper"
args = ["client","--socket","`+socketPath(home)+`","--name","context7"]

[mcp_servers.figma]
type = "streamable-http"
url = "https://mcp.figma.com/mcp"
`), 0644); err != nil {
		t.Fatal(err)
	}

	wrapperDir := wrappersDir(home)
	if err := os.MkdirAll(wrapperDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wrapperDir, "context7.json"), []byte(`{
  "schema_version": 1,
  "name": "context7",
  "sharing": "shared",
  "command": "npx",
  "args": ["context7"]
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wrapperDir, "figma.json"), []byte(`{
  "schema_version": 1,
  "name": "figma",
  "sharing": "shared",
  "url": "https://mcp.figma.com/mcp",
  "protocol": "streamable-http",
  "local_port": 54301
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	plan, err := NewUpdatePlan(Options{Home: home, BinaryPath: "/bin/lazy-mcp-wrapper"})
	if err != nil {
		t.Fatalf("NewUpdatePlan() error = %v", err)
	}
	if len(plan.RemovedWrappers) != 1 || plan.RemovedWrappers[0].Name != "figma" {
		t.Fatalf("removed wrappers = %#v", plan.RemovedWrappers)
	}
	for _, update := range plan.ClientUpdates {
		if strings.Contains(string(update.NewContent), `url = "http://127.0.0.1:54301"`) {
			t.Fatalf("removed figma wrapper local port leaked into client update:\n%s", string(update.NewContent))
		}
	}
}
