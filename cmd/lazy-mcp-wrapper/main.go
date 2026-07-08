package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/daemon"
	"github.com/binlee/lazy-mcp-wrapper/internal/oauth"
	"github.com/binlee/lazy-mcp-wrapper/internal/setup"
	"github.com/binlee/lazy-mcp-wrapper/internal/wrapper"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			printVersion()
			return
		case "--version":
			printVersion()
			return
		case "daemon":
			runDaemon(os.Args[2:])
			return
		case "client":
			runClient(os.Args[2:])
			return
		case "status":
			runStatus(os.Args[2:])
			return
		case "stop":
			runControl(os.Args[2:], "stop")
			return
		case "reload":
			runControl(os.Args[2:], "reload")
			return
		case "setup":
			runSetup(os.Args[2:])
			return
		case "auth":
			runAuth(os.Args[2:])
			return
		}
	}
	configPath := flag.String("config", "", "path to wrapper JSON config")
	printExample := flag.Bool("print-example", false, "print example config")
	refreshCache := flag.Bool("refresh-cache", false, "refresh tools/list cache and exit")
	clearCache := flag.Bool("clear-cache", false, "clear tools/list cache and exit")
	inspect := flag.Bool("inspect", false, "print resolved config and cache status as JSON and exit")
	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *versionFlag {
		printVersion()
		return
	}
	if *printExample {
		fmt.Println(exampleConfig)
		return
	}
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "missing --config")
		os.Exit(2)
	}

	cfg, err := wrapper.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(2)
	}

	logger, closer, err := wrapper.NewLogger(cfg.LogFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open log: %v\n", err)
		os.Exit(2)
	}
	if closer != nil {
		defer closer.Close()
	}
	if *inspect {
		data, _ := json.MarshalIndent(wrapper.Inspect(cfg), "", "  ")
		fmt.Println(string(data))
		return
	}
	if *refreshCache {
		info, err := wrapper.RefreshToolsCache(context.Background(), cfg, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "refresh cache: %v\n", err)
			os.Exit(1)
		}
		data, _ := json.MarshalIndent(info, "", "  ")
		fmt.Println(string(data))
		return
	}
	if *clearCache {
		info, err := cfg.ClearCache()
		if err != nil {
			fmt.Fprintf(os.Stderr, "clear cache: %v\n", err)
			os.Exit(1)
		}
		data, _ := json.MarshalIndent(info, "", "  ")
		fmt.Println(string(data))
		return
	}

	proxy := wrapper.NewProxy(cfg, logger)
	if err := proxy.Run(context.Background(), os.Stdin, os.Stdout); err != nil {
		logger.Printf("wrapper stopped: %v", err)
		os.Exit(1)
	}
}

func printVersion() {
	fmt.Printf("lazy-mcp-wrapper %s\n", version)
}

func runDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	socketPath := fs.String("socket", "", "Unix socket path")
	daemonConfigPath := fs.String("daemon-config", "", "daemon JSON config")
	configPaths := multiFlag{}
	fs.Var(&configPaths, "config", "wrapper JSON config; can be repeated")
	_ = fs.Parse(args)

	var server *daemon.Server
	if *daemonConfigPath != "" {
		loaded, err := daemon.LoadConfig(*daemonConfigPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load daemon config %s: %v\n", *daemonConfigPath, err)
			os.Exit(2)
		}
		*socketPath = loaded.SocketPath
		server, err = daemon.NewServerFromConfig(*daemonConfigPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "create daemon: %v\n", err)
			os.Exit(2)
		}
	} else {
		if *socketPath == "" {
			fmt.Fprintln(os.Stderr, "missing --socket")
			os.Exit(2)
		}
		if len(configPaths) == 0 {
			fmt.Fprintln(os.Stderr, "missing --config")
			os.Exit(2)
		}

		configs := make([]wrapper.Config, 0, len(configPaths))
		loggers := make(map[string]*log.Logger, len(configPaths))
		closers := make([]io.Closer, 0, len(configPaths))
		for _, path := range configPaths {
			cfg, err := wrapper.LoadConfig(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "load config %s: %v\n", path, err)
				os.Exit(2)
			}
			logger, closer, err := wrapper.NewLogger(cfg.LogFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "open log for %s: %v\n", cfg.Name, err)
				os.Exit(2)
			}
			if closer != nil {
				closers = append(closers, closer)
			}
			configs = append(configs, cfg)
			loggers[cfg.Name] = logger
			logger.Printf("daemon registered MCP %s", cfg.Name)
		}
		defer func() {
			for _, closer := range closers {
				_ = closer.Close()
			}
		}()

		var err error
		server, err = daemon.NewServer(*socketPath, configs, loggers)
		if err != nil {
			fmt.Fprintf(os.Stderr, "create daemon: %v\n", err)
			os.Exit(2)
		}
	}

	if handled, err := runWindowsServiceIfNeeded(*socketPath, server); handled {
		if err != nil {
			fmt.Fprintf(os.Stderr, "Windows Service stopped: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals...)
	defer stop()
	fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper daemon listening on %s\n", *socketPath)
	if err := server.Serve(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "daemon stopped: %v\n", err)
		os.Exit(1)
	}
}

func runClient(args []string) {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	socketPath := fs.String("socket", "", "Unix socket path")
	name := fs.String("name", "", "MCP name registered in daemon")
	_ = fs.Parse(args)

	if err := daemon.RunClient(*socketPath, *name, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper client: %v\n", err)
		os.Exit(1)
	}
}

func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	socketPath := fs.String("socket", "", "Unix socket path")
	format := fs.String("format", "json", "output format: json or table")
	_ = fs.Parse(args)

	status, err := daemon.QueryStatus(*socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper status: %v\n", err)
		os.Exit(1)
	}
	switch *format {
	case "json":
	case "table":
		printStatusTable(os.Stdout, status)
		return
	default:
		fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper status: unsupported format %q\n", *format)
		os.Exit(2)
	}
	data, _ := json.MarshalIndent(status, "", "  ")
	fmt.Println(string(data))
}

