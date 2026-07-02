package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/binlee/lazy-mcp-wrapper/internal/wrapper"
)

func main() {
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

const exampleConfig = `{
  "name": "context7",
  "command": "npx",
  "args": ["-y", "@upstash/context7-mcp"],
  "idle_timeout": "30s",
  "startup_timeout": "20s",
  "call_timeout": "120s",
  "log_file": "/tmp/lazy-mcp-wrapper-context7.log"
}`
