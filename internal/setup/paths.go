package setup

import (
	"os"
	"path/filepath"
	"runtime"
)

var currentGOOS = runtime.GOOS

func lazyMCPDir(home string) string {
	if currentGOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, "lazy-mcp-wrapper")
		}
	}
	return filepath.Join(home, ".lazy-mcp-wrapper")
}

func wrappersDir(home string) string {
	return filepath.Join(lazyMCPDir(home), "wrappers")
}

func socketPath(home string) string {
	return filepath.Join(lazyMCPDir(home), "lazy-mcpd.sock")
}

func daemonConfigPath(home string) string {
	return filepath.Join(lazyMCPDir(home), "config.json")
}

func logDir(home string) string {
	switch currentGOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Logs", "lazy-mcp-wrapper")
	case "windows":
		return filepath.Join(lazyMCPDir(home), "logs")
	default:
		return filepath.Join(lazyMCPDir(home), "logs")
	}
}

func launchAgentPath(home string) string {
	switch currentGOOS {
	case "windows":
		return ""
	case "darwin":
		return filepath.Join(home, "Library", "LaunchAgents", defaultLabel+".plist")
	case "linux":
		return filepath.Join(home, ".config", "systemd", "user", defaultLabel+".service")
	default:
		return ""
	}
}
