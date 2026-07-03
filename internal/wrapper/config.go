package wrapper

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/jsonrpc"
)

type Config struct {
	Name           string            `json:"name"`
	Sharing        string            `json:"sharing"`
	Command        string            `json:"command"`
	Args           []string          `json:"args"`
	Env            map[string]string `json:"env"`
	CWD            string            `json:"cwd"`
	URL            string            `json:"url,omitempty"`
	Protocol       string            `json:"protocol,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	LocalPort      int               `json:"local_port,omitempty"`
	RealProtocol   string            `json:"real_protocol_version"`
	RealFraming    string            `json:"real_framing"`
	CacheDir       string            `json:"cache_dir"`
	DisableCache   bool              `json:"disable_cache"`
	IdleTimeout    Duration          `json:"idle_timeout"`
	StartupTimeout Duration          `json:"startup_timeout"`
	CallTimeout    Duration          `json:"call_timeout"`
	LogFile        string            `json:"log_file"`
}

type Duration struct {
	time.Duration
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
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
	if cfg.Name == "" {
		return Config{}, fmt.Errorf("config name is required")
	}
	if cfg.Command == "" && cfg.URL == "" {
		return Config{}, fmt.Errorf("config command or url is required")
	}
	if cfg.Command != "" && cfg.URL != "" {
		return Config{}, fmt.Errorf("config command and url are mutually exclusive")
	}
	if cfg.URL != "" {
		switch cfg.HTTPProtocol() {
		case "sse", "streamable-http":
		default:
			return Config{}, fmt.Errorf("config protocol must be sse, http, or streamable-http")
		}
	}
	if cfg.Sharing == "" {
		cfg.Sharing = "shared"
	}
	if cfg.Sharing != "shared" && cfg.Sharing != "session" {
		return Config{}, fmt.Errorf("config sharing must be shared or session")
	}
	if _, err := cfg.Framing(); err != nil {
		return Config{}, err
	}
	if cfg.IdleTimeout.Duration == 0 {
		cfg.IdleTimeout.Duration = 30 * time.Second
	}
	if cfg.StartupTimeout.Duration == 0 {
		cfg.StartupTimeout.Duration = 20 * time.Second
	}
	if cfg.CallTimeout.Duration == 0 {
		cfg.CallTimeout.Duration = 2 * time.Minute
	}
	cfg.expandEnv()
	return cfg, nil
}

func (c *Config) expandEnv() {
	c.Command = os.ExpandEnv(c.Command)
	for i := range c.Args {
		c.Args[i] = os.ExpandEnv(c.Args[i])
	}
	for key, value := range c.Env {
		c.Env[key] = os.ExpandEnv(value)
	}
	c.CWD = os.ExpandEnv(c.CWD)
	c.URL = os.ExpandEnv(c.URL)
	c.CacheDir = os.ExpandEnv(c.CacheDir)
	c.LogFile = os.ExpandEnv(c.LogFile)
	for key, value := range c.Headers {
		c.Headers[key] = os.ExpandEnv(value)
	}
}

func (c Config) Framing() (jsonrpc.Framing, error) {
	return jsonrpc.NormalizeFraming(c.RealFraming)
}

func (c Config) HTTPProtocol() string {
	switch c.Protocol {
	case "":
		return "sse"
	case "http":
		return "streamable-http"
	default:
		return c.Protocol
	}
}
