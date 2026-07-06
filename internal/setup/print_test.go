package setup

import "testing"

func TestCommandLineUsesURLForRemoteMCP(t *testing.T) {
	got := commandLine(RawServer{URL: "https://example.test/mcp"})
	if got != "https://example.test/mcp" {
		t.Fatalf("commandLine() = %q, want URL", got)
	}
}
