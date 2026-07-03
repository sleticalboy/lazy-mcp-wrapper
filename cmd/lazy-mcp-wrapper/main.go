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
	"text/tabwriter"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/daemon"
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

func runSetup(args []string) {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	yes := fs.Bool("yes", false, "apply all changes without prompts")
	dryRun := fs.Bool("dry-run", false, "print setup plan without applying")
	home := fs.String("home", "", "home directory to scan; defaults to current user home")
	binaryPath := fs.String("bin", "", "lazy-mcp-wrapper binary path; defaults to current executable")
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
		_ = subFS.Parse(subcmdArgs)
	}

	bin := *binaryPath
	if bin == "" {
		if exe, err := os.Executable(); err == nil {
			bin = exe
		}
	}
	opts := setup.Options{
		Home:       *home,
		BinaryPath: bin,
		YesAll:     *yes,
		DryRun:     *dryRun,
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
