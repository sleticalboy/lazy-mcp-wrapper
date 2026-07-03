package setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/daemon"
	"github.com/binlee/lazy-mcp-wrapper/internal/wrapper"
)

const (
	defaultLabel = "com.binlee.lazy-mcp-wrapper"
	socketRel    = ".lazy-mcp-wrapper/lazy-mcpd.sock"
	daemonRel    = ".lazy-mcp-wrapper/config.json"
	wrappersRel  = ".lazy-mcp-wrapper/wrappers"
	httpPortBase = 54300
)

type Options struct {
	Home       string
	BinaryPath string
	YesAll     bool
	DryRun     bool
	Now        time.Time
	Exec       execFunc
}

type Plan struct {
	DetectedClients []ClientInfo
	WrapperConfigs  []WrapperConfigPlan
	ClientUpdates   []ClientUpdate
	DaemonConfig    DaemonConfigPlan
	LaunchAgent     LaunchAgentPlan
	Blockers        []string
}

type WrapperConfigPlan struct {
	Server     RawServer
	ConfigPath string
	Content    wrapper.Config
}

type ClientUpdate struct {
	Kind       string
	ConfigPath string
	BackupPath string
	Servers    []RawServer
	NewContent []byte
}

type DaemonConfigPlan struct {
	ConfigPath  string
	SocketPath  string
	ConfigPaths []string
	Content     []byte
}

type LaunchAgentPlan struct {
	Label              string
	PlistPath          string
	SocketPath         string
	SocketPollAttempts int
	DaemonConfig       string
	BinaryPath         string
	LogDir             string
	PATH               string
	Content            []byte
}

func NewPlan(opts Options) (Plan, error) {
	opts = normalizeOptions(opts)
	var plan Plan

	if opts.BinaryPath == "" {
		plan.Blockers = append(plan.Blockers, "binary path is required")
	}

	wrappable := map[string]RawServer{}
	for _, adapter := range scanClients(opts.Home) {
		servers, err := adapter.ReadServers()
		if err != nil {
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("%s: %v", adapter.Kind(), err))
			continue
		}
		info := ClientInfo{Kind: adapter.Kind(), ConfigPath: adapter.ConfigPath(), Servers: servers}
		plan.DetectedClients = append(plan.DetectedClients, info)
		for _, server := range servers {
			if server.IsWrappable {
				server.Name = canonicalName(server.Name)
				key := strings.ToLower(server.Name)
				if _, exists := wrappable[key]; !exists {
					wrappable[key] = server
				}
			}
		}
	}

	names := make([]string, 0, len(wrappable))
	for name := range wrappable {
		names = append(names, name)
	}
	sort.Strings(names)

	wrapperDir := filepath.Join(opts.Home, wrappersRel)
	usedPorts := existingLocalPorts(existingDaemonConfigPaths(filepath.Join(opts.Home, daemonRel)))
	for _, name := range names {
		server := wrappable[name]
		configPath := filepath.Join(wrapperDir, safeName(name)+".json")
		content := buildWrapperConfig(opts.Home, server)
		if server.URL != "" {
			port, err := allocateLocalPort(httpPortBase, usedPorts)
			if err != nil {
				plan.Blockers = append(plan.Blockers, fmt.Sprintf("%s: %v", server.Name, err))
			}
			content.LocalPort = port
		}
		plan.WrapperConfigs = append(plan.WrapperConfigs, WrapperConfigPlan{
			Server:     server,
			ConfigPath: configPath,
			Content:    content,
		})
	}

	socketPath := filepath.Join(opts.Home, socketRel)
	daemonConfigPath := filepath.Join(opts.Home, daemonRel)
	mergedConfigPaths := mergeConfigPaths(existingDaemonConfigPaths(daemonConfigPath), configPaths(plan.WrapperConfigs))
	if len(mergedConfigPaths) == 0 {
		plan.Blockers = append(plan.Blockers, "no wrappable stdio MCP servers found")
	}
	daemonData, err := buildDaemonConfigContent(socketPath, mergedConfigPaths)
	if err != nil {
		return Plan{}, err
	}
	plan.DaemonConfig = DaemonConfigPlan{
		ConfigPath:  daemonConfigPath,
		SocketPath:  socketPath,
		ConfigPaths: mergedConfigPaths,
		Content:     daemonData,
	}

	for _, adapter := range scanClients(opts.Home) {
		servers, err := adapter.ReadServers()
		if err != nil {
			continue
		}
		next := replaceWithWrapperRefs(servers, opts.BinaryPath, socketPath, localPortsByName(plan.WrapperConfigs))
		newContent, err := renderAdapterContent(adapter, next)
		if err != nil {
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("%s render: %v", adapter.Kind(), err))
			continue
		}
		currentContent, err := os.ReadFile(adapter.ConfigPath())
		if err == nil && bytes.Equal(currentContent, newContent) {
			continue
		}
		plan.ClientUpdates = append(plan.ClientUpdates, ClientUpdate{
			Kind:       adapter.Kind(),
			ConfigPath: adapter.ConfigPath(),
			BackupPath: backupPath(adapter.ConfigPath(), opts.Now),
			Servers:    next,
			NewContent: newContent,
		})
	}

	plan.LaunchAgent = defaultLaunchAgentPlan(opts)

	return plan, nil
}

