package daemon

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	SocketPath  string   `json:"socket"`
	ConfigPaths []string `json:"configs"`
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.SocketPath == "" {
		return Config{}, fmt.Errorf("daemon config socket is required")
	}
	if len(cfg.ConfigPaths) == 0 {
		return Config{}, fmt.Errorf("daemon config configs is required")
	}
	return cfg, nil
}
