package setup

import (
	"os"
	"path/filepath"
)

type codexAdapter struct {
	path string
}

func newCodexAdapter(home string) ClientAdapter {
	return codexAdapter{path: filepath.Join(home, ".codex", "config.toml")}
}

func (a codexAdapter) Kind() string {
	return "codex"
}

func (a codexAdapter) ConfigPath() string {
	return a.path
}

func (a codexAdapter) Installed() bool {
	_, err := os.Stat(a.path)
	return err == nil
}

func (a codexAdapter) ReadServers() ([]RawServer, error) {
	data, err := os.ReadFile(a.path)
	if err != nil {
		return nil, err
	}
	return parseTOMLMCPServers(data)
}

func (a codexAdapter) WriteServers(servers []RawServer, backupPath string) error {
	data, err := os.ReadFile(a.path)
	if err != nil {
		return err
	}
	if backupPath != "" {
		if err := os.WriteFile(backupPath, data, 0644); err != nil {
			return err
		}
	}
	return os.WriteFile(a.path, replaceTOMLMCPServers(data, servers), 0644)
}
