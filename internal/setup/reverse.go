package setup

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/wrapper"
)

type existingWrapperConfig struct {
	Name   string
	Path   string
	Config wrapper.Config
}

func isWrapperRef(server RawServer, socketPath string) bool {
	if defaultType(server.Type) != "stdio" {
		return false
	}
	if filepath.Base(server.Command) == "lazy-mcp-wrapper" {
		return true
	}
	if len(server.Args) == 0 || server.Args[0] != "client" {
		return false
	}
	hasSocket := false
	hasName := false
	for i, arg := range server.Args {
		switch arg {
		case "--socket":
			if i+1 < len(server.Args) && (socketPath == "" || server.Args[i+1] == socketPath) {
				hasSocket = true
			}
		case "--name":
			hasName = true
		}
	}
	return hasSocket && hasName
}

func daemonConnectable(socketPath string) bool {
	if socketPath == "" {
		return false
	}
	conn, err := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func listWrapperConfigs(wrapperDir string) ([]existingWrapperConfig, error) {
	matches, err := filepath.Glob(filepath.Join(wrapperDir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	configs := make([]existingWrapperConfig, 0, len(matches))
	for _, path := range matches {
		cfg, err := wrapper.LoadConfig(path)
		if err != nil {
			return nil, err
		}
		configs = append(configs, existingWrapperConfig{
			Name:   canonicalName(cfg.Name),
			Path:   path,
			Config: cfg,
		})
	}
	return configs, nil
}

func latestBackupPath(configPath string) (string, bool, error) {
	matches, err := filepath.Glob(configPath + ".bak-*")
	if err != nil {
		return "", false, err
	}
	if len(matches) == 0 {
		return "", false, nil
	}
	sort.Strings(matches)
	return matches[len(matches)-1], true, nil
}

func removeWrapperRefs(servers []RawServer, socketPath string) []RawServer {
	out := make([]RawServer, 0, len(servers))
	for _, server := range servers {
		if isWrapperRef(server, socketPath) {
			continue
		}
		out = append(out, server)
	}
	return out
}

func currentDaemonSocket(home string) string {
	configPath := daemonConfigPath(home)
	if cfg, err := loadDaemonConfigLoose(configPath); err == nil && cfg.SocketPath != "" {
		return cfg.SocketPath
	}
	return socketPath(home)
}

func loadDaemonConfigLoose(path string) (DaemonConfigPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DaemonConfigPlan{}, err
	}
	var cfg struct {
		Socket  string   `json:"socket"`
		Configs []string `json:"configs"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DaemonConfigPlan{}, err
	}
	return DaemonConfigPlan{ConfigPath: path, SocketPath: cfg.Socket, ConfigPaths: cfg.Configs}, nil
}