func printStatusTable(out io.Writer, status daemon.Status) {
	fmt.Fprintf(out, "Socket: %s\n", status.SocketPath)
	if status.DaemonConfigPath != "" {
		fmt.Fprintf(out, "Config: %s\n", status.DaemonConfigPath)
	}
	fmt.Fprintf(out, "Daemon: pid=%d uptime=%s clients=%d total_calls=%d\n", status.DaemonPID, status.Uptime, status.Clients, status.TotalCalls)
	if status.LastError != "" {
		fmt.Fprintf(out, "Last error: %s\n", status.LastError)
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Servers:")
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSHARE\tREAL\tPID\tSESS\tCALLS\tERRS\tLAT(ms)\tLAST METHOD\tLAST USED")
	for _, server := range status.Servers {
		realState := "down"
		if server.RealAlive {
			realState = "up"
		} else if server.Sharing == "session" {
			realState = "session"
		}
		pid := "-"
		if server.RealPID > 0 {
			pid = fmt.Sprintf("%d", server.RealPID)
		}
		latency := "-"
		if server.LastLatencyMS > 0 || server.AvgLatencyMS > 0 || server.MaxLatencyMS > 0 {
			latency = fmt.Sprintf("%d/%d/%d", server.LastLatencyMS, server.AvgLatencyMS, server.MaxLatencyMS)
		}
		lastMethod := server.LastMethod
		if lastMethod == "" {
			lastMethod = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%d\t%d\t%s\t%s\t%s\n",
			server.Name,
			server.Sharing,
			realState,
			pid,
			server.ActiveSessions,
			server.Calls,
			server.Errors,
			latency,
			lastMethod,
			formatTimePtr(server.LastUsedAt),
		)
	}
	_ = tw.Flush()

	if len(status.ActiveClients) == 0 {
		fmt.Fprintln(out, "\nActive clients: none")
		return
	}
	fmt.Fprintln(out, "\nActive clients:")
	tw = tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tMCP\tSHARE\tGEN\tCONNECTED\tREMOTE")
	for _, client := range status.ActiveClients {
		remote := client.RemoteAddr
		if remote == "" {
			remote = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n", client.ID, client.Name, client.Sharing, client.Generation, formatTime(client.ConnectedAt), remote)
	}
	_ = tw.Flush()
}

func formatTimePtr(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "-"
	}
	return formatTime(*value)
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Format("2006-01-02 15:04:05")
}

func runControl(args []string, control string) {
	fs := flag.NewFlagSet(control, flag.ExitOnError)
	socketPath := fs.String("socket", "", "Unix socket path")
	force := false
	graceful := false
	if control == "reload" {
		fs.BoolVar(&force, "force", false, "force reload even when active clients are connected")
		fs.BoolVar(&graceful, "graceful", false, "reload new proxies while active clients continue using old proxies")
	}
	_ = fs.Parse(args)

	resp, err := daemon.SendControl(*socketPath, control, daemon.ControlOptions{Force: force, Graceful: graceful})
	if err != nil {
		fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper %s: %v\n", control, err)
		os.Exit(1)
	}
	data, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Println(string(data))
	if !resp.OK {
		os.Exit(1)
	}
}

func runAuth(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: lazy-mcp-wrapper auth <status|login|logout> [name]")
		os.Exit(2)
	}
	subcmd := args[0]

	resolveHome := func(home string) string {
		if home != "" {
			return home
		}
		resolved, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper auth: resolve home: %v\n", err)
			os.Exit(2)
		}
		return resolved
	}

	switch subcmd {
	case "status":
		home, format, rest, parseErr := parseAuthFlags(args[1:], true)
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper auth status: %v\n", parseErr)
			os.Exit(2)
		}
		if len(rest) > 1 {
			fmt.Fprintln(os.Stderr, "usage: lazy-mcp-wrapper auth status [name] [--home DIR] [--format table|json]")
			os.Exit(2)
		}
		store := oauth.NewFileStore(resolveHome(home))
		var (
			value     any
			statusErr error
		)
		if len(rest) == 1 {
			value, statusErr = store.Status(rest[0])
		} else {
			value, statusErr = store.ListStatuses()
		}
		if statusErr != nil {
			fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper auth status: %v\n", statusErr)
			os.Exit(1)
		}
		switch format {
		case "json":
			data, _ := json.MarshalIndent(value, "", "  ")
			fmt.Println(string(data))
		case "table":
			printAuthStatusTable(os.Stdout, value)
		default:
			fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper auth status: unsupported format %q\n", format)
			os.Exit(2)
		}
	case "login":
		loginFlags, rest, err := parseAuthLoginFlags(args[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper auth login: %v\n", err)
			os.Exit(2)
		}
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, "usage: lazy-mcp-wrapper auth login <name> [--config PATH|--url URL] [--client-id ID] [--token-url URL] [--scope SCOPE] [--callback-port PORT] [--timeout 5m] [--no-open]")
			os.Exit(2)
		}
		name := rest[0]
		home := resolveHome(loginFlags.home)
		loginOpts, err := buildOAuthLoginOptions(home, name, loginFlags)
		if err != nil {
			fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper auth login: %v\n", err)
			os.Exit(2)
		}
		loginOpts.Out = os.Stdout
		ctx := context.Background()
		var cancel context.CancelFunc
		if loginFlags.timeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, loginFlags.timeout)
			defer cancel()
		}
		status, err := oauth.Login(ctx, loginOpts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper auth login: %v\n", err)
			os.Exit(1)
		}
		printAuthStatusTable(os.Stdout, status)
	case "logout":
		home, _, rest, err := parseAuthFlags(args[1:], false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper auth logout: %v\n", err)
			os.Exit(2)
		}
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, "usage: lazy-mcp-wrapper auth logout <name>")
			os.Exit(2)
		}
		store := oauth.NewFileStore(resolveHome(home))
		if err := store.Delete(rest[0]); err != nil {
			fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper auth logout: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, "Removed OAuth credential for %s\n", rest[0])
	default:
		fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper auth: unknown subcommand %q\n", subcmd)
		os.Exit(2)
	}
}

