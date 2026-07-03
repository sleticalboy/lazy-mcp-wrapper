package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/daemon"
)

// VerifyResult holds the result of verifying one MCP server.
type VerifyResult struct {
	Name      string
	ToolCount int
	Elapsed   time.Duration
	Err       error
}

// Verify connects to the daemon and calls tools/list on every registered server.
func Verify(opts Options) []VerifyResult {
	opts = normalizeOptions(opts)
	sock := socketPath(opts.Home)

	status, err := daemon.QueryStatus(sock)
	if err != nil {
		return []VerifyResult{{Name: "(daemon)", Err: fmt.Errorf("cannot reach daemon at %s: %w", sock, err)}}
	}

	results := make([]VerifyResult, 0, len(status.Servers))
	for _, srv := range status.Servers {
		results = append(results, verifyOne(sock, srv.Name))
	}
	return results
}

func verifyOne(socketPath, name string) VerifyResult {
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n"

	pr, pw := io.Pipe()
	var buf bytes.Buffer

	errc := make(chan error, 1)
	go func() {
		errc <- daemon.RunClient(socketPath, name, pr, &buf)
	}()

	start := time.Now()
	_, _ = io.WriteString(pw, req)
	_ = pw.Close()

	if err := <-errc; err != nil {
		return VerifyResult{Name: name, Elapsed: time.Since(start), Err: err}
	}
	elapsed := time.Since(start)

	// Parse tools/list response — find the first JSON line with "result"
	toolCount := -1
	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var resp struct {
			Result struct {
				Tools []json.RawMessage `json:"tools"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		if resp.Error != nil {
			return VerifyResult{Name: name, Elapsed: elapsed, Err: fmt.Errorf("tools/list error: %s", resp.Error.Message)}
		}
		toolCount = len(resp.Result.Tools)
		break
	}

	if toolCount < 0 {
		return VerifyResult{Name: name, Elapsed: elapsed, Err: fmt.Errorf("no valid response")}
	}
	return VerifyResult{Name: name, ToolCount: toolCount, Elapsed: elapsed}
}
