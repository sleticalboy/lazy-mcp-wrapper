package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUninstallRestoresLatestBackupAndDeletesWrappers(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	current := []byte(`[mcp_servers.context7]
type = "stdio"
command = "/bin/lazy-mcp-wrapper"
args = ["client","--name","context7"]
`)
	if err := os.WriteFile(codexPath, current, 0644); err != nil {
		t.Fatal(err)
	}
	older := codexPath + ".bak-20260101000000"
	latest := codexPath + ".bak-20260201000000"
	if err := os.WriteFile(older, []byte(`old = true
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(latest, []byte(`restored = true
`), 0644); err != nil {
		t.Fatal(err)
	}
	wrapperDir := wrappersDir(home)
	if err := os.MkdirAll(wrapperDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wrapperDir, "context7.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", defaultLabel+".plist")
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plistPath, []byte("plist"), 0644); err != nil {
		t.Fatal(err)
	}

	var calls []string
	err := Uninstall(Options{
		Home:   home,
		YesAll: true,
		Exec: func(name string, args ...string) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	data, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "restored = true\n" {
		t.Fatalf("config not restored from latest backup:\n%s", string(data))
	}
	if _, err := os.Stat(wrapperDir); !os.IsNotExist(err) {
		t.Fatalf("wrapper dir still exists: %v", err)
	}
	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
		t.Fatalf("plist still exists: %v", err)
	}
	if len(calls) < 2 {
		t.Fatalf("launchctl calls = %#v", calls)
	}
}

func TestUninstallRemovesWrapperRefsWithoutBackup(t *testing.T) {
	home := t.TempDir()
	socketPath := socketPath(home)
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(`[mcp_servers.context7]
type = "stdio"
command = "/bin/lazy-mcp-wrapper"
args = ["client","--socket","`+socketPath+`","--name","context7"]

[mcp_servers.keep]
type = "stdio"
command = "npx"
args = ["keep"]
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Uninstall(Options{Home: home, YesAll: true, Exec: func(string, ...string) error { return nil }}); err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	data, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if strings.Contains(out, "context7") || !strings.Contains(out, "keep") {
		t.Fatalf("unexpected config after uninstall:\n%s", out)
	}
}
