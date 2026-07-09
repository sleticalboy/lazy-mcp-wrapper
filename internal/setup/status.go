package setup

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/binlee/lazy-mcp-wrapper/internal/wrapper"
)

type StatusReport struct {
	WrapperDir    string
	WrapperCount  int
	DaemonRunning bool
	DaemonSocket  string
	PanicLog      string // non-empty if a panic.log exists
	Clients       []ClientStatus
}

type ClientStatus struct {
	Kind         string
	ConfigPath   string
	Installed    bool
	WrappedCount int
	TotalCount   int
	NotWrapped   []string
	Issues       []string
	ReadError    string
}

func Status(opts Options) StatusReport {
	opts = normalizeOptions(opts)
	wrapperDir := wrappersDir(opts.Home)
	wrappers, _ := listWrapperConfigs(wrapperDir)
	socketPath := currentDaemonSocket(opts.Home)

	report := StatusReport{
		WrapperDir:    wrapperDir,
		WrapperCount:  len(wrappers),
		DaemonRunning: daemonConnectable(socketPath),
		DaemonSocket:  socketPath,
	}
	// 检查 panic.log
	panicLog := filepath.Join(lazyMCPDir(opts.Home), "panic.log")
	if _, err := os.Stat(panicLog); err == nil {
		report.PanicLog = panicLog
	}
	daemonWrappers := daemonWrapperNames(opts.Home)
	for _, adapter := range allAdapters(opts.Home) {
		status := ClientStatus{
			Kind:       adapter.Kind(),
			ConfigPath: adapter.ConfigPath(),
			Installed:  adapter.Installed(),
		}
		if status.Installed {
			servers, err := adapter.ReadServers()
			if err != nil {
				status.ReadError = err.Error()
			} else {
				status.TotalCount = len(servers)
				for _, server := range servers {
					if isWrapperRef(server, socketPath) {
						status.WrappedCount++
						name := strings.ToLower(canonicalName(server.Name))
						if name != "" && !daemonWrappers[name] {
							status.Issues = append(status.Issues, "missing daemon wrapper: "+canonicalName(server.Name))
						}
					} else {
						status.NotWrapped = append(status.NotWrapped, server.Name)
					}
				}
			}
		}
		report.Clients = append(report.Clients, status)
	}
	return report
}

func daemonWrapperNames(home string) map[string]bool {
	names := map[string]bool{}
	for _, path := range existingDaemonConfigPaths(daemonConfigPath(home)) {
		cfg, err := wrapper.LoadConfig(path)
		if err != nil {
			continue
		}
		name := strings.ToLower(canonicalName(cfg.Name))
		if name != "" {
			names[name] = true
		}
	}
	return names
}
