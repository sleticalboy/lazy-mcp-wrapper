package setup

import "path/filepath"

func newClaudeDesktopAdapter(home string) ClientAdapter {
	return newJSONAdapter("claude-desktop", filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"))
}
