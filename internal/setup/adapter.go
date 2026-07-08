package setup

import "encoding/json"

type RawServer struct {
	Name          string            `json:"name"`
	Type          string            `json:"type"`
	Auth          string            `json:"auth,omitempty"`
	OAuthClientID string            `json:"oauth_client_id,omitempty"`
	OAuthResource string            `json:"oauth_resource,omitempty"`
	OAuthScopes   []string          `json:"oauth_scopes,omitempty"`
	Command       string            `json:"command"`
	Args          []string          `json:"args"`
	Env           map[string]string `json:"env,omitempty"`
	URL           string            `json:"url,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Raw           json.RawMessage   `json:"-"`
	Rewritten     bool              `json:"-"`
	IsWrappable   bool              `json:"is_wrappable"`
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
