package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/jsonrpc"
)

type ProxyHTTPServer struct {
	proxy    *Proxy
	addr     string
	srv      *http.Server
	ln       net.Listener
	sessions sync.Map
}

type httpProxySession struct {
	inW  *io.PipeWriter
	done chan struct{}
}

func NewProxyHTTPServer(proxy *Proxy, addr string) *ProxyHTTPServer {
	s := &ProxyHTTPServer{proxy: proxy, addr: addr}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)
	s.srv = &http.Server{Addr: addr, Handler: mux}
	return s
}

func (s *ProxyHTTPServer) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.ln = ln
	go func() {
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.proxy.log.Printf("HTTP proxy server stopped: %v", err)
		}
	}()
	return nil
}

func (s *ProxyHTTPServer) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.srv.Shutdown(ctx)
}

func (s *ProxyHTTPServer) Addr() string {
	if s.ln != nil {
		return s.ln.Addr().String()
	}
	return s.addr
}

func (s *ProxyHTTPServer) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/sse":
		s.handleSSE(w, r)
	case r.Method == http.MethodPost && (r.URL.Path == "/" || r.URL.Path == "/messages"):
		s.handlePost(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *ProxyHTTPServer) handlePost(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(bytes.TrimSpace(body)) == 0 {
		http.Error(w, "empty request body", http.StatusBadRequest)
		return
	}

	var msg jsonrpc.Message
	if err := json.Unmarshal(body, &msg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if sessionID := r.URL.Query().Get("sessionId"); sessionID != "" {
		value, ok := s.sessions.Load(sessionID)
		if !ok {
			http.Error(w, "unknown sessionId", http.StatusNotFound)
			return
		}
		session := value.(*httpProxySession)
		if err := jsonrpc.NewJSONLWriter(session.inW).Write(msg); err != nil {
			http.Error(w, err.Error(), http.StatusGone)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}

	var in bytes.Buffer
	if err := jsonrpc.NewJSONLWriter(&in).Write(msg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var out bytes.Buffer
	if err := s.proxy.Run(r.Context(), &in, &out); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	reader := jsonrpc.NewJSONLReader(&out)
	resp, err := reader.Read()
	if err != nil {
		if msg.IsNotification() {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.proxy.log.Printf("write HTTP proxy response failed: %v", err)
	}
}

func (s *ProxyHTTPServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	defer outR.Close()

	errc := make(chan error, 1)
	go func() {
		errc <- s.proxy.Run(r.Context(), inR, outW)
		_ = outW.Close()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	sessionID := fmt.Sprintf("%d", time.Now().UnixNano())
	session := &httpProxySession{inW: inW, done: make(chan struct{})}
	s.sessions.Store(sessionID, session)
	defer func() {
		s.sessions.Delete(sessionID)
		_ = inW.Close()
		close(session.done)
	}()

	fmt.Fprintf(w, "event: endpoint\ndata: /messages?sessionId=%s\n\n", sessionID)
	flusher.Flush()

	reader := jsonrpc.NewReader(outR)
	var writeMu sync.Mutex
	writeEvent := func(msg jsonrpc.Message) error {
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_, err = fmt.Fprintf(w, "event: message\ndata: %s\n\n", strings.ReplaceAll(string(data), "\n", "\ndata: "))
		if err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	msgc := make(chan jsonrpc.Message)
	readErrc := make(chan error, 1)
	go func() {
		defer close(msgc)
		for {
			msg, err := reader.Read()
			if err != nil {
				readErrc <- err
				return
			}
			msgc <- msg
		}
	}()

	for {
		select {
		case <-r.Context().Done():
			_ = inW.Close()
			<-errc
			return
		case err := <-errc:
			if err != nil {
				s.proxy.log.Printf("HTTP SSE proxy session stopped: %v", err)
			}
			return
		case err := <-readErrc:
			if err != nil && err != io.EOF {
				s.proxy.log.Printf("HTTP SSE proxy output stopped: %v", err)
			}
			_ = inW.Close()
			<-errc
			return
		case msg, ok := <-msgc:
			if !ok {
				return
			}
			if err := writeEvent(msg); err != nil {
				_ = inW.Close()
				<-errc
				return
			}
		}
	}
}
