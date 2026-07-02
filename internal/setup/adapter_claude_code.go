package setup

import "path/filepath"

func newClaudeCodeAdapter(home string) ClientAdapter {
	return newJSONAdapter("claude-code", filepath.Join(home, ".claude", "settings.json"))
}
