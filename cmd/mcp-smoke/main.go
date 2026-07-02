package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/jsonrpc"
)

func main() {
	callTool := flag.String("call-tool", "", "optional tool name to call after tools/list")
	callArgs := flag.String("call-args", "{}", "JSON object passed as tool arguments")
	allowToolError := flag.Bool("allow-tool-error", false, "allow a tool result with isError=true")
	socketPath := flag.String("socket", "", "daemon Unix socket path; when set, smoke runs wrapper client mode")
	name := flag.String("name", "", "daemon MCP name; required with --socket")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: mcp-smoke [--call-tool name --call-args json] [--socket path --name mcp] <wrapper> [config]")
		os.Exit(2)
	}
	if *socketPath == "" && flag.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: mcp-smoke [--call-tool name --call-args json] <wrapper> <config>")
		os.Exit(2)
	}
	if *socketPath != "" && *name == "" {
		fmt.Fprintln(os.Stderr, "mcp-smoke: --name is required with --socket")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmdArgs := []string{"--config", flag.Arg(1)}
	if *socketPath != "" {
		cmdArgs = []string{"client", "--socket", *socketPath, "--name", *name}
	}
	cmd := exec.CommandContext(ctx, flag.Arg(0), cmdArgs...)
	stdin, err := cmd.StdinPipe()
	must(err)
	stdout, err := cmd.StdoutPipe()
	must(err)
	cmd.Stderr = os.Stderr
	must(cmd.Start())
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	reader := jsonrpc.NewReader(stdout)
	writer := jsonrpc.NewWriter(stdin)

	must(writer.Write(jsonrpc.Message{JSONRPC: "2.0", ID: raw(1), Method: "initialize", Params: raw(map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "mcp-smoke", "version": "0.1.0"},
	})}))
	initResp, err := reader.Read()
	must(err)
	if initResp.Error != nil {
		panic(initResp.Error.Message)
	}

	must(writer.Write(jsonrpc.Message{JSONRPC: "2.0", Method: "notifications/initialized"}))
	must(writer.Write(jsonrpc.Message{JSONRPC: "2.0", ID: raw(2), Method: "tools/list", Params: raw(map[string]any{})}))

	resp, err := reader.Read()
	must(err)
	if resp.Error != nil {
		panic(resp.Error.Message)
	}
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	must(json.Unmarshal(resp.Result, &result))
	fmt.Printf("tools=%d\n", len(result.Tools))
	for _, tool := range result.Tools {
		fmt.Println(tool.Name)
	}

	if *callTool == "" {
		return
	}
	var args any
	must(json.Unmarshal([]byte(*callArgs), &args))
	must(writer.Write(jsonrpc.Message{JSONRPC: "2.0", ID: raw(3), Method: "tools/call", Params: raw(map[string]any{
		"name":      *callTool,
		"arguments": args,
	})}))
	callResp, err := reader.Read()
	must(err)
	if callResp.Error != nil {
		panic(callResp.Error.Message)
	}
	var compact any
	must(json.Unmarshal(callResp.Result, &compact))
	if resultMap, ok := compact.(map[string]any); ok && resultMap["isError"] == true && !*allowToolError {
		panic("tool returned isError=true")
	}
	encoded, _ := json.Marshal(compact)
	fmt.Printf("call_result=%s\n", encoded)

	if closer, ok := stdin.(io.Closer); ok {
		_ = closer.Close()
	}
}

func raw(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
