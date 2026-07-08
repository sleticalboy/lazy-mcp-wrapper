package setup

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

func resolveServerPlaceholders(server RawServer) (RawServer, error) {
	var err error
	if server.Command, err = resolvePlaceholders(server.Command); err != nil {
		return server, fmt.Errorf("%s command: %w", server.Name, err)
	}
	if server.URL, err = resolvePlaceholders(server.URL); err != nil {
		return server, fmt.Errorf("%s url: %w", server.Name, err)
	}
	for i, arg := range server.Args {
		if server.Args[i], err = resolvePlaceholders(arg); err != nil {
			return server, fmt.Errorf("%s args[%d]: %w", server.Name, i, err)
		}
	}
	for key, value := range server.Env {
		if server.Env[key], err = resolvePlaceholders(value); err != nil {
			return server, fmt.Errorf("%s env.%s: %w", server.Name, key, err)
		}
	}
	for key, value := range server.Headers {
		if server.Headers[key], err = resolvePlaceholders(value); err != nil {
			return server, fmt.Errorf("%s headers.%s: %w", server.Name, key, err)
		}
	}
	return server, nil
}

func resolvePlaceholders(value string) (string, error) {
	if !strings.Contains(value, "${") {
		return value, nil
	}
	var out strings.Builder
	for i := 0; i < len(value); {
		start := strings.Index(value[i:], "${")
		if start < 0 {
			out.WriteString(value[i:])
			break
		}
		start += i
		out.WriteString(value[i:start])
		end := strings.IndexByte(value[start+2:], '}')
		if end < 0 {
			return "", fmt.Errorf("unterminated placeholder in %q", value)
		}
		end += start + 2
		name := value[start+2 : end]
		resolved, err := resolvePlaceholder(name)
		if err != nil {
			return "", err
		}
		out.WriteString(resolved)
		i = end + 1
	}
	return out.String(), nil
}

func resolvePlaceholder(name string) (string, error) {
	switch {
	case strings.HasPrefix(name, "env:"):
		key := strings.TrimPrefix(name, "env:")
		if key == "" {
			return "", fmt.Errorf("empty env placeholder")
		}
		if value, ok := os.LookupEnv(key); ok {
			return value, nil
		}
		return "", fmt.Errorf("environment variable %s is not set", key)
	case strings.HasPrefix(name, "cmd:"):
		return "", fmt.Errorf("command placeholder ${%s} is not supported during automatic setup", name)
	case name == "userHome":
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", fmt.Errorf("cannot resolve userHome")
		}
		return home, nil
	case name == "/" || name == "pathSeparator":
		if runtime.GOOS == "windows" {
			return `\`, nil
		}
		return "/", nil
	case name == "workspaceFolder":
		return "", fmt.Errorf("workspaceFolder is not available during global setup scan")
	default:
		if isSimpleEnvName(name) {
			if value, ok := os.LookupEnv(name); ok {
				return value, nil
			}
			return "", fmt.Errorf("environment variable %s is not set", name)
		}
		return "", fmt.Errorf("unsupported placeholder ${%s}", name)
	}
}

func isSimpleEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}
