package setup

import "testing"

func TestParseJSONMCPServersOAuthFields(t *testing.T) {
	servers, err := parseJSONMCPServers([]byte(`{
  "mcpServers": {
    "figma": {
      "type": "streamable-http",
      "url": "https://mcp.figma.com/mcp",
      "auth": "oauth",
      "oauth_resource": "https://mcp.figma.com",
      "scopes": ["tools:read"],
      "oauth": {
        "client_id": "figma-client"
      }
    }
  }
}`))
	if err != nil {
		t.Fatalf("parseJSONMCPServers() error = %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("servers = %#v", servers)
	}
	server := servers[0]
	if server.OAuthClientID != "figma-client" || server.OAuthResource != "https://mcp.figma.com" {
		t.Fatalf("oauth fields = %#v", server)
	}
	if len(server.OAuthScopes) != 1 || server.OAuthScopes[0] != "tools:read" {
		t.Fatalf("oauth scopes = %#v", server.OAuthScopes)
	}
}
