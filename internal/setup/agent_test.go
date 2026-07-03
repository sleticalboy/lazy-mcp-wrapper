package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPlistXML(t *testing.T) {
	xml := buildPlistXML(LaunchAgentPlan{
		Label:        "com.test.lazy",
		BinaryPath:   "/bin/lazy",
		DaemonConfig: "/tmp/config.json",
		LogDir:       "/tmp/logs",
		PATH:         "/bin:/usr/bin",
	})
	if !strings.Contains(xml, "com.test.lazy") || !strings.Contains(xml, "/bin/lazy") || !strings.Contains(xml, "/tmp/config.json") {
		t.Fatalf("plist XML missing values:\n%s", xml)
	}
}

func TestBuildSystemdUnit(t *testing.T) {
	unit := buildSystemdUnit(LaunchAgentPlan{
		BinaryPath:   "/bin/lazy mcp",
		DaemonConfig: "/tmp/lazy config.json",
		LogDir:       "/tmp/logs",
		PATH:         "/bin:/usr/bin",
	})
	if !strings.Contains(unit, "Description="+serviceDescription) {
		t.Fatalf("systemd unit missing description:\n%s", unit)
	}
	if !strings.Contains(unit, `ExecStart="/bin/lazy mcp" daemon --daemon-config "/tmp/lazy config.json"`) {
		t.Fatalf("systemd unit missing ExecStart:\n%s", unit)
	}
	if !strings.Contains(unit, `Environment=PATH="/bin:/usr/bin"`) {
		t.Fatalf("systemd unit missing PATH:\n%s", unit)
	}
	if !strings.Contains(unit, "StandardOutput=append:/tmp/logs/daemon.out.log") {
		t.Fatalf("systemd unit missing log paths:\n%s", unit)
	}
}

func TestInstallLaunchAgentWritesPlist(t *testing.T) {
	withGOOS(t, "darwin")
	home := t.TempDir()
	plan := LaunchAgentPlan{
		Label:        "com.test.lazy",
		PlistPath:    filepath.Join(home, "Library", "LaunchAgents", "com.test.lazy.plist"),
		SocketPath:   filepath.Join(home, ".lazy-mcp-wrapper", "lazy-mcpd.sock"),
		DaemonConfig: filepath.Join(home, ".lazy-mcp-wrapper", "config.json"),
		BinaryPath:   "/bin/lazy",
		LogDir:       filepath.Join(home, "Library", "Logs", "lazy"),
		PATH:         "/bin",
	}
	plan.Content = []byte(buildPlistXML(plan))
	var calls []string
	execer := func(name string, args ...string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if len(args) > 0 && args[0] == "kickstart" {
			if err := os.MkdirAll(filepath.Dir(plan.SocketPath), 0755); err != nil {
				return err
			}
			file, err := os.Create(plan.SocketPath)
			if err != nil {
				return err
			}
			return file.Close()
		}
		return nil
	}
	if err := installLaunchAgent(plan, execer); err != nil {
		t.Fatalf("installLaunchAgent() error = %v", err)
	}
	if len(calls) < 5 {
		t.Fatalf("calls = %#v", calls)
	}
	if _, err := os.Stat(plan.PlistPath); err != nil {
		t.Fatalf("plist not written: %v", err)
	}
}

func TestInstallLaunchAgentWritesSystemdUnit(t *testing.T) {
	withGOOS(t, "linux")
	home := t.TempDir()
	plan := LaunchAgentPlan{
		Label:              "com.test.lazy",
		PlistPath:          filepath.Join(home, ".config", "systemd", "user", "com.test.lazy.service"),
		SocketPath:         filepath.Join(home, ".lazy-mcp-wrapper", "lazy-mcpd.sock"),
		SocketPollAttempts: 0,
		DaemonConfig:       filepath.Join(home, ".lazy-mcp-wrapper", "config.json"),
		BinaryPath:         "/bin/lazy",
		LogDir:             filepath.Join(home, ".lazy-mcp-wrapper", "logs"),
		PATH:               "/bin",
	}
	plan.Content = []byte(buildSystemdUnit(plan))
	var calls []string
	execer := func(name string, args ...string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil
	}
	if err := installLaunchAgent(plan, execer); err != nil {
		t.Fatalf("installLaunchAgent() error = %v", err)
	}
	data, err := os.ReadFile(plan.PlistPath)
	if err != nil {
		t.Fatalf("systemd unit not written: %v", err)
	}
	if !strings.Contains(string(data), `ExecStart="/bin/lazy" daemon --daemon-config `) {
		t.Fatalf("systemd unit missing ExecStart:\n%s", string(data))
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"systemctl --user stop com.test.lazy",
		"systemctl --user disable com.test.lazy",
		"systemctl --user daemon-reload",
		"systemctl --user enable com.test.lazy",
		"systemctl --user start com.test.lazy",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("systemctl call %q missing from:\n%s", want, joined)
		}
	}
}
