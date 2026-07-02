package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type jsonAdapter struct {
	kind string
	path string
}

type jsonConfig struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

type jsonServer struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
}

func newJSONAdapter(kind, path string) ClientAdapter {
	return jsonAdapter{kind: kind, path: path}
}

func (a jsonAdapter) Kind() string {
	return a.kind
}

func (a jsonAdapter) ConfigPath() string {
	return a.path
}

func (a jsonAdapter) Installed() bool {
	_, err := os.Stat(a.path)
	return err == nil
}

func (a jsonAdapter) ReadServers() ([]RawServer, error) {
	data, err := os.ReadFile(a.path)
	if err != nil {
		return nil, err
	}
	var cfg jsonConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(cfg.MCPServers))
	for name := range cfg.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	servers := make([]RawServer, 0, len(names))
	for _, name := range names {
		var server jsonServer
		if err := json.Unmarshal(cfg.MCPServers[name], &server); err != nil {
			return nil, err
		}
		raw := RawServer{
			Name:    name,
			Type:    defaultType(server.Type),
			Command: server.Command,
			Args:    server.Args,
			Env:     server.Env,
			URL:     server.URL,
			Raw:     cfg.MCPServers[name],
		}
		raw.IsWrappable = isWrappable(raw)
		servers = append(servers, raw)
	}
	return servers, nil
}

func (a jsonAdapter) WriteServers(servers []RawServer, backupPath string) error {
	content, err := renderJSONConfig(a.path, servers)
	if err != nil {
		return err
	}
	return os.WriteFile(a.path, content, 0644)
}

func renderJSONConfig(path string, servers []RawServer) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	mcpServers := map[string]json.RawMessage{}
	for _, server := range servers {
		if !server.IsWrappable && len(server.Raw) > 0 {
			mcpServers[server.Name] = server.Raw
			continue
		}
		data, err := json.Marshal(jsonServer{
			Type:    defaultType(server.Type),
			Command: server.Command,
			Args:    server.Args,
			Env:     server.Env,
			URL:     server.URL,
		})
		if err != nil {
			return nil, err
		}
		mcpServers[server.Name] = data
	}
	encodedServers, err := json.Marshal(mcpServers)
	if err != nil {
		return nil, err
	}
	doc["mcpServers"] = encodedServers
	data, err = json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func renderAdapterContent(adapter ClientAdapter, servers []RawServer) ([]byte, error) {
	switch adapter.Kind() {
	case "codex":
		data, err := os.ReadFile(adapter.ConfigPath())
		if err != nil {
			return nil, err
		}
		return replaceTOMLMCPServers(data, servers), nil
	default:
		return renderJSONConfig(adapter.ConfigPath(), servers)
	}
}

func defaultType(value string) string {
	if value == "" {
		return "stdio"
	}
	return value
}

func isWrappable(server RawServer) bool {
	if strings.EqualFold(server.Name, "node_repl") || strings.Contains(strings.ToLower(filepath.Base(server.Command)), "node_repl") {
		return false
	}
	if defaultType(server.Type) != "stdio" {
		return false
	}
	if server.Command == "" {
		return false
	}
	if filepath.Base(server.Command) == "lazy-mcp-wrapper" {
		return false
	}
	for _, arg := range server.Args {
		if arg == "client" || arg == "--config" {
			return false
		}
	}
	return true
}
