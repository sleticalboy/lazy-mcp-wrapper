package setup

import (
	"fmt"
	"html"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/daemon"
)

type execFunc func(name string, args ...string) error

const serviceDescription = "Lazy-loads MCP servers on demand and proxies AI client connections"

func buildPlistXML(plan LaunchAgentPlan) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>daemon</string>
    <string>--daemon-config</string>
    <string>%s</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>%s</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <false/>
  <key>StandardOutPath</key>
  <string>%s/daemon.out.log</string>
  <key>StandardErrorPath</key>
  <string>%s/daemon.err.log</string>
</dict>
</plist>
`, xmlEscape(plan.Label), xmlEscape(plan.BinaryPath), xmlEscape(plan.DaemonConfig), xmlEscape(plan.PATH), xmlEscape(plan.LogDir), xmlEscape(plan.LogDir))
}

func buildSystemdUnit(plan LaunchAgentPlan) string {
	return fmt.Sprintf(`[Unit]
Description=%s
After=network.target

[Service]
Type=simple
ExecStart=%s daemon --daemon-config %s
Environment=PATH=%s
Restart=on-failure
StandardOutput=append:%s/daemon.out.log
StandardError=append:%s/daemon.err.log

[Install]
WantedBy=default.target
`, serviceDescription, systemdQuoteArg(plan.BinaryPath), systemdQuoteArg(plan.DaemonConfig), systemdQuoteArg(plan.PATH), plan.LogDir, plan.LogDir)
}

func installLaunchAgent(plan LaunchAgentPlan, execer execFunc) error {
	switch currentGOOS {
	case "windows":
		return installWindowsService(plan)
	case "darwin":
		return installDarwinLaunchAgent(plan, execer)
	case "linux":
		return installSystemdService(plan, execer)
	default:
		fmt.Fprintf(os.Stderr, "note: daemon auto-start is not supported on %s; start manually with: lazy-mcp-wrapper daemon --daemon-config %s\n", currentGOOS, plan.DaemonConfig)
		return nil
	}
}

func installDarwinLaunchAgent(plan LaunchAgentPlan, execer execFunc) error {
	if execer == nil {
		execer = realExec
	}
	if err := os.MkdirAll(filepath.Dir(plan.PlistPath), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plan.SocketPath), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(plan.LogDir, 0755); err != nil {
		return err
	}
	if daemonConnectable(plan.SocketPath) {
		if err := os.WriteFile(plan.PlistPath, plan.Content, 0644); err != nil {
			return err
		}
		resp, err := daemon.SendControl(plan.SocketPath, "reload", daemon.ControlOptions{Graceful: true})
		if err != nil {
			return fmt.Errorf("reload existing daemon: %w", err)
		}
		if !resp.OK {
			return fmt.Errorf("reload existing daemon: %s", resp.Error)
		}
		return nil
	}
	if err := uninstallLaunchAgent(plan, execer); err != nil {
		return err
	}
	if err := os.WriteFile(plan.PlistPath, plan.Content, 0644); err != nil {
		return err
	}
	if err := execer("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), plan.PlistPath); err != nil {
		return err
	}
	if err := execer("launchctl", "enable", fmt.Sprintf("gui/%d/%s", os.Getuid(), plan.Label)); err != nil {
		return err
	}
	if err := execer("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/%s", os.Getuid(), plan.Label)); err != nil {
		return err
	}
	if plan.SocketPollAttempts <= 0 {
		return nil
	}
	return pollSocket(plan.SocketPath, plan.SocketPollAttempts, 100*time.Millisecond)
}

func uninstallLaunchAgent(plan LaunchAgentPlan, execer execFunc) error {
	switch currentGOOS {
	case "windows":
		return uninstallWindowsService(plan)
	case "darwin":
		return uninstallDarwinLaunchAgent(plan, execer)
	case "linux":
		return uninstallSystemdService(plan, execer)
	default:
		return nil
	}
}

func uninstallDarwinLaunchAgent(plan LaunchAgentPlan, execer execFunc) error {
	if execer == nil {
		execer = realExec
	}
	_ = execer("launchctl", "bootout", fmt.Sprintf("gui/%d", os.Getuid()), plan.PlistPath)
	_ = execer("launchctl", "remove", plan.Label)
	if err := os.Remove(plan.SocketPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(plan.PlistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func installSystemdService(plan LaunchAgentPlan, execer execFunc) error {
	if execer == nil {
		execer = realExec
	}
	if err := os.MkdirAll(filepath.Dir(plan.PlistPath), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plan.SocketPath), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(plan.LogDir, 0755); err != nil {
		return err
	}
	if err := uninstallSystemdService(plan, execer); err != nil {
		return err
	}
	if err := os.WriteFile(plan.PlistPath, plan.Content, 0644); err != nil {
		return err
	}
	_ = execer("systemctl", "--user", "daemon-reload")
	if err := execer("systemctl", "--user", "enable", plan.Label); err != nil {
		return err
	}
	if err := execer("systemctl", "--user", "start", plan.Label); err != nil {
		return err
	}
	if plan.SocketPollAttempts <= 0 {
		return nil
	}
	return pollSocket(plan.SocketPath, plan.SocketPollAttempts, 100*time.Millisecond)
}

func uninstallSystemdService(plan LaunchAgentPlan, execer execFunc) error {
	if execer == nil {
		execer = realExec
	}
	_ = execer("systemctl", "--user", "stop", plan.Label)
	_ = execer("systemctl", "--user", "disable", plan.Label)
	if err := os.Remove(plan.PlistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = execer("systemctl", "--user", "daemon-reload")
	if err := os.Remove(plan.SocketPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func realExec(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func pollSocket(path string, attempts int, delay time.Duration) error {
	for i := 0; i < attempts; i++ {
		conn, err := net.DialTimeout("unix", path, delay)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(delay)
	}
	return fmt.Errorf("daemon socket was not created: %s", path)
}

func xmlEscape(value string) string {
	return html.EscapeString(value)
}

func systemdQuoteArg(value string) string {
	if value == "" {
		return `""`
	}
	return strconv.Quote(value)
}
