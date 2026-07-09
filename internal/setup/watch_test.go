package setup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWatchSnapshotDetectsClientAndWrapperChanges(t *testing.T) {
	home := t.TempDir()
	clientPath := filepath.Join(home, "mcp.json")
	if err := os.WriteFile(clientPath, []byte(`{"mcpServers":{"one":{"type":"stdio","command":"npx","args":["one"]}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	wrapperDir := wrappersDir(home)
	if err := os.MkdirAll(wrapperDir, 0755); err != nil {
		t.Fatal(err)
	}
	wrapperPath := filepath.Join(wrapperDir, "one.json")
	if err := os.WriteFile(wrapperPath, []byte(`{"name":"one","command":"npx","args":["one"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	opts := Options{Home: home, ConfigPaths: []string{clientPath}}

	before, err := newWatchSnapshot(opts)
	if err != nil {
		t.Fatalf("newWatchSnapshot(before) error = %v", err)
	}
	if err := os.WriteFile(clientPath, []byte(`{"mcpServers":{"two":{"type":"stdio","command":"npx","args":["two"]}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wrapperDir, "two.json"), []byte(`{"name":"two","command":"npx","args":["two"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	after, err := newWatchSnapshot(opts)
	if err != nil {
		t.Fatalf("newWatchSnapshot(after) error = %v", err)
	}
	changed := changedWatchPaths(before, after)
	if !containsString(changed, clientPath) {
		t.Fatalf("changed paths = %#v, want client config", changed)
	}
	if !containsString(changed, wrapperDir) {
		t.Fatalf("changed paths = %#v, want wrapper dir", changed)
	}
}

func TestFSNotifyWatchPathsUseNearestExistingParent(t *testing.T) {
	home := t.TempDir()
	missingConfig := filepath.Join(home, ".codex", "config.toml")
	snapshot := watchSnapshot{
		missingConfig: {},
	}
	paths := fsnotifyWatchPaths(snapshot)
	if !paths[home] {
		t.Fatalf("watch paths = %#v, want nearest existing parent %s", paths, home)
	}
}

func TestUpdatePlanUsesExplicitConfigPaths(t *testing.T) {
	home := t.TempDir()
	explicitPath := filepath.Join(home, "explicit.json")
	if err := os.WriteFile(explicitPath, []byte(`{"mcpServers":{"explicit":{"type":"stdio","command":"npx","args":["explicit"]}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(`[mcp_servers.codex]
type = "stdio"
command = "npx"
args = ["codex"]
`), 0644); err != nil {
		t.Fatal(err)
	}

	plan, err := NewUpdatePlan(Options{Home: home, BinaryPath: "/bin/lazy-mcp-wrapper", ConfigPaths: []string{explicitPath}})
	if err != nil {
		t.Fatalf("NewUpdatePlan() error = %v", err)
	}
	if len(plan.AddedWrappers) != 1 || plan.AddedWrappers[0].Server.Name != "explicit" {
		t.Fatalf("added wrappers = %#v, want only explicit", plan.AddedWrappers)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
