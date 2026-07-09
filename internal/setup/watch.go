package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type WatchOptions struct {
	Interval time.Duration
	Apply    bool
	Out      io.Writer
}

type watchFileState struct {
	Exists bool
	IsDir  bool
	Hash   string
}

type watchSnapshot map[string]watchFileState

func Watch(ctx context.Context, opts Options, watchOpts WatchOptions) error {
	opts = normalizeOptions(opts)
	interval := watchOpts.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	out := watchOpts.Out
	if out == nil {
		out = os.Stdout
	}

	previous, err := newWatchSnapshot(opts)
	if err != nil {
		return err
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()
	if err := syncFSNotifyWatches(watcher, previous); err != nil {
		return err
	}

	fmt.Fprintf(out, "Watching MCP config files with fsnotify (debounce %s). Press Ctrl+C to stop.\n", interval)
	printWatchedPaths(out, previous)

	var debounce <-chan time.Time
	var timer *time.Timer
	trigger := func() {
		if timer == nil {
			timer = time.NewTimer(interval)
			debounce = timer.C
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(interval)
		debounce = timer.C
	}
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename|fsnotify.Chmod) != 0 {
				trigger()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			fmt.Fprintf(out, "\nwatch event error: %v\n", err)
		case <-debounce:
			debounce = nil
			timer = nil
			next, err := newWatchSnapshot(opts)
			if err != nil {
				fmt.Fprintf(out, "\nwatch scan failed: %v\n", err)
				continue
			}
			if err := syncFSNotifyWatches(watcher, next); err != nil {
				fmt.Fprintf(out, "\nwatch update failed: %v\n", err)
			}
			changed := changedWatchPaths(previous, next)
			if len(changed) == 0 {
				previous = next
				continue
			}
			previous = next
			fmt.Fprintf(out, "\nConfig change detected at %s:\n", time.Now().Format("2006-01-02 15:04:05"))
			for _, path := range changed {
				fmt.Fprintf(out, "  - %s\n", path)
			}
			fmt.Fprintln(out)

			if watchOpts.Apply {
				applyOpts := opts
				applyOpts.DryRun = false
				applyOpts.YesAll = true
				if err := Update(applyOpts); err != nil {
					fmt.Fprintf(out, "setup update failed: %v\n", err)
				}
				continue
			}
			plan, err := NewUpdatePlan(opts)
			if err != nil {
				fmt.Fprintf(out, "setup update dry-run failed: %v\n", err)
				continue
			}
			PrintUpdatePlan(out, plan)
		}
	}
}

func newWatchSnapshot(opts Options) (watchSnapshot, error) {
	opts = normalizeOptions(opts)
	snapshot := watchSnapshot{}
	for _, path := range watchedConfigPaths(opts) {
		if err := snapshot.addPath(path); err != nil {
			return nil, err
		}
	}
	wrapperDir := wrappersDir(opts.Home)
	if err := snapshot.addPath(wrapperDir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(wrapperDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if err := snapshot.addPath(filepath.Join(wrapperDir, entry.Name())); err != nil {
			return nil, err
		}
	}
	if err := snapshot.addPath(daemonConfigPath(opts.Home)); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func syncFSNotifyWatches(watcher *fsnotify.Watcher, snapshot watchSnapshot) error {
	want := fsnotifyWatchPaths(snapshot)
	current := map[string]bool{}
	for _, path := range watcher.WatchList() {
		current[path] = true
	}
	for path := range current {
		if !want[path] {
			if err := watcher.Remove(path); err != nil {
				return err
			}
		}
	}
	for path := range want {
		if current[path] {
			continue
		}
		if err := watcher.Add(path); err != nil {
			return err
		}
	}
	return nil
}

func fsnotifyWatchPaths(snapshot watchSnapshot) map[string]bool {
	paths := map[string]bool{}
	for path, state := range snapshot {
		if state.Exists && state.IsDir {
			paths[path] = true
			continue
		}
		if dir := nearestExistingDir(filepath.Dir(path)); dir != "" {
			paths[dir] = true
		}
	}
	return paths
}

func nearestExistingDir(path string) string {
	for path != "" && path != "." {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			return path
		}
		next := filepath.Dir(path)
		if next == path {
			break
		}
		path = next
	}
	return ""
}

func watchedFSNotifyPaths(watcher *fsnotify.Watcher) []string {
	paths := watcher.WatchList()
	sort.Strings(paths)
	return paths
}

func watchedConfigPaths(opts Options) []string {
	var adapters []ClientAdapter
	if len(opts.ConfigPaths) > 0 {
		adapters = make([]ClientAdapter, 0, len(opts.ConfigPaths))
		for _, path := range opts.ConfigPaths {
			adapters = append(adapters, newExplicitConfigAdapter(path))
		}
	} else {
		adapters = allAdapters(opts.Home)
	}
	paths := make([]string, 0, len(adapters))
	seen := map[string]bool{}
	for _, adapter := range adapters {
		path := adapter.ConfigPath()
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func (s watchSnapshot) addPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			s[path] = watchFileState{}
			return nil
		}
		return err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			names = append(names, entry.Name())
		}
		sort.Strings(names)
		s[path] = watchFileState{Exists: true, IsDir: true, Hash: hashString(strings.Join(names, "\n"))}
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s[path] = watchFileState{Exists: true, Hash: hashBytes(data)}
	return nil
}

func changedWatchPaths(previous, next watchSnapshot) []string {
	seen := map[string]bool{}
	for path := range previous {
		seen[path] = true
	}
	for path := range next {
		seen[path] = true
	}
	changed := make([]string, 0)
	for path := range seen {
		if previous[path] != next[path] {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed
}

func printWatchedPaths(out io.Writer, snapshot watchSnapshot) {
	paths := make([]string, 0, len(snapshot))
	for path := range snapshot {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	fmt.Fprintln(out, "Watched paths:")
	for _, path := range paths {
		fmt.Fprintf(out, "  - %s\n", path)
	}
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hashString(value string) string {
	return hashBytes([]byte(value))
}
