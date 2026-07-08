package setup

import (
	"bytes"
	"strings"
	"testing"
)

func TestCommandLineUsesURLForRemoteMCP(t *testing.T) {
	got := commandLine(RawServer{URL: "https://example.test/mcp"})
	if got != "https://example.test/mcp" {
		t.Fatalf("commandLine() = %q, want URL", got)
	}
}

func TestPrintPlanIncludesDecisionReasons(t *testing.T) {
	var out bytes.Buffer
	PrintPlan(&out, Plan{
		Decisions: []ServerDecision{
			{
				ClientKind: "codex",
				Name:       "figma",
				Action:     "skip",
				Reason:     "skipped-figma-dynamic-client-rejected",
				Detail:     "Figma rejected dynamic OAuth client registration",
			},
		},
	})
	text := out.String()
	if !strings.Contains(text, "Plan decisions:") || !strings.Contains(text, "skipped-figma-dynamic-client-rejected") {
		t.Fatalf("PrintPlan() missing decision reason:\n%s", text)
	}
}
