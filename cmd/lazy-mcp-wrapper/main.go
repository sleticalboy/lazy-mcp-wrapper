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
	"syscall"

	"github.com/binlee/lazy-mcp-wrapper/internal/daemon"
	"github.com/binlee/lazy-mcp-wrapper/internal/wrapper"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
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
		}
	}
	configPath := flag.String("config", "", "path to wrapper JSON config")
	printExample := flag.Bool("print-example", false, "print example config")
	refreshCache := flag.Bool("refresh-cache", false, "refresh tools/list cache and exit")
	clearCache := flag.Bool("clear-cache", false, "clear tools/list cache and exit")
	inspect := flag.Bool("inspect", false, "print resolved config and cache status as JSON and exit")
	flag.Parse()

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

func runDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	socketPath := fs.String("socket", "", "Unix socket path")
	daemonConfigPath := fs.String("daemon-config", "", "daemon JSON config")
	configPaths := multiFlag{}
	fs.Var(&configPaths, "config", "wrapper JSON config; can be repeated")
	_ = fs.Parse(args)

	if *daemonConfigPath != "" {
		cfg, err := daemon.LoadConfig(*daemonConfigPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load daemon config %s: %v\n", *daemonConfigPath, err)
			os.Exit(2)
		}
		*socketPath = cfg.SocketPath
		configPaths = cfg.ConfigPaths
	}
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

	server, err := daemon.NewServer(*socketPath, configs, loggers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create daemon: %v\n", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
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
	_ = fs.Parse(args)

	status, err := daemon.QueryStatus(*socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lazy-mcp-wrapper status: %v\n", err)
		os.Exit(1)
	}
	data, _ := json.MarshalIndent(status, "", "  ")
	fmt.Println(string(data))
}

func runControl(args []string, control string) {
	fs := flag.NewFlagSet(control, flag.ExitOnError)
	socketPath := fs.String("socket", "", "Unix socket path")
	_ = fs.Parse(args)

	resp, err := daemon.SendControl(*socketPath, control)
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
