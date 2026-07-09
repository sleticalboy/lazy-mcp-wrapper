package wrapper

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/jsonrpc"
)

const configSchemaVersion = 1

// CurrentSchemaVersion is the schema version written to new config files.
const CurrentSchemaVersion = configSchemaVersion

type Config struct {
	SchemaVersion  int               `json:"schema_version,omitempty"`
	Name           string            `json:"name"`
	Sharing        string            `json:"sharing"`
	Command        string            `json:"command"`
	Args           []string          `json:"args"`
	Env            map[string]string `json:"env"`
	CWD            string            `json:"cwd"`
	URL            string            `json:"url,omitempty"`
	Protocol       string            `json:"protocol,omitempty"`
	HTTPBackend    string            `json:"http_backend,omitempty"`
	UpstreamMode   string            `json:"upstream_protocol_mode,omitempty"`
	Auth           string            `json:"auth,omitempty"`
	OAuthClientID  string            `json:"oauth_client_id,omitempty"`
	OAuthResource  string            `json:"oauth_resource,omitempty"`
	OAuthScopes    []string          `json:"oauth_scopes,omitempty"`
	OAuthStoreDir  string            `json:"oauth_store_dir,omitempty"`
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
	// 旧版 config 无 schema_version，兼容读取；新版写入时会带上版本号
	if cfg.SchemaVersion == 0 {
		cfg.SchemaVersion = configSchemaVersion
	} else if cfg.SchemaVersion > configSchemaVersion {
		return Config{}, fmt.Errorf("config schema version %d is newer than supported version %d; please upgrade lazy-mcp-wrapper", cfg.SchemaVersion, configSchemaVersion)
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
	if cfg.Auth != "" {
		cfg.Auth = strings.ToLower(cfg.Auth)
		switch cfg.Auth {
		case "none", "oauth":
		default:
			return Config{}, fmt.Errorf("config auth must be none or oauth (got %q)", cfg.Auth)
		}
	}
	if cfg.Auth == "oauth" && cfg.URL == "" {
		return Config{}, fmt.Errorf("config auth oauth requires url")
	}
	if cfg.Auth == "oauth" && cfg.HTTPBackend == "native" {
		return Config{}, fmt.Errorf("config auth oauth requires sdk http_backend")
	}
	if cfg.URL != "" {
		switch cfg.HTTPProtocol() {
		case "streamable-http":
		case "sse":
			return Config{}, fmt.Errorf("protocol %q (HTTP+SSE) is no longer supported; use \"streamable-http\" instead", cfg.Protocol)
		default:
			return Config{}, fmt.Errorf("config protocol must be streamable-http (got %q)", cfg.Protocol)
		}
		switch cfg.HTTPBackend {
		case "", "native", "sdk":
		default:
			return Config{}, fmt.Errorf("config http_backend must be native or sdk (got %q)", cfg.HTTPBackend)
		}
		cfg.UpstreamMode = strings.ToLower(strings.TrimSpace(cfg.UpstreamMode))
		switch cfg.UpstreamMode {
		case "", "auto", "legacy", "stateless":
		default:
			return Config{}, fmt.Errorf("config upstream_protocol_mode must be auto, legacy, or stateless (got %q)", cfg.UpstreamMode)
		}
		if cfg.UpstreamMode == "stateless" && cfg.UseSDKHTTPBackend() {
			return Config{}, fmt.Errorf("config upstream_protocol_mode stateless requires native http_backend and auth none")
		}
	}
	if cfg.Sharing == "" {
		if cfg.RequiresOAuth() {
			cfg.Sharing = "session"
		} else {
			cfg.Sharing = "shared"
		}
	}
	if cfg.Sharing != "shared" && cfg.Sharing != "session" {
		return Config{}, fmt.Errorf("config sharing must be shared or session")
	}
	if cfg.RequiresOAuth() && cfg.Sharing != "session" {
		return Config{}, fmt.Errorf("config auth oauth requires session sharing")
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
	c.OAuthClientID = os.ExpandEnv(c.OAuthClientID)
	c.OAuthResource = os.ExpandEnv(c.OAuthResource)
	c.OAuthStoreDir = os.ExpandEnv(c.OAuthStoreDir)
	for i := range c.OAuthScopes {
		c.OAuthScopes[i] = os.ExpandEnv(c.OAuthScopes[i])
	}
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
	case "", "http", "streamable-http":
		return "streamable-http"
	case "sse":
		return "sse"
	default:
		return c.Protocol
	}
}

func (c Config) UseSDKHTTPBackend() bool {
	return c.HTTPBackend == "sdk" || c.RequiresOAuth()
}

func (c Config) UpstreamProtocolMode() string {
	switch strings.ToLower(strings.TrimSpace(c.UpstreamMode)) {
	case "legacy":
		return "legacy"
	case "stateless":
		return "stateless"
	default:
		return "auto"
	}
}

func (c Config) StatelessHTTPUpstream() bool {
	return c.URL != "" && c.UpstreamProtocolMode() == "stateless"
}

func (c Config) RequiresOAuth() bool {
	return strings.EqualFold(c.Auth, "oauth")
}
