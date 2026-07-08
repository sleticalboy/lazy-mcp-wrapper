package setup

import "os"

type explicitConfigAdapter struct {
	path string
}

func newExplicitConfigAdapter(path string) ClientAdapter {
	return explicitConfigAdapter{path: path}
}

func (a explicitConfigAdapter) Kind() string {
	return "config"
}

func (a explicitConfigAdapter) ConfigPath() string {
	return a.path
}

func (a explicitConfigAdapter) Installed() bool {
	return true
}

func (a explicitConfigAdapter) ReadServers() ([]RawServer, error) {
	return readServersFromPath(a.path)
}

func (a explicitConfigAdapter) WriteServers(servers []RawServer, backupPath string) error {
	data, err := os.ReadFile(a.path)
	if err != nil {
		return err
	}
	if backupPath != "" {
		if err := os.WriteFile(backupPath, data, 0644); err != nil {
			return err
		}
	}
	content, err := renderServersForPath(a.path, servers)
	if err != nil {
		return err
	}
	return os.WriteFile(a.path, content, 0644)
}
