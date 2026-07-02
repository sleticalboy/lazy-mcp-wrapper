package setup

import (
	"encoding/json"
	"path/filepath"
)

type RawServer struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env,omitempty"`
	URL         string            `json:"url,omitempty"`
	Raw         json.RawMessage   `json:"-"`
	IsWrappable bool              `json:"is_wrappable"`
}

type ClientAdapter interface {
	Kind() string
	ConfigPath() string
	Installed() bool
	ReadServers() ([]RawServer, error)
	WriteServers(servers []RawServer, backupPath string) error
}

type ClientInfo struct {
	Kind       string
	ConfigPath string
	Servers    []RawServer
}

func allAdapters(home string) []ClientAdapter {
	return []ClientAdapter{
		newCodexAdapter(filepath.Join(home, ".codex", "config.toml")),
		newJSONAdapter("cursor", filepath.Join(home, ".cursor", "mcp.json")),
		newJSONAdapter("claude-code", filepath.Join(home, ".claude", "settings.json")),
		newJSONAdapter("claude-desktop", filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")),
	}
}
