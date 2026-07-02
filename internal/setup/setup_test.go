package setup

import (
	"os"
	"path/filepath"
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
	if plan.DaemonConfig.SocketPath != filepath.Join(home, socketRel) {
		t.Fatalf("socket path = %s", plan.DaemonConfig.SocketPath)
	}
}
