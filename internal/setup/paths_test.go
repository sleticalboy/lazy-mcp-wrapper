package setup

import (
	"path/filepath"
	"testing"
)

func TestPathsUseLocalAppDataOnWindows(t *testing.T) {
	home := filepath.Join("Users", "test")
	localAppData := filepath.Join(home, "AppData", "Local")
	t.Setenv("LOCALAPPDATA", localAppData)
	withGOOS(t, "windows")

	base := filepath.Join(localAppData, "lazy-mcp-wrapper")
	if got := lazyMCPDir(home); got != base {
		t.Fatalf("lazyMCPDir() = %s, want %s", got, base)
	}
	if got := wrappersDir(home); got != filepath.Join(base, "wrappers") {
		t.Fatalf("wrappersDir() = %s", got)
	}
	if got := socketPath(home); got != filepath.Join(base, "lazy-mcpd.sock") {
		t.Fatalf("socketPath() = %s", got)
	}
	if got := daemonConfigPath(home); got != filepath.Join(base, "config.json") {
		t.Fatalf("daemonConfigPath() = %s", got)
	}
	if got := logDir(home); got != filepath.Join(base, "logs") {
		t.Fatalf("logDir() = %s", got)
	}
}

func TestPathsFallbackToHomeWithoutLocalAppData(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LOCALAPPDATA", "")
	withGOOS(t, "windows")

	if got := lazyMCPDir(home); got != filepath.Join(home, ".lazy-mcp-wrapper") {
		t.Fatalf("lazyMCPDir() = %s", got)
	}
}

func TestLogDirByPlatform(t *testing.T) {
	home := t.TempDir()

	withGOOS(t, "darwin")
	if got := logDir(home); got != filepath.Join(home, "Library", "Logs", "lazy-mcp-wrapper") {
		t.Fatalf("darwin logDir() = %s", got)
	}

	withGOOS(t, "linux")
	if got := logDir(home); got != filepath.Join(home, ".lazy-mcp-wrapper", "logs") {
		t.Fatalf("linux logDir() = %s", got)
	}
}

func TestLaunchAgentPathByPlatform(t *testing.T) {
	home := t.TempDir()

	withGOOS(t, "windows")
	if got := launchAgentPath(home); got != "" {
		t.Fatalf("windows launchAgentPath() = %s", got)
	}

	withGOOS(t, "darwin")
	if got := launchAgentPath(home); got != filepath.Join(home, "Library", "LaunchAgents", defaultLabel+".plist") {
		t.Fatalf("darwin launchAgentPath() = %s", got)
	}

	withGOOS(t, "linux")
	if got := launchAgentPath(home); got != filepath.Join(home, ".config", "systemd", "user", defaultLabel+".service") {
		t.Fatalf("linux launchAgentPath() = %s", got)
	}

	withGOOS(t, "freebsd")
	if got := launchAgentPath(home); got != "" {
		t.Fatalf("other launchAgentPath() = %s", got)
	}
}

func TestClaudeDesktopConfigPathByPlatform(t *testing.T) {
	home := t.TempDir()
	appData := filepath.Join(home, "AppData", "Roaming")
	t.Setenv("APPDATA", appData)

	withGOOS(t, "windows")
	if got := claudeDesktopConfigPath(home); got != filepath.Join(appData, "Claude", "claude_desktop_config.json") {
		t.Fatalf("windows Claude Desktop path = %s", got)
	}

	withGOOS(t, "linux")
	if got := claudeDesktopConfigPath(home); got != filepath.Join(home, ".config", "Claude", "claude_desktop_config.json") {
		t.Fatalf("linux Claude Desktop path = %s", got)
	}

	withGOOS(t, "darwin")
	if got := claudeDesktopConfigPath(home); got != filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json") {
		t.Fatalf("darwin Claude Desktop path = %s", got)
	}
}

func withGOOS(t *testing.T, goos string) {
	t.Helper()
	previous := currentGOOS
	currentGOOS = goos
	t.Cleanup(func() {
		currentGOOS = previous
	})
}
