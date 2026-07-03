//go:build windows

package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	windowsServiceName        = "lazy-mcp-wrapper"
	windowsServiceDisplayName = "Lazy MCP Wrapper Daemon"
	windowsServiceDescription = "Lazy-loads MCP servers on demand and proxies AI client connections"
)

func installWindowsService(plan LaunchAgentPlan) error {
	if err := os.MkdirAll(filepath.Dir(plan.SocketPath), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(plan.LogDir, 0755); err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Service Control Manager: %w (run as Administrator)", err)
	}
	defer m.Disconnect()

	if err := uninstallWindowsServiceWithManager(m); err != nil {
		return err
	}

	cfg := mgr.Config{
		DisplayName:  windowsServiceDisplayName,
		Description:  windowsServiceDescription,
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	}
	s, err := m.CreateService(windowsServiceName, plan.BinaryPath, cfg, "daemon", "--daemon-config", plan.DaemonConfig)
	if err != nil {
		return fmt.Errorf("create Windows Service %q: %w", windowsServiceName, err)
	}
	defer s.Close()

	if err := s.Start(); err != nil {
		return fmt.Errorf("start Windows Service %q: %w", windowsServiceName, err)
	}
	if plan.SocketPollAttempts <= 0 {
		return nil
	}
	return pollSocket(plan.SocketPath, plan.SocketPollAttempts, 100*time.Millisecond)
}

func uninstallWindowsService(plan LaunchAgentPlan) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Service Control Manager: %w (run as Administrator)", err)
	}
	defer m.Disconnect()

	return uninstallWindowsServiceWithManager(m)
}

func uninstallWindowsServiceWithManager(m *mgr.Mgr) error {
	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		return nil
	}
	defer s.Close()

	_, _ = s.Control(svc.Stop)
	time.Sleep(500 * time.Millisecond)

	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete Windows Service %q: %w", windowsServiceName, err)
	}
	return nil
}
