package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/binlee/lazy-mcp-wrapper/internal/daemon"
)

type UpdatePlan struct {
	ExistingWrappers []existingWrapperConfig
	Added            []WrapperConfigPlan
	Removed          []string
	AddedWrappers    []WrapperConfigPlan
	RemovedWrappers  []existingWrapperConfig
	DaemonConfig     DaemonConfigPlan
	Blockers         []string
}

func NewUpdatePlan(opts Options) (UpdatePlan, error) {
	opts = normalizeOptions(opts)
	wrapperDir := wrappersDir(opts.Home)
	existing, err := listWrapperConfigs(wrapperDir)
	if err != nil {
		return UpdatePlan{}, err
	}
	existingByName := map[string]existingWrapperConfig{}
	for _, cfg := range existing {
		existingByName[strings.ToLower(canonicalName(cfg.Name))] = cfg
	}

	current := map[string]RawServer{}
	present := map[string]bool{}
	socketPath := currentDaemonSocket(opts.Home)
	for _, adapter := range scanClients(opts.Home) {
		servers, err := adapter.ReadServers()
		if err != nil {
			return UpdatePlan{}, err
		}
		for _, server := range servers {
			name := canonicalName(server.Name)
			if isWrapperRef(server, socketPath) {
				if name != "" {
					present[strings.ToLower(name)] = true
				}
				continue
			}
			if isHTTPWrapperRef(server) {
				if name != "" {
					present[strings.ToLower(name)] = true
				}
				continue
			}
			if !server.IsWrappable {
				continue
			}
			server.Name = name
			present[strings.ToLower(name)] = true
			key := strings.ToLower(server.Name)
			if _, exists := current[key]; !exists {
				current[key] = server
			}
		}
	}

	var plan UpdatePlan
	plan.ExistingWrappers = existing
	currentKeys := make([]string, 0, len(current))
	for key := range current {
		currentKeys = append(currentKeys, key)
	}
	sort.Strings(currentKeys)
	for _, key := range currentKeys {
		if _, exists := existingByName[key]; exists {
			continue
		}
		server := current[key]
		plan.AddedWrappers = append(plan.AddedWrappers, WrapperConfigPlan{
			Server:     server,
			ConfigPath: filepath.Join(wrapperDir, safeName(server.Name)+".json"),
			Content:    buildWrapperConfig(opts.Home, server),
		})
	}
	for _, cfg := range existing {
		if !present[strings.ToLower(canonicalName(cfg.Name))] {
			plan.RemovedWrappers = append(plan.RemovedWrappers, cfg)
		}
	}
	plan.Added = plan.AddedWrappers
	for _, cfg := range plan.RemovedWrappers {
		plan.Removed = append(plan.Removed, cfg.Path)
	}

	nextPaths := make([]string, 0, len(existing)+len(plan.AddedWrappers))
	removed := map[string]bool{}
	for _, cfg := range plan.RemovedWrappers {
		removed[cfg.Path] = true
	}
	for _, cfg := range existing {
		if !removed[cfg.Path] {
			nextPaths = append(nextPaths, cfg.Path)
		}
	}
	for _, cfg := range plan.AddedWrappers {
		nextPaths = append(nextPaths, cfg.ConfigPath)
	}
	sort.Strings(nextPaths)
	if len(nextPaths) == 0 {
		plan.Blockers = append(plan.Blockers, "no wrapper configs remain")
	}
	daemonPlan, err := defaultDaemonConfigPlan(opts.Home, nextPaths)
	if err != nil {
		return UpdatePlan{}, err
	}
	plan.DaemonConfig = daemonPlan
	return plan, nil
}

func Update(opts Options) error {
	opts = normalizeOptions(opts)
	plan, err := NewUpdatePlan(opts)
	if err != nil {
		return err
	}
	PrintUpdatePlan(os.Stdout, plan)
	if opts.DryRun {
		return nil
	}
	if len(plan.Blockers) > 0 {
		return fmt.Errorf("setup update has blockers: %s", strings.Join(plan.Blockers, "; "))
	}
	if (len(plan.AddedWrappers) > 0 || len(plan.RemovedWrappers) > 0) && shouldApply(opts, "Step 1/2: Apply wrapper config changes?") {
		if err := writeWrapperConfigs(plan.AddedWrappers); err != nil {
			return err
		}
		for _, cfg := range plan.RemovedWrappers {
			if err := os.Remove(cfg.Path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	if shouldApply(opts, "Step 2/2: Update daemon config and reload?") {
		if err := writeDaemonConfig(plan.DaemonConfig); err != nil {
			return err
		}
		if daemonConnectable(plan.DaemonConfig.SocketPath) {
			resp, err := daemon.SendControl(plan.DaemonConfig.SocketPath, "reload", daemon.ControlOptions{Graceful: true})
			if err != nil {
				return err
			}
			if !resp.OK {
				return fmt.Errorf("daemon reload failed: %s", resp.Error)
			}
		}
	}
	return nil
}
