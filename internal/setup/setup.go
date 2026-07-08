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
	oauthstore "github.com/binlee/lazy-mcp-wrapper/internal/oauth"
	"github.com/binlee/lazy-mcp-wrapper/internal/wrapper"
)

const (
	defaultLabel = "com.binlee.lazy-mcp-wrapper"
	httpPortBase = 54300
)

type Options struct {
	Home        string
	BinaryPath  string
	ConfigPaths []string
	YesAll      bool
	DryRun      bool
	Now         time.Time
	Exec        execFunc
}

type Plan struct {
	DetectedClients []ClientInfo
	WrapperConfigs  []WrapperConfigPlan
	Decisions       []ServerDecision
	ClientUpdates   []ClientUpdate
	DaemonConfig    DaemonConfigPlan
	LaunchAgent     LaunchAgentPlan
	Blockers        []string
}

type ServerDecision struct {
	ClientKind string
	ConfigPath string
	Name       string
	Action     string
	Reason     string
	Detail     string
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
	socketPath := socketPath(opts.Home)
	skippedByName := map[string]bool{}

	if opts.BinaryPath == "" {
		plan.Blockers = append(plan.Blockers, "binary path is required")
	}

	wrappable := map[string]RawServer{}
	oauthSkipped := map[string]RawServer{}
	chatGPTSkipped := map[string]RawServer{}
	explicitConfigs := len(opts.ConfigPaths) > 0
	for _, adapter := range scanClients(opts.Home, opts.ConfigPaths) {
		servers, err := adapter.ReadServers()
		if err != nil {
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("%s: %v", adapter.Kind(), err))
			continue
		}
		info := ClientInfo{Kind: adapter.Kind(), ConfigPath: adapter.ConfigPath(), Servers: servers}
		plan.DetectedClients = append(plan.DetectedClients, info)
		for _, server := range servers {
			server.Name = canonicalName(server.Name)
			if server.Name == "" {
				continue
			}
			key := strings.ToLower(server.Name)
			if explicitConfigs {
				delete(wrappable, key)
				delete(oauthSkipped, key)
				delete(chatGPTSkipped, key)
				delete(skippedByName, key)
			}
			if isWrapperRef(server, socketPath) || isHTTPWrapperRef(server) {
				plan.Decisions = append(plan.Decisions, serverDecision(adapter, server, "skip", "skipped-existing-wrapper", "already points to lazy-mcp-wrapper"))
				continue
			}
			if canWrapServer(opts.Home, server) {
				server.IsWrappable = true
				plan.Decisions = append(plan.Decisions, serverDecision(adapter, server, "wrap", wrappedReason(server), "will be managed by lazy-mcp-wrapper"))
				if explicitConfigs {
					wrappable[key] = server
				} else if _, exists := wrappable[key]; !exists {
					wrappable[key] = server
				}
			} else {
				skippedByName[key] = true
				plan.Decisions = append(plan.Decisions, serverDecision(adapter, server, "skip", skippedReason(server), skippedDetail(server)))
				if isChatGPTManagedRemoteMCP(server) {
					chatGPTSkipped[key] = server
				} else if isOAuthManagedRemoteMCP(server) {
					oauthSkipped[key] = server
				}
			}
		}
	}
	remoteBlockers := remoteSetupBlockers(wrappable, oauthSkipped, chatGPTSkipped)

	names := make([]string, 0, len(wrappable))
	for name := range wrappable {
		names = append(names, name)
	}
	sort.Strings(names)

	wrapperDir := wrappersDir(opts.Home)
	usedPorts := existingLocalPorts(existingDaemonConfigPaths(daemonConfigPath(opts.Home)))
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

	daemonConfigPath := daemonConfigPath(opts.Home)
	mergedConfigPaths := mergeConfigPathsByName(existingDaemonConfigPaths(daemonConfigPath), configPaths(plan.WrapperConfigs), skippedByName)
	if len(mergedConfigPaths) == 0 {
		if len(remoteBlockers) > 0 {
			plan.Blockers = append(plan.Blockers, remoteBlockers...)
		} else {
			plan.Blockers = append(plan.Blockers, "no wrappable MCP servers found")
		}
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

	for _, adapter := range scanClients(opts.Home, opts.ConfigPaths) {
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

func remoteSetupBlockers(wrappable map[string]RawServer, oauthSkipped, chatGPTSkipped map[string]RawServer) []string {
	var blockers []string
	for name := range chatGPTSkipped {
		if _, willWrap := wrappable[name]; !willWrap {
			blockers = append(blockers, fmt.Sprintf("%s: ChatGPT-auth remote MCP cannot be wrapped; keep it direct in Codex", name))
		}
	}
	for name, server := range oauthSkipped {
		if _, willWrap := wrappable[name]; !willWrap {
			blockers = append(blockers, oauthRemoteBlocker(name, server))
		}
	}
	sort.Strings(blockers)
	return blockers
}

func serverDecision(adapter ClientAdapter, server RawServer, action, reason, detail string) ServerDecision {
	return ServerDecision{
		ClientKind: adapter.Kind(),
		ConfigPath: adapter.ConfigPath(),
		Name:       server.Name,
		Action:     action,
		Reason:     reason,
		Detail:     detail,
	}
}

func wrappedReason(server RawServer) string {
	if isOAuthManagedRemoteMCP(server) {
		return "wrapped-oauth-credential"
	}
	switch effectiveType(server) {
	case "http", "streamable-http":
		if isLocalHTTPMCP(server) {
			return "wrapped-local-http"
		}
		if strings.EqualFold(server.Auth, "none") {
			return "wrapped-auth-none"
		}
		if hasExplicitHTTPAuth(server) {
			return "wrapped-explicit-auth"
		}
	}
	return "wrapped-stdio"
}

func skippedReason(server RawServer) string {
	if strings.EqualFold(server.Name, "node_repl") || strings.Contains(strings.ToLower(filepath.Base(server.Command)), "node_repl") {
		return "skipped-node-repl"
	}
	if isChatGPTManagedRemoteMCP(server) {
		return "skipped-chatgpt-auth"
	}
	if isFigmaRemoteMCP(server) && server.OAuthClientID == "" {
		return "skipped-figma-dynamic-client-rejected"
	}
	if isOAuthManagedRemoteMCP(server) {
		return "skipped-oauth-missing-credential"
	}
	if server.URL != "" && !isLocalHTTPMCP(server) && !strings.EqualFold(server.Auth, "none") && !hasExplicitHTTPAuth(server) {
		return "skipped-url-only-remote"
	}
	return "skipped-invalid-config"
}

func skippedDetail(server RawServer) string {
	switch skippedReason(server) {
	case "skipped-node-repl":
		return "node_repl keeps process-local state and should stay direct"
	case "skipped-chatgpt-auth":
		return "Codex owns per-session ChatGPT auth headers"
	case "skipped-figma-dynamic-client-rejected":
		return "Figma rejected dynamic OAuth client registration"
	case "skipped-oauth-missing-credential":
		return "run auth login before wrapping this OAuth remote"
	case "skipped-url-only-remote":
		return "remote HTTP auth model is not explicit"
	default:
		return "not wrappable by current setup rules"
	}
}

func oauthRemoteBlocker(name string, server RawServer) string {
	if isFigmaRemoteMCP(server) && server.OAuthClientID == "" {
		return fmt.Sprintf("%s: Figma MCP is kept direct; Figma rejects dynamic OAuth client registration, and no oauth.client_id is configured for wrapper-managed login", name)
	}
	if server.OAuthClientID != "" {
		return fmt.Sprintf("%s: OAuth credential not found or expired; run lazy-mcp-wrapper auth login %s --config <client-mcp-config>", name, name)
	}
	return fmt.Sprintf("%s: OAuth credential not found or expired; run lazy-mcp-wrapper auth login %s --url <remote-mcp-url>", name, name)
}

func (p Plan) Apply(opts Options) error {
	opts = normalizeOptions(opts)
	if len(p.Blockers) > 0 {
		return fmt.Errorf("setup has blockers: %s", strings.Join(p.Blockers, "; "))
	}

	if len(p.WrapperConfigs) > 0 && shouldApply(opts, "Step 1/3: Create wrapper configs in "+wrappersDir(opts.Home)+"?") {
		if err := writeWrapperConfigs(p.WrapperConfigs); err != nil {
			return err
		}
	}
	if shouldApply(opts, daemonSetupPrompt()) {
		if err := writeDaemonConfig(p.DaemonConfig); err != nil {
			return err
		}
		if err := installLaunchAgent(p.LaunchAgent, opts.execFunc()); err != nil {
			return err
		}
	}
	if len(p.ClientUpdates) > 0 && shouldApply(opts, "Step 3/3: Update client configs with backups?") {
		if err := writeClientUpdates(opts.Home, opts.ConfigPaths, p.ClientUpdates); err != nil {
			return err
		}
	}
	return nil
}

func writeClientUpdates(home string, configPaths []string, updates []ClientUpdate) error {
	adaptersByPath := map[string]ClientAdapter{}
	for _, adapter := range scanClients(home, configPaths) {
		adaptersByPath[adapter.ConfigPath()] = adapter
	}
	for _, update := range updates {
		if err := os.MkdirAll(filepath.Dir(update.BackupPath), 0755); err != nil {
			return err
		}
		adapter := adaptersByPath[update.ConfigPath]
		if adapter == nil {
			return fmt.Errorf("adapter not found for %s", update.ConfigPath)
		}
		if err := adapter.WriteServers(update.Servers, update.BackupPath); err != nil {
			return err
		}
	}
	return nil
}

func daemonSetupPrompt() string {
	switch currentGOOS {
	case "windows":
		return "Step 2/3: Write daemon config and install Windows Service (requires Administrator)?"
	case "darwin":
		return "Step 2/3: Install daemon as macOS LaunchAgent?"
	case "linux":
		return "Step 2/3: Install daemon as systemd user service?"
	default:
		return "Step 2/3: Write daemon config?"
	}
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
	server = normalizeServerCommand(server)
	sharing := "shared"
	if isStateful(server.Name, server.Command, server.Args) || isOAuthManagedRemoteMCP(server) {
		sharing = "session"
	}
	cfg := wrapper.Config{
		SchemaVersion:  wrapper.CurrentSchemaVersion,
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
		cfg.Protocol = effectiveType(server)
		cfg.Auth = server.Auth
		if cfg.Auth == "" && isOAuthManagedRemoteMCP(server) {
			cfg.Auth = "oauth"
		}
		cfg.OAuthClientID = server.OAuthClientID
		cfg.OAuthResource = server.OAuthResource
		cfg.OAuthScopes = server.OAuthScopes
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

func canWrapServer(home string, server RawServer) bool {
	if server.IsWrappable {
		return true
	}
	if !isOAuthManagedRemoteMCP(server) {
		return false
	}
	status, err := oauthstore.NewFileStore(home).Status(server.Name)
	if err != nil {
		return false
	}
	return status.Authenticated && status.HasAccessToken && !status.Expired
}

func normalizeServerCommand(server RawServer) RawServer {
	if server.URL != "" || len(server.Args) > 0 {
		return server
	}
	fields := strings.Fields(server.Command)
	if len(fields) <= 1 || !isKnownInlineCommand(fields[0]) {
		return server
	}
	server.Command = fields[0]
	server.Args = fields[1:]
	return server
}

func isKnownInlineCommand(command string) bool {
	switch filepath.Base(command) {
	case "npx", "npm", "pnpm", "yarn", "bun", "uvx":
		return true
	default:
		return false
	}
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

func mergeConfigPathsByName(existing, next []string, skipped map[string]bool) []string {
	orderedNames := []string{}
	pathsByName := map[string]string{}
	seenPath := map[string]bool{}
	add := func(path string, prefer bool) {
		if path == "" {
			return
		}
		name := configName(path)
		if name == "" {
			name = "path:" + path
		}
		if _, ok := pathsByName[name]; !ok {
			orderedNames = append(orderedNames, name)
		}
		if prefer || pathsByName[name] == "" {
			pathsByName[name] = path
		}
	}
	for _, path := range existing {
		name := configName(path)
		if name != "" && skipped[name] {
			continue
		}
		add(path, false)
	}
	for _, path := range next {
		add(path, true)
	}
	out := make([]string, 0, len(orderedNames))
	for _, name := range orderedNames {
		path := pathsByName[name]
		if path == "" || seenPath[path] {
			continue
		}
		seenPath[path] = true
		out = append(out, path)
	}
	return out
}

func configName(path string) string {
	cfg, err := wrapper.LoadConfig(path)
	if err != nil {
		return ""
	}
	return strings.ToLower(canonicalName(cfg.Name))
}

func replaceWithWrapperRefs(servers []RawServer, binaryPath, socketPath string, localPorts map[string]int) []RawServer {
	out := make([]RawServer, 0, len(servers))
	for _, server := range servers {
		next := server
		name := canonicalName(server.Name)
		_, hasLocalHTTP := localPorts[strings.ToLower(name)]
		if server.IsWrappable || (server.URL != "" && hasLocalHTTP) {
			next.Name = name
			if server.URL != "" {
				next.Type = effectiveType(server)
				next.Auth = ""
				next.OAuthClientID = ""
				next.OAuthResource = ""
				next.OAuthScopes = nil
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
			next.Rewritten = true
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
	configPath := daemonConfigPath(home)
	socketPath := socketPath(home)
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
	plistPath := launchAgentPath(opts.Home)
	socketPath := socketPath(opts.Home)
	daemonConfigPath := daemonConfigPath(opts.Home)
	logDir := logDir(opts.Home)
	pathValue := enrichPATH(os.Getenv("PATH"))
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
	switch currentGOOS {
	case "darwin":
		plan.Content = []byte(buildPlistXML(plan))
	case "linux":
		plan.Content = []byte(buildSystemdUnit(plan))
	}
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
