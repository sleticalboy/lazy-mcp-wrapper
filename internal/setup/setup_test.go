package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlanBuildsWrapperConfigsAndClientUpdates(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(`[mcp_servers.context7]
type = "stdio"
command = "npx"
args = ["-y","@upstash/context7-mcp"]

[mcp_servers.playwright]
type = "stdio"
command = "npx"
args = ["@playwright/mcp@latest"]
`), 0644); err != nil {
		t.Fatal(err)
	}

	plan, err := NewPlan(Options{
		Home:       home,
		BinaryPath: "/bin/lazy-mcp-wrapper",
		Now:        time.Date(2026, 7, 2, 15, 30, 45, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("blockers = %#v", plan.Blockers)
	}
	if len(plan.WrapperConfigs) != 2 {
		t.Fatalf("wrapper configs = %#v", plan.WrapperConfigs)
	}
	if plan.WrapperConfigs[1].Content.Sharing != "session" {
		t.Fatalf("playwright sharing = %s", plan.WrapperConfigs[1].Content.Sharing)
	}
	if len(plan.ClientUpdates) != 1 {
		t.Fatalf("client updates = %#v", plan.ClientUpdates)
	}
	if plan.DaemonConfig.SocketPath != socketPath(home) {
		t.Fatalf("socket path = %s", plan.DaemonConfig.SocketPath)
	}
}

func TestPlanWrapsExplicitNoAuthRemoteMCPAsStreamableHTTP(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(`[mcp_servers.remote]
auth = "none"
url = "https://example.test/mcp"
`), 0644); err != nil {
		t.Fatal(err)
	}

	plan, err := NewPlan(Options{
		Home:       home,
		BinaryPath: "/bin/lazy-mcp-wrapper",
		Now:        time.Date(2026, 7, 5, 9, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("blockers = %#v", plan.Blockers)
	}
	if len(plan.WrapperConfigs) != 1 {
		t.Fatalf("wrapper configs = %#v", plan.WrapperConfigs)
	}
	cfg := plan.WrapperConfigs[0].Content
	if cfg.Name != "remote" || cfg.URL != "https://example.test/mcp" || cfg.Protocol != "streamable-http" || cfg.LocalPort == 0 {
		t.Fatalf("remote wrapper config = %#v", cfg)
	}
	if len(plan.ClientUpdates) != 1 {
		t.Fatalf("client updates = %#v", plan.ClientUpdates)
	}
	content := string(plan.ClientUpdates[0].NewContent)
	if !strings.Contains(content, `type = "streamable-http"`) || !strings.Contains(content, `url = "http://127.0.0.1:`) {
		t.Fatalf("client update missing local streamable-http ref:\n%s", content)
	}
}

func TestPlanSkipsURLOnlyRemoteMCPByDefault(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(`[mcp_servers.remote]
url = "https://example.test/mcp"
`), 0644); err != nil {
		t.Fatal(err)
	}

	plan, err := NewPlan(Options{
		Home:       home,
		BinaryPath: "/bin/lazy-mcp-wrapper",
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	if len(plan.WrapperConfigs) != 0 {
		t.Fatalf("URL-only remote MCP should not be wrapped by default: %#v", plan.WrapperConfigs)
	}
	if len(plan.ClientUpdates) != 0 {
		t.Fatalf("URL-only remote MCP config should be preserved: %#v", plan.ClientUpdates)
	}
	if len(plan.Blockers) == 0 {
		t.Fatal("expected blocker when only URL-only remote MCP is configured")
	}
}

func TestPlanWrapsRemoteMCPWithAuthorizationHeader(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(`[mcp_servers.remote]
url = "https://example.test/mcp"

[mcp_servers.remote.headers]
Authorization = "Bearer test-token"
`), 0644); err != nil {
		t.Fatal(err)
	}

	plan, err := NewPlan(Options{
		Home:       home,
		BinaryPath: "/bin/lazy-mcp-wrapper",
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	if len(plan.WrapperConfigs) != 1 {
		t.Fatalf("wrapper configs = %#v", plan.WrapperConfigs)
	}
	cfg := plan.WrapperConfigs[0].Content
	if cfg.Headers["Authorization"] != "Bearer test-token" {
		t.Fatalf("headers = %#v", cfg.Headers)
	}
}

func TestPlanSkipsFigmaRemoteMCP(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(`[mcp_servers.figma]
url = "https://mcp.figma.com/mcp"
`), 0644); err != nil {
		t.Fatal(err)
	}

	plan, err := NewPlan(Options{
		Home:       home,
		BinaryPath: "/bin/lazy-mcp-wrapper",
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	if len(plan.WrapperConfigs) != 0 {
		t.Fatalf("figma should not be wrapped: %#v", plan.WrapperConfigs)
	}
	if len(plan.ClientUpdates) != 0 {
		t.Fatalf("figma config should be preserved: %#v", plan.ClientUpdates)
	}
	if len(plan.Blockers) == 0 {
		t.Fatal("expected blocker when only figma is configured")
	}
}

func TestBuildWrapperConfigSplitsKnownInlineCommand(t *testing.T) {
	cfg := buildWrapperConfig(t.TempDir(), RawServer{
		Name:    "playwright",
		Command: "npx @playwright/mcp@latest",
	})
	if cfg.Command != "npx" {
		t.Fatalf("command = %q", cfg.Command)
	}
	if len(cfg.Args) != 1 || cfg.Args[0] != "@playwright/mcp@latest" {
		t.Fatalf("args = %#v", cfg.Args)
	}
}

func TestMergeConfigPathsByNamePrefersNewWrapperPath(t *testing.T) {
	dir := t.TempDir()
	writeConfig := func(path, name string) string {
		t.Helper()
		data := []byte(`{
  "name": "` + name + `",
  "command": "npx",
  "args": ["` + name + `"]
}
`)
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	context7 := writeConfig(filepath.Join(dir, "context7-old.json"), "context7")
	mastergoOld := writeConfig(filepath.Join(dir, "mastergo-old.json"), "mastergo-magic-mcp")
	mastergoNew := writeConfig(filepath.Join(dir, "mastergo-new.json"), "mastergo-magic-mcp")
	figma := writeConfig(filepath.Join(dir, "figma.json"), "figma")

	got := mergeConfigPathsByName([]string{context7, mastergoOld}, []string{mastergoNew, figma}, nil)
	want := []string{context7, mastergoNew, figma}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("merged paths = %#v, want %#v", got, want)
	}
}

func TestMergeConfigPathsByNameDropsSkippedExistingConfig(t *testing.T) {
	dir := t.TempDir()
	writeConfig := func(path, name string) string {
		t.Helper()
		data := []byte(`{
  "name": "` + name + `",
  "url": "https://mcp.figma.com/mcp",
  "protocol": "streamable-http"
}
`)
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	context7 := writeConfig(filepath.Join(dir, "context7.json"), "context7")
	figma := writeConfig(filepath.Join(dir, "figma.json"), "figma")

	got := mergeConfigPathsByName([]string{context7, figma}, nil, map[string]bool{"figma": true})
	want := []string{context7}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("merged paths = %#v, want %#v", got, want)
	}
}

func TestPlanApplyWritesAllFilesAndBackups(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(`[mcp_servers.context7]
type = "stdio"
command = "npx"
args = ["-y","@upstash/context7-mcp"]
`), 0644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 2, 15, 30, 45, 0, time.UTC)
	opts := Options{
		Home:       home,
		BinaryPath: "/bin/lazy-mcp-wrapper",
		YesAll:     true,
		Now:        now,
	}
	opts.Exec = func(name string, args ...string) error { return nil }
	plan, err := NewPlan(opts)
	if err != nil {
		t.Fatal(err)
	}
	plan.LaunchAgent.SocketPollAttempts = 0
	if err := plan.Apply(opts); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(wrappersDir(home), "context7.json")); err != nil {
		t.Fatalf("wrapper config missing: %v", err)
	}
	var daemonConfig struct {
		Socket  string   `json:"socket"`
		Configs []string `json:"configs"`
	}
	data, err := os.ReadFile(daemonConfigPath(home))
	if err != nil {
		t.Fatalf("daemon config missing: %v", err)
	}
	if err := json.Unmarshal(data, &daemonConfig); err != nil {
		t.Fatalf("daemon config JSON: %v", err)
	}
	if len(daemonConfig.Configs) != 1 {
		t.Fatalf("daemon configs = %#v", daemonConfig.Configs)
	}
	if _, err := os.Stat(plan.LaunchAgent.PlistPath); err != nil {
		t.Fatalf("plist missing: %v", err)
	}
	if _, err := os.Stat(codexPath + ".bak-20260702153045"); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	updated, _ := os.ReadFile(codexPath)
	if !strings.Contains(string(updated), `/bin/lazy-mcp-wrapper`) {
		t.Fatalf("client config not updated:\n%s", string(updated))
	}
}
