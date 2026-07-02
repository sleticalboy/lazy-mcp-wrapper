package main

import (
	"os"

	"github.com/binlee/lazy-mcp-wrapper/internal/jsonrpc"
)

func main() {
	notifyToolsChanged := len(os.Args) > 1 && os.Args[1] == "--notify-tools-changed"
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
			if notifyToolsChanged {
				_ = writer.Write(jsonrpc.Message{
					JSONRPC: "2.0",
					Method:  "notifications/tools/list_changed",
				})
			}
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
