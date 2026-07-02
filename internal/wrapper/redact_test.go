package wrapper

import "testing"

func TestRedactArgs(t *testing.T) {
	got := redactArgs([]string{"--token=abc", "--api-key=def", "--url=https://x"})
	want := []string{"--token=***REDACTED***", "--api-key=***REDACTED***", "--url=https://x"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRedactEnv(t *testing.T) {
	got := redactEnv(map[string]string{"MASTERGO_TOKEN": "abc", "NPM_CONFIG_REGISTRY": "https://registry.npmjs.org/"})
	if got["MASTERGO_TOKEN"] != "***REDACTED***" {
		t.Fatalf("got MASTERGO_TOKEN = %q", got["MASTERGO_TOKEN"])
	}
	if got["NPM_CONFIG_REGISTRY"] == "***REDACTED***" {
		t.Fatal("non-sensitive env should not be redacted")
	}
}
