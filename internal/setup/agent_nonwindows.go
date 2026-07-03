//go:build !windows

package setup

import "fmt"

func installWindowsService(plan LaunchAgentPlan) error {
	return fmt.Errorf("Windows Service install requires a Windows build")
}

func uninstallWindowsService(plan LaunchAgentPlan) error {
	return fmt.Errorf("Windows Service uninstall requires a Windows build")
}
