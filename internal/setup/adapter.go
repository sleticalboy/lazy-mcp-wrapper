package setup

import "encoding/json"

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
		newCodexAdapter(home),
		newCursorAdapter(home),
		newClaudeCodeAdapter(home),
		newClaudeDesktopAdapter(home),
	}
}
