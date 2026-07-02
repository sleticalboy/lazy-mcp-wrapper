package setup

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

func PrintPlan(out io.Writer, plan Plan) {
	fmt.Fprintln(out, "Scanning AI clients...")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Found %d clients:\n", len(plan.DetectedClients))
	for _, client := range plan.DetectedClients {
		names := serverNames(client.Servers)
		fmt.Fprintf(out, "  %-14s %-48s (%d servers: %s)\n", client.Kind, client.ConfigPath, len(client.Servers), strings.Join(names, ", "))
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Servers to wrap (%d unique):\n", len(plan.WrapperConfigs))
	for _, cfg := range plan.WrapperConfigs {
		fmt.Fprintf(out, "  %-22s %-40s [%s]\n", cfg.Server.Name, commandLine(cfg.Server), cfg.Content.Sharing)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Daemon config: %s\n", plan.DaemonConfig.ConfigPath)
	fmt.Fprintf(out, "Socket:        %s\n", plan.DaemonConfig.SocketPath)
	fmt.Fprintf(out, "LaunchAgent:   %s\n", plan.LaunchAgent.PlistPath)
	fmt.Fprintln(out)
	if len(plan.Blockers) > 0 {
		fmt.Fprintln(out, "Blockers:")
		for _, blocker := range plan.Blockers {
			fmt.Fprintf(out, "  - %s\n", blocker)
		}
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "Verify with: %s status --socket %s --format table\n", filepath.Base(plan.LaunchAgent.BinaryPath), plan.DaemonConfig.SocketPath)
}

func serverNames(servers []RawServer) []string {
	names := make([]string, 0, len(servers))
	for _, server := range servers {
		names = append(names, server.Name)
	}
	return names
}

func commandLine(server RawServer) string {
	line := strings.TrimSpace(server.Command + " " + strings.Join(server.Args, " "))
	if len(line) > 40 {
		return line[:37] + "..."
	}
	return line
}
