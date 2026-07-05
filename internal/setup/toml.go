package setup

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var mcpSectionRE = regexp.MustCompile(`^\s*\[mcp_servers\.((?:"[^"]+"|[A-Za-z0-9_-]+))(?:\.(env|headers))?\]\s*$`)

func parseTOMLMCPServers(data []byte) ([]RawServer, error) {
	lines := strings.Split(string(data), "\n")
	var servers []RawServer
	serversByName := map[string]*RawServer{}
	order := []string{}
	var current *RawServer
	subsection := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if match := mcpSectionRE.FindStringSubmatch(trimmed); match != nil {
			name := unquoteTableName(match[1])
			current = serversByName[name]
			if current == nil {
				current = &RawServer{Name: name}
				serversByName[name] = current
				order = append(order, name)
			}
			subsection = match[2]
			if subsection == "env" && current.Env == nil {
				current.Env = map[string]string{}
			}
			if subsection == "headers" && current.Headers == nil {
				current.Headers = map[string]string{}
			}
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			current = nil
			subsection = ""
			continue
		}
		if current == nil || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(stripComment(value))
		switch subsection {
		case "env":
			if current.Env == nil {
				current.Env = map[string]string{}
			}
			current.Env[key] = parseTOMLString(value)
			continue
		case "headers":
			if current.Headers == nil {
				current.Headers = map[string]string{}
			}
			current.Headers[key] = parseTOMLString(value)
			continue
		}
		switch key {
		case "type":
			current.Type = parseTOMLString(value)
		case "command":
			current.Command = parseTOMLString(value)
		case "url":
			current.URL = parseTOMLString(value)
		case "args":
			args, err := parseTOMLStringArray(value)
			if err != nil {
				return nil, err
			}
			current.Args = args
		}
	}
	for _, name := range order {
		server := *serversByName[name]
		server.IsWrappable = isWrappable(server)
		servers = append(servers, server)
	}
	return servers, nil
}

func replaceTOMLMCPServers(data []byte, servers []RawServer) []byte {
	lines := strings.Split(string(data), "\n")
	var out []string
	inMCP := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if mcpSectionRE.MatchString(trimmed) {
			inMCP = true
			continue
		}
		if inMCP {
			if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
				inMCP = false
			} else {
				continue
			}
		}
		if !inMCP {
			out = append(out, line)
		}
	}

	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	if len(out) > 0 {
		out = append(out, "")
	}
	out = append(out, renderTOMLMCPServers(servers)...)
	return []byte(strings.Join(out, "\n") + "\n")
}

func renderTOMLMCPServers(servers []RawServer) []string {
	var lines []string
	for i, server := range servers {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, fmt.Sprintf("[mcp_servers.%s]", quoteTableName(server.Name)))
		lines = append(lines, fmt.Sprintf("type = %q", defaultType(server.Type)))
		if server.URL != "" {
			lines = append(lines, fmt.Sprintf("url = %q", server.URL))
		} else {
			lines = append(lines, fmt.Sprintf("command = %q", server.Command))
			lines = append(lines, "args = "+formatStringArray(server.Args))
		}
		if len(server.Env) > 0 {
			lines = append(lines, fmt.Sprintf("[mcp_servers.%s.env]", quoteTableName(server.Name)))
			keys := make([]string, 0, len(server.Env))
			for key := range server.Env {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				lines = append(lines, fmt.Sprintf("%s = %q", key, server.Env[key]))
			}
		}
		if len(server.Headers) > 0 {
			lines = append(lines, fmt.Sprintf("[mcp_servers.%s.headers]", quoteTableName(server.Name)))
			keys := make([]string, 0, len(server.Headers))
			for key := range server.Headers {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				lines = append(lines, fmt.Sprintf("%s = %q", key, server.Headers[key]))
			}
		}
	}
	return lines
}

func parseTOMLString(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' {
		if out, err := strconv.Unquote(value); err == nil {
			return out
		}
	}
	return strings.Trim(value, `"`)
}

func parseTOMLStringArray(value string) ([]string, error) {
	var out []string
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return nil, fmt.Errorf("parse TOML string array %q: %w", value, err)
	}
	return out, nil
}

func formatStringArray(values []string) string {
	data, _ := json.Marshal(values)
	return string(data)
}

func stripComment(value string) string {
	inString := false
	escaped := false
	for i, r := range value {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			continue
		}
		if r == '#' && !inString {
			return strings.TrimSpace(value[:i])
		}
	}
	return value
}

func quoteTableName(name string) string {
	if regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(name) {
		return name
	}
	return strconv.Quote(name)
}

func unquoteTableName(name string) string {
	if strings.HasPrefix(name, `"`) {
		value, err := strconv.Unquote(name)
		if err == nil {
			return value
		}
	}
	return name
}
