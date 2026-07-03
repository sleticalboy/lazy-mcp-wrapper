package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/jsonrpc"
)

func main() {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	notifyToolsChanged := fs.Bool("notify-tools-changed", false, "send tools/list changed notification")
	httpAddr := fs.String("http", "", "serve fake MCP over HTTP")
	_ = fs.Parse(os.Args[1:])

	if *httpAddr != "" {
		if err := serveHTTP(*httpAddr, *notifyToolsChanged); err != nil {
			log.Fatal(err)
		}
		return
	}
	serveStdio(*notifyToolsChanged)
}

func serveStdio(notifyToolsChanged bool) {
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

		resp, notify := handleMessage(msg, notifyToolsChanged)
		_ = writer.Write(resp)
		if notify != nil {
			_ = writer.Write(*notify)
		}
	}
}

func serveHTTP(addr string, notifyToolsChanged bool) error {
	sessions := httpSessions{}
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		sessionID := fmt.Sprintf("%d", time.Now().UnixNano())
		ch := sessions.add(sessionID)
		defer sessions.delete(sessionID)
		fmt.Fprintf(w, "event: endpoint\ndata: /messages?sessionId=%s\n\n", sessionID)
		flusher.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case msg := <-ch:
				writeSSE(w, flusher, msg)
			}
		}
	})
	mux.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sessionID := r.URL.Query().Get("sessionId")
		ch := sessions.get(sessionID)
		if ch == nil {
			http.Error(w, "unknown sessionId", http.StatusNotFound)
			return
		}
		var msg jsonrpc.Message
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()
		if msg.IsNotification() {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		resp, notify := handleMessage(msg, notifyToolsChanged)
		ch <- resp
		if notify != nil {
			ch <- *notify
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		var msg jsonrpc.Message
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()
		if msg.IsNotification() {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		resp, notify := handleMessage(msg, notifyToolsChanged)
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "streaming unsupported", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			writeSSE(w, flusher, resp)
			if notify != nil {
				writeSSE(w, flusher, *notify)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	return http.ListenAndServe(addr, mux)
}

func handleMessage(msg jsonrpc.Message, notifyToolsChanged bool) (jsonrpc.Message, *jsonrpc.Message) {
	switch msg.Method {
	case "initialize":
		return jsonrpc.Response(msg.ID, map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "fake-mcp",
				"version": "0.1.0",
			},
		}), nil
	case "tools/list":
		resp := jsonrpc.Response(msg.ID, map[string]any{
			"tools": []map[string]any{
				{
					"name":        "echo",
					"description": "Echo test tool",
					"inputSchema": map[string]any{"type": "object"},
				},
			},
		})
		if notifyToolsChanged {
			notify := jsonrpc.Message{
				JSONRPC: "2.0",
				Method:  "notifications/tools/list_changed",
			}
			return resp, &notify
		}
		return resp, nil
	case "tools/call":
		return jsonrpc.Response(msg.ID, map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
		}), nil
	default:
		return jsonrpc.ErrorResponse(msg.ID, -32601, "method not found"), nil
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, msg jsonrpc.Message) {
	data, _ := json.Marshal(msg)
	fmt.Fprintf(w, "event: message\ndata: %s\n\n", strings.ReplaceAll(string(data), "\n", "\ndata: "))
	flusher.Flush()
}

type httpSessions struct {
	mu       sync.Mutex
	sessions map[string]chan jsonrpc.Message
}

func (s *httpSessions) add(id string) chan jsonrpc.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = map[string]chan jsonrpc.Message{}
	}
	ch := make(chan jsonrpc.Message, 16)
	s.sessions[id] = ch
	return ch
}

func (s *httpSessions) get(id string) chan jsonrpc.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

func (s *httpSessions) delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}