type authLoginFlags struct {
	home         string
	configPath   string
	url          string
	clientID     string
	tokenURL     string
	resource     string
	scopes       []string
	callbackPort int
	openBrowser  bool
	timeout      time.Duration
}

func parseAuthLoginFlags(args []string) (authLoginFlags, []string, error) {
	flags := authLoginFlags{openBrowser: true, timeout: 5 * time.Minute}
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--home":
			i++
			if i >= len(args) {
				return flags, nil, fmt.Errorf("missing value for --home")
			}
			flags.home = args[i]
		case strings.HasPrefix(arg, "--home="):
			flags.home = strings.TrimPrefix(arg, "--home=")
		case arg == "--config":
			i++
			if i >= len(args) {
				return flags, nil, fmt.Errorf("missing value for --config")
			}
			flags.configPath = args[i]
		case strings.HasPrefix(arg, "--config="):
			flags.configPath = strings.TrimPrefix(arg, "--config=")
		case arg == "--url":
			i++
			if i >= len(args) {
				return flags, nil, fmt.Errorf("missing value for --url")
			}
			flags.url = args[i]
		case strings.HasPrefix(arg, "--url="):
			flags.url = strings.TrimPrefix(arg, "--url=")
		case arg == "--client-id":
			i++
			if i >= len(args) {
				return flags, nil, fmt.Errorf("missing value for --client-id")
			}
			flags.clientID = args[i]
		case strings.HasPrefix(arg, "--client-id="):
			flags.clientID = strings.TrimPrefix(arg, "--client-id=")
		case arg == "--token-url":
			i++
			if i >= len(args) {
				return flags, nil, fmt.Errorf("missing value for --token-url")
			}
			flags.tokenURL = args[i]
		case strings.HasPrefix(arg, "--token-url="):
			flags.tokenURL = strings.TrimPrefix(arg, "--token-url=")
		case arg == "--resource":
			i++
			if i >= len(args) {
				return flags, nil, fmt.Errorf("missing value for --resource")
			}
			flags.resource = args[i]
		case strings.HasPrefix(arg, "--resource="):
			flags.resource = strings.TrimPrefix(arg, "--resource=")
		case arg == "--scope":
			i++
			if i >= len(args) {
				return flags, nil, fmt.Errorf("missing value for --scope")
			}
			flags.scopes = append(flags.scopes, args[i])
		case strings.HasPrefix(arg, "--scope="):
			flags.scopes = append(flags.scopes, strings.TrimPrefix(arg, "--scope="))
		case arg == "--timeout":
			i++
			if i >= len(args) {
				return flags, nil, fmt.Errorf("missing value for --timeout")
			}
			timeout, err := time.ParseDuration(args[i])
			if err != nil {
				return flags, nil, fmt.Errorf("invalid --timeout: %w", err)
			}
			flags.timeout = timeout
		case strings.HasPrefix(arg, "--timeout="):
			timeout, err := time.ParseDuration(strings.TrimPrefix(arg, "--timeout="))
			if err != nil {
				return flags, nil, fmt.Errorf("invalid --timeout: %w", err)
			}
			flags.timeout = timeout
		case arg == "--callback-port":
			i++
			if i >= len(args) {
				return flags, nil, fmt.Errorf("missing value for --callback-port")
			}
			port, err := parsePositiveInt(args[i])
			if err != nil {
				return flags, nil, fmt.Errorf("invalid --callback-port: %w", err)
			}
			flags.callbackPort = port
		case strings.HasPrefix(arg, "--callback-port="):
			port, err := parsePositiveInt(strings.TrimPrefix(arg, "--callback-port="))
			if err != nil {
				return flags, nil, fmt.Errorf("invalid --callback-port: %w", err)
			}
			flags.callbackPort = port
		case arg == "--no-open":
			flags.openBrowser = false
		case strings.HasPrefix(arg, "-"):
			return flags, nil, fmt.Errorf("unknown flag %s", arg)
		default:
			rest = append(rest, arg)
		}
	}
	return flags, rest, nil
}