func (p Plan) Apply(opts Options) error {
	opts = normalizeOptions(opts)
	if len(p.Blockers) > 0 {
		return fmt.Errorf("setup has blockers: %s", strings.Join(p.Blockers, "; "))
	}

	if len(p.WrapperConfigs) > 0 && shouldApply(opts, "Step 1/3: Create wrapper configs in "+filepath.Join(opts.Home, wrappersRel)+"?") {
		if err := writeWrapperConfigs(p.WrapperConfigs); err != nil {
			return err
		}
	}
	if shouldApply(opts, "Step 2/3: Install daemon as macOS LaunchAgent?") {
		if err := writeDaemonConfig(p.DaemonConfig); err != nil {
			return err
		}
		if err := installLaunchAgent(p.LaunchAgent, opts.execFunc()); err != nil {
			return err
		}
	}
	if len(p.ClientUpdates) > 0 && shouldApply(opts, "Step 3/3: Update client configs with backups?") {
		adaptersByKind := map[string]ClientAdapter{}
		for _, adapter := range scanClients(opts.Home) {
			adaptersByKind[adapter.Kind()] = adapter
		}
		for _, update := range p.ClientUpdates {
			if err := os.MkdirAll(filepath.Dir(update.BackupPath), 0755); err != nil {
				return err
			}
			adapter := adaptersByKind[update.Kind]
			if adapter == nil {
				return fmt.Errorf("adapter not found for %s", update.Kind)
			}
			if err := adapter.WriteServers(update.Servers, update.BackupPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func (o Options) execFunc() execFunc {
	if o.Exec != nil {
		return o.Exec
	}
	return realExec
}

func normalizeOptions(opts Options) Options {
	if opts.Home == "" {
		if home, err := os.UserHomeDir(); err == nil {
			opts.Home = home
		}
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	return opts
}

func buildWrapperConfig(home string, server RawServer) wrapper.Config {
	sharing := "shared"
	if isStateful(server.Name, server.Command, server.Args) {
		sharing = "session"
	}
	cfg := wrapper.Config{
		Name:           server.Name,
		Sharing:        sharing,
		RealProtocol:   "2024-11-05",
		RealFraming:    "jsonl",
		IdleTimeout:    wrapper.Duration{Duration: 5 * time.Minute},
		StartupTimeout: wrapper.Duration{Duration: 30 * time.Second},
		CallTimeout:    wrapper.Duration{Duration: 180 * time.Second},
		LogFile:        filepath.Join(os.TempDir(), "lazy-mcp-wrapper-"+safeName(server.Name)+".log"),
	}
	if server.URL != "" {
		cfg.URL = server.URL
		cfg.Protocol = server.Type
		cfg.Headers = server.Headers
		cfg.RealProtocol = ""
		cfg.RealFraming = ""
		return cfg
	}
	cfg.Command = server.Command
	cfg.Args = server.Args
	cfg.Env = server.Env
	return cfg
}

func isStateful(name, command string, args []string) bool {
	value := strings.ToLower(name + " " + command + " " + strings.Join(args, " "))
	return strings.Contains(value, "playwright")
}

func configPaths(configs []WrapperConfigPlan) []string {
	paths := make([]string, 0, len(configs))
	for _, cfg := range configs {
		paths = append(paths, cfg.ConfigPath)
	}
	return paths
}

func existingDaemonConfigPaths(path string) []string {
	cfg, err := daemon.LoadConfig(path)
	if err != nil {
		return nil
	}
	return cfg.ConfigPaths
}

func mergeConfigPaths(existing, next []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, path := range append(existing, next...) {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

func replaceWithWrapperRefs(servers []RawServer, binaryPath, socketPath string, localPorts map[string]int) []RawServer {
	out := make([]RawServer, 0, len(servers))
	for _, server := range servers {
		next := server
		if server.IsWrappable {
			next.Name = canonicalName(server.Name)
			if server.URL != "" {
				next.Type = defaultType(server.Type)
				next.URL = localHTTPAddr(localPorts[strings.ToLower(next.Name)])
				next.Command = ""
				next.Args = nil
				next.Env = nil
				next.Headers = nil
			} else {
				next.Type = "stdio"
				next.Command = binaryPath
				next.Args = []string{"client", "--socket", socketPath, "--name", next.Name}
				next.Env = nil
			}
			next.Raw = nil
			next.Env = nil
			next.IsWrappable = false
		}
		out = append(out, next)
	}
	return out
}

func allocateLocalPort(start int, used map[int]bool) (int, error) {
	for port := start; port < start+200; port++ {
		if used[port] {
			continue
		}
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		_ = ln.Close()
		used[port] = true
		return port, nil
	}
	return 0, errors.New("no available port in range")
}

func existingLocalPorts(configPaths []string) map[int]bool {
	used := map[int]bool{}
	for _, path := range configPaths {
		cfg, err := wrapper.LoadConfig(path)
		if err != nil {
			continue
		}
		if cfg.LocalPort > 0 {
			used[cfg.LocalPort] = true
		}
	}
	return used
}

func localPortsByName(configs []WrapperConfigPlan) map[string]int {
	ports := map[string]int{}
	for _, cfg := range configs {
		if cfg.Content.LocalPort > 0 {
			ports[strings.ToLower(canonicalName(cfg.Content.Name))] = cfg.Content.LocalPort
		}
	}
	return ports
}

func localHTTPAddr(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func writeWrapperConfigs(configs []WrapperConfigPlan) error {
	for _, cfg := range configs {
		if err := os.MkdirAll(filepath.Dir(cfg.ConfigPath), 0755); err != nil {
			return err
		}
		data, err := json.MarshalIndent(cfg.Content, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if err := os.WriteFile(cfg.ConfigPath, data, 0644); err != nil {
			return err
		}
	}
	return nil
}

func writeDaemonConfig(plan DaemonConfigPlan) error {
	if err := os.MkdirAll(filepath.Dir(plan.ConfigPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(plan.ConfigPath, plan.Content, 0644)
}

func buildDaemonConfigContent(socketPath string, configPaths []string) ([]byte, error) {
	daemonData, err := json.MarshalIndent(map[string]any{
		"socket":  socketPath,
		"configs": configPaths,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(daemonData, '\n'), nil
}

func defaultDaemonConfigPlan(home string, configPaths []string) (DaemonConfigPlan, error) {
	configPath := filepath.Join(home, daemonRel)
	socketPath := filepath.Join(home, socketRel)
	if cfg, err := daemon.LoadConfig(configPath); err == nil && cfg.SocketPath != "" {
		socketPath = cfg.SocketPath
	}
	content, err := buildDaemonConfigContent(socketPath, configPaths)
	if err != nil {
		return DaemonConfigPlan{}, err
	}
	return DaemonConfigPlan{
		ConfigPath:  configPath,
		SocketPath:  socketPath,
		ConfigPaths: configPaths,
		Content:     content,
	}, nil
}

func defaultLaunchAgentPlan(opts Options) LaunchAgentPlan {
	opts = normalizeOptions(opts)
	plistPath := filepath.Join(opts.Home, "Library", "LaunchAgents", defaultLabel+".plist")
	socketPath := filepath.Join(opts.Home, socketRel)
	daemonConfigPath := filepath.Join(opts.Home, daemonRel)
	logDir := filepath.Join(opts.Home, "Library", "Logs", "lazy-mcp-wrapper")
	pathValue := os.Getenv("PATH")
	plan := LaunchAgentPlan{
		Label:              defaultLabel,
		PlistPath:          plistPath,
		SocketPath:         socketPath,
		SocketPollAttempts: 50,
		DaemonConfig:       daemonConfigPath,
		BinaryPath:         opts.BinaryPath,
		LogDir:             logDir,
		PATH:               pathValue,
	}
	plan.Content = []byte(buildPlistXML(plan))
	return plan
}

func backupPath(path string, now time.Time) string {
	return fmt.Sprintf("%s.bak-%s", path, now.Format("20060102150405"))
}

func safeName(name string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-")
	return replacer.Replace(name)
}

func canonicalName(name string) string {
	trimmed := strings.TrimSpace(name)
	if strings.EqualFold(trimmed, "playwright") {
		return "playwright"
	}
	return trimmed
}
