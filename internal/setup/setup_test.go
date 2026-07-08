package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	oauthstore "github.com/binlee/lazy-mcp-wrapper/internal/oauth"
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
	assertDecision(t, plan, "context7", "wrap", "wrapped-stdio")
	assertDecision(t, plan, "playwright", "wrap", "wrapped-stdio")
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
	if cfg.Auth != "none" {
		t.Fatalf("auth = %q, want none", cfg.Auth)
	}
	assertDecision(t, plan, "remote", "wrap", "wrapped-auth-none")
	if len(plan.ClientUpdates) != 1 {
		t.Fatalf("client updates = %#v", plan.ClientUpdates)
	}
	content := string(plan.ClientUpdates[0].NewContent)
	if !strings.Contains(content, `type = "streamable-http"`) || !strings.Contains(content, `url = "http://127.0.0.1:`) {
		t.Fatalf("client update missing local streamable-http ref:\n%s", content)
	}
}

func TestPlanExplicitConfigPathsPreferLaterServerByName(t *testing.T) {
	home := t.TempDir()
	firstPath := filepath.Join(home, "first.toml")
	secondPath := filepath.Join(home, "second.toml")
	if err := os.WriteFile(firstPath, []byte(`[mcp_servers.tool]
command = "npx"
args = ["old-package"]
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte(`[mcp_servers.tool]
command = "npx"
args = ["new-package"]
`), 0644); err != nil {
		t.Fatal(err)
	}

	plan, err := NewPlan(Options{
		Home:        home,
		BinaryPath:  "/bin/lazy-mcp-wrapper",
		ConfigPaths: []string{firstPath, secondPath},
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("blockers = %#v", plan.Blockers)
	}
	if len(plan.DetectedClients) != 2 {
		t.Fatalf("detected clients = %#v", plan.DetectedClients)
	}
	if len(plan.WrapperConfigs) != 1 {
		t.Fatalf("wrapper configs = %#v", plan.WrapperConfigs)
	}
	if got := strings.Join(plan.WrapperConfigs[0].Content.Args, " "); got != "new-package" {
		t.Fatalf("wrapper args = %q, want later config args", got)
	}
	if len(plan.ClientUpdates) != 2 {
		t.Fatalf("client updates = %#v", plan.ClientUpdates)
	}
	for _, update := range plan.ClientUpdates {
		content := string(update.NewContent)
		if !strings.Contains(content, `command = "/bin/lazy-mcp-wrapper"`) || !strings.Contains(content, `--name`) {
			t.Fatalf("client update for %s missing wrapper ref:\n%s", update.ConfigPath, content)
		}
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
	assertDecision(t, plan, "remote", "skip", "skipped-url-only-remote")
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
	assertDecision(t, plan, "remote", "wrap", "wrapped-explicit-auth")
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
	blockers := strings.Join(plan.Blockers, "\n")
	if !strings.Contains(blockers, "Figma MCP is kept direct") || !strings.Contains(blockers, "dynamic OAuth client registration") {
		t.Fatalf("blockers = %#v, want figma direct blocker", plan.Blockers)
	}
	if strings.Contains(blockers, "auth login figma") {
		t.Fatalf("blockers should not suggest auth login without oauth client id: %#v", plan.Blockers)
	}
	assertDecision(t, plan, "figma", "skip", "skipped-figma-dynamic-client-rejected")
}

func TestPlanSkipsFigmaRemoteMCPWithOAuthClientID(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(`[mcp_servers.figma]
url = "https://mcp.figma.com/mcp"
auth = "oauth"
oauth_resource = "https://mcp.figma.com"
scopes = ["tools:read"]

[mcp_servers.figma.oauth]
client_id = "figma-client"
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
	if !strings.Contains(strings.Join(plan.Blockers, "\n"), "auth login figma") {
		t.Fatalf("blockers = %#v, want auth login hint", plan.Blockers)
	}
	assertDecision(t, plan, "figma", "skip", "skipped-oauth-missing-credential")
}

func TestPlanWrapsFigmaRemoteMCPWithOAuthCredential(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(`[mcp_servers.figma]
url = "https://mcp.figma.com/mcp"
auth = "oauth"
oauth_resource = "https://mcp.figma.com"
scopes = ["tools:read"]

[mcp_servers.figma.oauth]
client_id = "figma-client"
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := oauthstore.NewFileStore(home).Save(oauthstore.Credential{
		Name:        "figma",
		ServerURL:   "https://mcp.figma.com/mcp",
		AccessToken: "stored-token",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	plan, err := NewPlan(Options{
		Home:       home,
		BinaryPath: "/bin/lazy-mcp-wrapper",
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
	assertDecision(t, plan, "figma", "wrap", "wrapped-oauth-credential")
	cfg := plan.WrapperConfigs[0].Content
	if cfg.Name != "figma" || cfg.Auth != "oauth" || cfg.Sharing != "session" || cfg.URL != "https://mcp.figma.com/mcp" || cfg.LocalPort == 0 {
		t.Fatalf("figma wrapper config = %#v", cfg)
	}
	if cfg.OAuthClientID != "figma-client" || cfg.OAuthResource != "https://mcp.figma.com" {
		t.Fatalf("figma oauth fields = %#v", cfg)
	}
	if len(cfg.OAuthScopes) != 1 || cfg.OAuthScopes[0] != "tools:read" {
		t.Fatalf("figma oauth scopes = %#v", cfg.OAuthScopes)
	}
	if len(plan.ClientUpdates) != 1 {
		t.Fatalf("client updates = %#v", plan.ClientUpdates)
	}
	content := string(plan.ClientUpdates[0].NewContent)
	if !strings.Contains(content, `type = "streamable-http"`) || !strings.Contains(content, `url = "http://127.0.0.1:`) || strings.Contains(content, `mcp.figma.com`) {
		t.Fatalf("client update did not replace figma with local HTTP wrapper:\n%s", content)
	}
	if strings.Contains(content, `oauth_resource`) || strings.Contains(content, `[mcp_servers.figma.oauth]`) || strings.Contains(content, `scopes =`) {
		t.Fatalf("client update should remove remote oauth fields:\n%s", content)
	}
}

func TestPlanSkipsOAuthRemoteMCP(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(`[mcp_servers.remote]
url = "https://example.test/mcp"
auth = "oauth"
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
		t.Fatalf("oauth remote should not be wrapped: %#v", plan.WrapperConfigs)
	}
	if len(plan.ClientUpdates) != 0 {
		t.Fatalf("oauth remote config should be preserved: %#v", plan.ClientUpdates)
	}
	if len(plan.Blockers) == 0 {
		t.Fatal("expected blocker when only oauth remote MCP is configured")
	}
	if !strings.Contains(strings.Join(plan.Blockers, "\n"), "auth login remote") {
		t.Fatalf("blockers = %#v, want auth login hint", plan.Blockers)
	}
	assertDecision(t, plan, "remote", "skip", "skipped-oauth-missing-credential")
}

func TestPlanSkipsChatGPTAuthRemoteMCP(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(`[mcp_servers.figma]
url = "https://mcp.figma.com/mcp"
auth = "chatgpt"
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
		t.Fatalf("chatgpt-auth remote should not be wrapped: %#v", plan.WrapperConfigs)
	}
	blockers := strings.Join(plan.Blockers, "\n")
	if !strings.Contains(blockers, "ChatGPT-auth remote MCP cannot be wrapped") {
		t.Fatalf("blockers = %#v, want chatgpt-auth blocker", plan.Blockers)
	}
	if strings.Contains(blockers, "auth login figma") {
		t.Fatalf("blockers should not suggest OAuth login for chatgpt auth: %#v", plan.Blockers)
	}
	assertDecision(t, plan, "figma", "skip", "skipped-chatgpt-auth")
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

func TestPlanDecisionForExistingWrapperRef(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(`[mcp_servers.context7]
type = "stdio"
command = "/bin/lazy-mcp-wrapper"
args = ["client", "--socket", "`+socketPath(home)+`", "--name", "context7"]
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
	assertDecision(t, plan, "context7", "skip", "skipped-existing-wrapper")
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

func assertDecision(t *testing.T, plan Plan, name, action, reason string) {
	t.Helper()
	for _, decision := range plan.Decisions {
		if decision.Name == name && decision.Action == action && decision.Reason == reason {
			return
		}
	}
	t.Fatalf("missing decision name=%s action=%s reason=%s in %#v", name, action, reason, plan.Decisions)
}
