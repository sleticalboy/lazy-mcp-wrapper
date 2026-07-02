package main

import (
	"os"

	"github.com/binlee/lazy-mcp-wrapper/internal/jsonrpc"
)

func main() {
	reader := jsonrpc.NewReader(os.Stdin)
	writer := jsonrpc.NewWriter(os.Stdout)

	for {
		msg, err := reader.Read()
		if err != nil {
			return
		}
		if msg.IsNotification() {
			continue
		}

		switch msg.Method {
		case "initialize":
			_ = writer.Write(jsonrpc.Response(msg.ID, map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    "fake-mcp",
					"version": "0.1.0",
				},
			}))
		case "tools/list":
			_ = writer.Write(jsonrpc.Response(msg.ID, map[string]any{
				"tools": []map[string]any{
					{
						"name":        "echo",
						"description": "Echo test tool",
						"inputSchema": map[string]any{"type": "object"},
					},
				},
			}))
		case "tools/call":
			_ = writer.Write(jsonrpc.Response(msg.ID, map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "ok"},
				},
			}))
		default:
			_ = writer.Write(jsonrpc.ErrorResponse(msg.ID, -32601, "method not found"))
		}
	}
}
