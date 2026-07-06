package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseAuthLoginFlags(t *testing.T) {
	flags, rest, err := parseAuthLoginFlags([]string{
		"figma",
		"--home", "/tmp/home",
		"--url=https://mcp.figma.com/mcp",
		"--client-id", "client-id",
		"--token-url", "https://auth.example.test/token",
		"--scope", "tools",
		"--callback-port=3456",
		"--timeout", "30s",
		"--no-open",
	})
	if err != nil {
		t.Fatalf("parseAuthLoginFlags() error = %v", err)
	}
	if len(rest) != 1 || rest[0] != "figma" {
		t.Fatalf("rest = %#v", rest)
	}
	if flags.home != "/tmp/home" ||
		flags.url != "https://mcp.figma.com/mcp" ||
		flags.clientID != "client-id" ||
		flags.tokenURL != "https://auth.example.test/token" ||
		flags.callbackPort != 3456 ||
		flags.timeout != 30*time.Second ||
		flags.openBrowser {
		t.Fatalf("flags = %#v", flags)
	}
	if len(flags.scopes) != 1 || flags.scopes[0] != "tools" {
		t.Fatalf("scopes = %#v", flags.scopes)
	}
}

func TestParseAuthLoginFlagsRejectsBadTimeout(t *testing.T) {
	if _, _, err := parseAuthLoginFlags([]string{"figma", "--timeout", "soon"}); err == nil {
		t.Fatal("parseAuthLoginFlags() error = nil, want bad timeout error")
	}
}

func TestBuildOAuthLoginOptionsReadsClientConfigWhenWrapperConfigMissing(t *testing.T) {
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte(`[mcp_servers.figma]
url = "https://mcp.figma.com/mcp"
auth = "oauth"
oauth_resource = "https://mcp.figma.com"
scopes = ["tools:read"]

[mcp_servers.figma.oauth]
client_id = "figma-client"
`), 0644); err != nil {
		t.Fatal(err)
	}

	opts, err := buildOAuthLoginOptions(home, "figma", authLoginFlags{openBrowser: true})
	if err != nil {
		t.Fatalf("buildOAuthLoginOptions() error = %v", err)
	}
	if opts.ServerURL != "https://mcp.figma.com/mcp" || opts.ClientID != "figma-client" || opts.Resource != "https://mcp.figma.com" {
		t.Fatalf("login options = %#v", opts)
	}
	if len(opts.Scopes) != 1 || opts.Scopes[0] != "tools:read" {
		t.Fatalf("scopes = %#v", opts.Scopes)
	}
}

func TestBuildOAuthLoginOptionsReadsExplicitClientConfig(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.toml")
	if err := os.WriteFile(configPath, []byte(`[mcp_servers.figma]
url = "https://mcp.figma.com/mcp"
auth = "oauth"

[mcp_servers.figma.oauth]
client_id = "figma-client"
`), 0644); err != nil {
		t.Fatal(err)
	}

	opts, err := buildOAuthLoginOptions(home, "figma", authLoginFlags{
		configPath:  configPath,
		openBrowser: true,
	})
	if err != nil {
		t.Fatalf("buildOAuthLoginOptions() error = %v", err)
	}
	if opts.ServerURL != "https://mcp.figma.com/mcp" || opts.ClientID != "figma-client" {
		t.Fatalf("login options = %#v", opts)
	}
}
