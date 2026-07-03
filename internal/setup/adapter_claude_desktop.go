package setup

import (
	"os"
	"path/filepath"
)

func newClaudeDesktopAdapter(home string) ClientAdapter {
	return newJSONAdapter("claude-desktop", claudeDesktopConfigPath(home))
}

func claudeDesktopConfigPath(home string) string {
	switch currentGOOS {
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "Claude", "claude_desktop_config.json")
		}
	case "linux":
		return filepath.Join(home, ".config", "Claude", "claude_desktop_config.json")
	}
	return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
}