func buildOAuthLoginOptions(home, name string, flags authLoginFlags) (oauth.LoginOptions, error) {
	cfg := wrapper.Config{Name: name}
	configPath := flags.configPath
	if configPath == "" {
		candidate := filepath.Join(home, ".lazy-mcp-wrapper", "wrappers", name+".json")
		if _, err := os.Stat(candidate); err == nil {
			configPath = candidate
		}
	}
	if configPath != "" {
		loaded, err := loadOAuthLoginConfig(configPath, home, name)
		if err != nil {
			return oauth.LoginOptions{}, err
		}
		cfg = loaded
	} else if loaded, _, err := setup.FindClientWrapperConfig(home, name); err == nil {
		cfg = loaded
	}
	if flags.url != "" {
		cfg.URL = flags.url
	}
	if flags.clientID != "" {
		cfg.OAuthClientID = flags.clientID
	}
	if flags.resource != "" {
		cfg.OAuthResource = flags.resource
	}
	if len(flags.scopes) > 0 {
		cfg.OAuthScopes = flags.scopes
	}
	if cfg.URL == "" {
		return oauth.LoginOptions{}, fmt.Errorf("missing remote MCP URL; pass --url or --config")
	}
	store := oauth.NewFileStore(home)
	if cfg.OAuthStoreDir != "" {
		store = &oauth.FileStore{Dir: cfg.OAuthStoreDir}
	}
	return oauth.LoginOptions{
		Name:         name,
		ServerURL:    cfg.URL,
		ClientID:     cfg.OAuthClientID,
		TokenURL:     flags.tokenURL,
		Resource:     cfg.OAuthResource,
		Scopes:       cfg.OAuthScopes,
		Store:        store,
		CallbackPort: flags.callbackPort,
		OpenBrowser:  flags.openBrowser,
	}, nil
}

func loadOAuthLoginConfig(path, home, name string) (wrapper.Config, error) {
	loaded, wrapperErr := wrapper.LoadConfig(path)
	if wrapperErr == nil {
		return loaded, nil
	}
	loaded, clientErr := setup.LoadClientWrapperConfig(path, home, name)
	if clientErr == nil {
		return loaded, nil
	}
	return wrapper.Config{}, fmt.Errorf("load config %s: wrapper config error: %v; client config error: %v", path, wrapperErr, clientErr)
}

func parsePositiveInt(value string) (int, error) {
	out, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	if out < 0 {
		return 0, fmt.Errorf("must be >= 0")
	}
	return out, nil
}

func parseAuthFlags(args []string, allowFormat bool) (home string, format string, rest []string, err error) {
	format = "table"
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--home":
			i++
			if i >= len(args) {
				return "", "", nil, fmt.Errorf("missing value for --home")
			}
			home = args[i]
		case strings.HasPrefix(arg, "--home="):
			home = strings.TrimPrefix(arg, "--home=")
		case arg == "--format":
			if !allowFormat {
				return "", "", nil, fmt.Errorf("--format is only supported by auth status")
			}
			i++
			if i >= len(args) {
				return "", "", nil, fmt.Errorf("missing value for --format")
			}
			format = args[i]
		case strings.HasPrefix(arg, "--format="):
			if !allowFormat {
				return "", "", nil, fmt.Errorf("--format is only supported by auth status")
			}
			format = strings.TrimPrefix(arg, "--format=")
		case strings.HasPrefix(arg, "-"):
			return "", "", nil, fmt.Errorf("unknown flag %s", arg)
		default:
			rest = append(rest, arg)
		}
	}
	return home, format, rest, nil
}

