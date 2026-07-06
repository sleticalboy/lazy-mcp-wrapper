package wrapper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context7.json")
	if err := os.WriteFile(path, []byte(`{"name":"context7","command":"npx"}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.IdleTimeout.Duration != 30*time.Second {
		t.Fatalf("IdleTimeout = %s", cfg.IdleTimeout.Duration)
	}
	if cfg.StartupTimeout.Duration != 20*time.Second {
		t.Fatalf("StartupTimeout = %s", cfg.StartupTimeout.Duration)
	}
	if cfg.CallTimeout.Duration != 2*time.Minute {
		t.Fatalf("CallTimeout = %s", cfg.CallTimeout.Duration)
	}
	if cfg.Sharing != "shared" {
		t.Fatalf("Sharing = %s, want shared", cfg.Sharing)
	}
}

func TestLoadConfigSharing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "playwright.json")
	if err := os.WriteFile(path, []byte(`{"name":"playwright","sharing":"session","command":"npx"}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Sharing != "session" {
		t.Fatalf("Sharing = %s, want session", cfg.Sharing)
	}
}

func TestLoadConfigInvalidSharing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"name":"bad","sharing":"global","command":"npx"}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := LoadConfig(path); err == nil {
		t.Fatalf("LoadConfig() error = nil, want invalid sharing")
	}
}

func TestConfigHTTPDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "http.json")
	if err := os.WriteFile(path, []byte(`{"name":"remote","url":"https://example.test/mcp","headers":{"Authorization":"Bearer ${TEST_HTTP_TOKEN}"}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("TEST_HTTP_TOKEN", "secret")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.HTTPProtocol() != "streamable-http" {
		t.Fatalf("HTTPProtocol() = %q", cfg.HTTPProtocol())
	}
	if cfg.Headers["Authorization"] != "Bearer secret" {
		t.Fatalf("header expansion = %q", cfg.Headers["Authorization"])
	}
}

func TestConfigOAuthDefaultsToSessionAndExpandsFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth.json")
	if err := os.WriteFile(path, []byte(`{
  "name": "figma",
  "url": "https://mcp.figma.com/mcp",
  "auth": "oauth",
  "oauth_client_id": "${TEST_OAUTH_CLIENT_ID}",
  "oauth_resource": "${TEST_OAUTH_RESOURCE}",
  "oauth_scopes": ["${TEST_OAUTH_SCOPE}"]
}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("TEST_OAUTH_CLIENT_ID", "client-id")
	t.Setenv("TEST_OAUTH_RESOURCE", "https://mcp.figma.com")
	t.Setenv("TEST_OAUTH_SCOPE", "tools")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if !cfg.RequiresOAuth() {
		t.Fatal("RequiresOAuth() = false")
	}
	if !cfg.UseSDKHTTPBackend() {
		t.Fatal("UseSDKHTTPBackend() = false")
	}
	if cfg.Sharing != "session" {
		t.Fatalf("Sharing = %s, want session", cfg.Sharing)
	}
	if cfg.OAuthClientID != "client-id" || cfg.OAuthResource != "https://mcp.figma.com" {
		t.Fatalf("oauth fields = client_id:%q resource:%q", cfg.OAuthClientID, cfg.OAuthResource)
	}
	if len(cfg.OAuthScopes) != 1 || cfg.OAuthScopes[0] != "tools" {
		t.Fatalf("oauth scopes = %#v", cfg.OAuthScopes)
	}
}

func TestConfigRejectsOAuthShared(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth.json")
	if err := os.WriteFile(path, []byte(`{"name":"remote","url":"https://example.test/mcp","auth":"oauth","sharing":"shared"}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "session sharing") {
		t.Fatalf("LoadConfig() error = %v, want session sharing", err)
	}
}

func TestConfigRejectsOAuthNativeHTTPBackend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth.json")
	if err := os.WriteFile(path, []byte(`{"name":"remote","url":"https://example.test/mcp","auth":"oauth","http_backend":"native"}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "sdk http_backend") {
		t.Fatalf("LoadConfig() error = %v, want sdk http_backend", err)
	}
}

func TestConfigRejectsInvalidAuth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"name":"bad","url":"https://example.test/mcp","auth":"magic"}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "auth") {
		t.Fatalf("LoadConfig() error = %v, want auth error", err)
	}
}

func TestConfigRejectsSSEProtocol(t *testing.T) {
	path := filepath.Join(t.TempDir(), "http.json")
	if err := os.WriteFile(path, []byte(`{"name":"remote","url":"https://example.test/mcp","protocol":"sse"}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("LoadConfig() expected error for sse protocol, got nil")
	}
	if !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("LoadConfig() error = %v, want 'no longer supported'", err)
	}
}

func TestConfigRejectsCommandAndURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"name":"bad","command":"npx","url":"https://example.test/mcp"}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("LoadConfig() error = %v, want mutually exclusive", err)
	}
}

func TestConfigRejectsMissingCommandAndURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"name":"bad"}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "command or url") {
		t.Fatalf("LoadConfig() error = %v, want command or url", err)
	}
}

func TestConfigRejectsInvalidHTTPProtocol(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"name":"bad","url":"https://example.test/mcp","protocol":"websocket"}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "protocol") {
		t.Fatalf("LoadConfig() error = %v, want protocol error", err)
	}
}
