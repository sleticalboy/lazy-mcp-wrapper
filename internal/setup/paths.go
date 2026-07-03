package setup

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var currentGOOS = runtime.GOOS

func lazyMCPDir(home string) string {
	if currentGOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, "lazy-mcp-wrapper")
		}
	}
	return filepath.Join(home, ".lazy-mcp-wrapper")
}

func wrappersDir(home string) string {
	return filepath.Join(lazyMCPDir(home), "wrappers")
}

func socketPath(home string) string {
	return filepath.Join(lazyMCPDir(home), "lazy-mcpd.sock")
}

func daemonConfigPath(home string) string {
	return filepath.Join(lazyMCPDir(home), "config.json")
}

func logDir(home string) string {
	switch currentGOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Logs", "lazy-mcp-wrapper")
	case "windows":
		return filepath.Join(lazyMCPDir(home), "logs")
	default:
		return filepath.Join(lazyMCPDir(home), "logs")
	}
}

func launchAgentPath(home string) string {
	switch currentGOOS {
	case "windows":
		return ""
	case "darwin":
		return filepath.Join(home, "Library", "LaunchAgents", defaultLabel+".plist")
	case "linux":
		return filepath.Join(home, ".config", "systemd", "user", defaultLabel+".service")
	default:
		return ""
	}
}

// enrichPATH 在系统 PATH 基础上追加常见工具链路径，确保 daemon 能找到
// 通过 nvm/pyenv/mise/homebrew 等安装的工具。
func enrichPATH(base string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return base
	}
	candidates := []string{
		// Node（nvm）
		filepath.Join(home, ".nvm", "versions", "node"),
		// Python（pyenv）
		filepath.Join(home, ".pyenv", "shims"),
		filepath.Join(home, ".pyenv", "bin"),
		// mise / asdf
		filepath.Join(home, ".local", "share", "mise", "shims"),
		filepath.Join(home, ".asdf", "shims"),
		// Homebrew（Apple Silicon）
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		// Homebrew（Intel）
		"/usr/local/bin",
		// Cargo
		filepath.Join(home, ".cargo", "bin"),
		// Go
		filepath.Join(home, "go", "bin"),
		// user local bin
		filepath.Join(home, ".local", "bin"),
	}

	existing := make(map[string]bool)
	for _, p := range filepath.SplitList(base) {
		existing[p] = true
	}

	extra := make([]string, 0, len(candidates))
	for _, c := range candidates {
		// nvm 目录本身不可执行，展开一层找实际 bin 目录
		if filepath.Base(c) == "node" {
			entries, err := os.ReadDir(c)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() {
					bin := filepath.Join(c, e.Name(), "bin")
					if !existing[bin] {
						extra = append(extra, bin)
						existing[bin] = true
					}
				}
			}
			continue
		}
		if !existing[c] {
			extra = append(extra, c)
			existing[c] = true
		}
	}

	if len(extra) == 0 {
		return base
	}
	return base + string(os.PathListSeparator) + strings.Join(extra, string(os.PathListSeparator))
}
