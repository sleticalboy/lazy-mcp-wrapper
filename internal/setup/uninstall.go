package setup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"
)

type UninstallPlan struct {
	LaunchAgent    LaunchAgentPlan
	ClientRestores []ClientRestorePlan
	WrapperDir     string
}

type ClientRestorePlan struct {
	Kind       string
	ConfigPath string
	BackupPath string
	FromBackup bool
	RemoveRefs bool
	Servers    []RawServer
}

func NewUninstallPlan(opts Options) (UninstallPlan, error) {
	opts = normalizeOptions(opts)
	socketPath := currentDaemonSocket(opts.Home)
	plan := UninstallPlan{
		LaunchAgent: defaultLaunchAgentPlan(opts),
		WrapperDir:  wrappersDir(opts.Home),
	}
	for _, adapter := range scanClients(opts.Home) {
		restore := ClientRestorePlan{
			Kind:       adapter.Kind(),
			ConfigPath: adapter.ConfigPath(),
		}
		if backup, ok, err := latestBackupPath(adapter.ConfigPath()); err != nil {
			return UninstallPlan{}, err
		} else if ok {
			restore.BackupPath = backup
			restore.FromBackup = true
			plan.ClientRestores = append(plan.ClientRestores, restore)
			continue
		}
		servers, err := adapter.ReadServers()
		if err != nil {
			return UninstallPlan{}, err
		}
		next := removeWrapperRefs(servers, socketPath)
		if len(next) != len(servers) {
			restore.RemoveRefs = true
			restore.Servers = next
			plan.ClientRestores = append(plan.ClientRestores, restore)
		}
	}
	return plan, nil
}

func Uninstall(opts Options) error {
	opts = normalizeOptions(opts)
	plan, err := NewUninstallPlan(opts)
	if err != nil {
		return err
	}
	PrintUninstallPlan(os.Stdout, plan)
	if opts.DryRun {
		return nil
	}
	if shouldApply(opts, "Step 1/3: Stop and remove LaunchAgent?") {
		if err := uninstallLaunchAgent(plan.LaunchAgent, opts.execFunc()); err != nil {
			return err
		}
	}
	if len(plan.ClientRestores) > 0 && shouldApply(opts, "Step 2/3: Restore client configs?") {
		adaptersByKind := map[string]ClientAdapter{}
		for _, adapter := range scanClients(opts.Home) {
			adaptersByKind[adapter.Kind()] = adapter
		}
		for _, restore := range plan.ClientRestores {
			if restore.FromBackup {
				data, err := os.ReadFile(restore.BackupPath)
				if err != nil {
					return err
				}
				if err := os.WriteFile(restore.ConfigPath, data, 0644); err != nil {
					return err
				}
				continue
			}
			adapter := adaptersByKind[restore.Kind]
			if adapter == nil {
				return fmt.Errorf("adapter not found for %s", restore.Kind)
			}
			if err := adapter.WriteServers(restore.Servers, ""); err != nil {
				return err
			}
		}
	}
	if shouldApply(opts, "Step 3/3: Delete wrapper config files?") {
		if err := os.RemoveAll(plan.WrapperDir); err != nil {
			return err
		}
	}
	return nil
}

func PrintUninstallPlan(out io.Writer, plan UninstallPlan) {
	fmt.Fprintf(out, "LaunchAgent: %s\n", plan.LaunchAgent.PlistPath)
	fmt.Fprintf(out, "Socket:      %s\n", plan.LaunchAgent.SocketPath)
	fmt.Fprintf(out, "Wrappers:    %s\n", plan.WrapperDir)
	fmt.Fprintln(out)

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Client\tConfig\tRestore")
	for _, restore := range plan.ClientRestores {
		action := "remove wrapper refs"
		if restore.FromBackup {
			action = "backup " + filepath.Base(restore.BackupPath)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", restore.Kind, restore.ConfigPath, action)
	}
	_ = tw.Flush()
}
