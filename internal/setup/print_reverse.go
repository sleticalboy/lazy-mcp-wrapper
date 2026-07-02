package setup

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

func PrintStatusReport(out io.Writer, report StatusReport) {
	daemonState := "not running"
	if report.DaemonRunning {
		daemonState = "running"
	}
	fmt.Fprintf(out, "Daemon:   %s  (%s)\n", daemonState, report.DaemonSocket)
	fmt.Fprintf(out, "Wrappers: %d        (%s)\n", report.WrapperCount, report.WrapperDir)
	fmt.Fprintln(out)

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Client\tConfig\tWrapped\tTotal\tNotes")
	for _, client := range report.Clients {
		wrapped := "-"
		total := "-"
		notes := ""
		if client.Installed {
			wrapped = fmt.Sprintf("%d", client.WrappedCount)
			total = fmt.Sprintf("%d", client.TotalCount)
			if len(client.NotWrapped) > 0 {
				notes = fmt.Sprintf("(%d not wrapped: %s)", len(client.NotWrapped), strings.Join(client.NotWrapped, ", "))
			}
			if client.ReadError != "" {
				notes = client.ReadError
			}
		} else {
			notes = "not installed"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", client.Kind, client.ConfigPath, wrapped, total, notes)
	}
	_ = tw.Flush()
}

func PrintUpdatePlan(out io.Writer, plan UpdatePlan) {
	fmt.Fprintln(out, "Wrapper config changes:")
	if len(plan.Added) == 0 && len(plan.Removed) == 0 {
		fmt.Fprintln(out, "  no changes")
	}
	for _, cfg := range plan.Added {
		fmt.Fprintf(out, "  + %-22s %-40s [new]\n", cfg.Server.Name, commandLine(cfg.Server))
	}
	for _, path := range plan.Removed {
		fmt.Fprintf(out, "  - %-22s %s\n", strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), "[removed from all clients]")
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Daemon config: %s\n", plan.DaemonConfig.ConfigPath)
	fmt.Fprintf(out, "Socket:        %s\n", plan.DaemonConfig.SocketPath)
	if len(plan.Blockers) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Blockers:")
		for _, blocker := range plan.Blockers {
			fmt.Fprintf(out, "  - %s\n", blocker)
		}
	}
}