func printAuthStatusTable(out io.Writer, value any) {
	statuses, ok := value.([]oauth.Status)
	if !ok {
		statuses = []oauth.Status{value.(oauth.Status)}
	}
	if len(statuses) == 0 {
		fmt.Fprintln(out, "No OAuth credentials found")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tAUTH\tEXPIRED\tEXPIRES\tRESOURCE\tPATH")
	for _, status := range statuses {
		authenticated := "no"
		if status.Authenticated {
			authenticated = "yes"
		}
		expired := "no"
		if status.Expired {
			expired = "yes"
		}
		expires := "-"
		if status.Expiry != nil {
			expires = formatTime(*status.Expiry)
		}
		resource := status.Resource
		if resource == "" {
			resource = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", status.Name, authenticated, expired, expires, resource, status.Path)
	}
	_ = tw.Flush()
}

func runSetup(args []string) {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	yes := fs.Bool("yes", false, "apply all changes without prompts")
	dryRun := fs.Bool("dry-run", false, "print setup plan without applying")
	home := fs.String("home", "", "home directory to scan; defaults to current user home")
	binaryPath := fs.String("bin", "", "lazy-mcp-wrapper binary path; defaults to current executable")
	configPaths := multiFlag{}
	fs.Var(&configPaths, "config", "client MCP config to scan; can be repeated and later entries override earlier entries by server name")
	_ = fs.Parse(args)
	subcmd := ""
	subcmdArgs := fs.Args()
	if len(subcmdArgs) > 0 {
		subcmd = subcmdArgs[0]
		subcmdArgs = subcmdArgs[1:]
	}
	if subcmd != "" {
		subFS := flag.NewFlagSet("setup "+subcmd, flag.ExitOnError)
		subFS.BoolVar(yes, "yes", *yes, "apply all changes without prompts")
		subFS.BoolVar(dryRun, "dry-run", *dryRun, "print setup plan without applying")
		subFS.StringVar(home, "home", *home, "home directory to scan; defaults to current user home")
		subFS.StringVar(binaryPath, "bin", *binaryPath, "lazy-mcp-wrapper binary path; defaults to current executable")
		subFS.Var(&configPaths, "config", "client MCP config to scan; can be repeated and later entries override earlier entries by server name")
		_ = subFS.Parse(subcmdArgs)
	}

	bin := *binaryPath
	if bin == "" {
		if exe, err := os.Executable(); err == nil {
			bin = exe
		}
	}
	opts := setup.Options{
		Home:        *home,
		BinaryPath:  bin,
		ConfigPaths: []string(configPaths),
		YesAll:      *yes,
		DryRun:      *dryRun,
	}
	switch subcmd {
	case "status":
		setup.PrintStatusReport(os.Stdout, setup.Status(opts))
		return
	case "verify":
		results := setup.Verify(opts)
		ok := true
		for _, r := range results {
			if r.Err != nil {
				fmt.Fprintf(os.Stdout, "  %-30s ERROR  %v\n", r.Name, r.Err)
				ok = false
			} else {
				fmt.Fprintf(os.Stdout, "  %-30s OK     %d tools  (%s)\n", r.Name, r.ToolCount, r.Elapsed.Round(time.Millisecond))
			}
		}
		if !ok {
			os.Exit(1)
		}
		return
	case "uninstall":
		if err := setup.Uninstall(opts); err != nil {
			fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper setup uninstall: %v\n", err)
			os.Exit(1)
		}
		return
	case "update":
		if err := setup.Update(opts); err != nil {
			fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper setup update: %v\n", err)
			os.Exit(1)
		}
		return
	case "":
	default:
		fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper setup: unknown subcommand %q\n", subcmd)
		os.Exit(2)
	}
	plan, err := setup.NewPlan(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper setup: %v\n", err)
		os.Exit(1)
	}
	setup.PrintPlan(os.Stdout, plan)
	if *dryRun {
		return
	}
	if err := plan.Apply(opts); err != nil {
		fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper setup: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "\nDone. Run `%s setup verify` to confirm all servers are reachable.\n", filepath.Base(bin))
}

type multiFlag []string

func (m *multiFlag) String() string {
	data, _ := json.Marshal([]string(*m))
	return string(data)
}

func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

const exampleConfig = `{
  "name": "context7",
  "command": "npx",
  "args": ["-y", "@upstash/context7-mcp"],
  "idle_timeout": "30s",
  "startup_timeout": "20s",
  "call_timeout": "120s",
  "log_file": "/tmp/lazy-mcp-wrapper-context7.log"
}`
