package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/binlee/lazy-mcp-wrapper/internal/wrapper"
)

func LoadClientWrapperConfig(path, home, name string) (wrapper.Config, error) {
	servers, err := readServersFromPath(path)
	if err != nil {
		return wrapper.Config{}, err
	}
	server, ok := findRawServer(servers, name)
	if !ok {
		return wrapper.Config{}, fmt.Errorf("MCP server %q not found in %s", name, path)
	}
	return buildWrapperConfig(home, server), nil
}

func FindClientWrapperConfig(home, name string) (wrapper.Config, string, error) {
	for _, adapter := range allAdapters(home) {
		if !adapter.Installed() {
			continue
		}
		servers, err := adapter.ReadServers()
		if err != nil {
			continue
		}
		server, ok := findRawServer(servers, name)
		if !ok {
			continue
		}
		return buildWrapperConfig(home, server), adapter.ConfigPath(), nil
	}
	return wrapper.Config{}, "", os.ErrNotExist
}

func readServersFromPath(path string) ([]RawServer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".toml":
		return parseTOMLMCPServers(data)
	case ".json":
		return parseJSONMCPServers(data)
	default:
		servers, tomlErr := parseTOMLMCPServers(data)
		if tomlErr == nil {
			return servers, nil
		}
		servers, jsonErr := parseJSONMCPServers(data)
		if jsonErr == nil {
			return servers, nil
		}
		return nil, errors.Join(tomlErr, jsonErr)
	}
}

func findRawServer(servers []RawServer, name string) (RawServer, bool) {
	want := canonicalName(name)
	for _, server := range servers {
		if canonicalName(server.Name) == want {
			return server, true
		}
	}
	return RawServer{}, false
}
