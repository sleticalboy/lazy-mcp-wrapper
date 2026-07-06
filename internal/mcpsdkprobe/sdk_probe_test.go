package mcpsdkprobe

import (
	"testing"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestOfficialGoSDKAPISurface(t *testing.T) {
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "lazy-mcp-wrapper-probe",
		Version: "0.0.0",
	}, nil)
	if client == nil {
		t.Fatal("mcp.NewClient returned nil")
	}

	transport := &mcp.StreamableClientTransport{
		Endpoint:             "https://example.test/mcp",
		DisableStandaloneSSE: true,
	}
	if transport.Endpoint == "" {
		t.Fatal("streamable client transport endpoint is empty")
	}

	var _ mcpauth.OAuthHandler = (*mcpauth.AuthorizationCodeHandler)(nil)
}
