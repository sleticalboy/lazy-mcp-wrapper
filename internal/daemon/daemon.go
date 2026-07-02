package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/binlee/lazy-mcp-wrapper/internal/wrapper"
)

type BindRequest struct {
	Name string `json:"name"`
}

type Server struct {
	socketPath string
	proxies    map[string]*wrapper.Proxy
	loggers    map[string]*log.Logger
	mu         sync.Mutex
}

func NewServer(socketPath string, configs []wrapper.Config, loggers map[string]*log.Logger) (*Server, error) {
	if socketPath == "" {
		return nil, fmt.Errorf("socket path is required")
	}
	if len(configs) == 0 {
		return nil, fmt.Errorf("at least one config is required")
	}

	proxies := make(map[string]*wrapper.Proxy, len(configs))
	for _, cfg := range configs {
		if _, exists := proxies[cfg.Name]; exists {
			return nil, fmt.Errorf("duplicate MCP name: %s", cfg.Name)
		}
		logger := loggers[cfg.Name]
		if logger == nil {
			logger = log.New(io.Discard, "", 0)
		}
		proxies[cfg.Name] = wrapper.NewProxyWithOptions(cfg, logger, wrapper.ProxyOptions{
			KeepRealOnClientClose: true,
		})
	}

	return &Server{
		socketPath: socketPath,
		proxies:    proxies,
		loggers:    loggers,
	}, nil
}

func (s *Server) Serve(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0755); err != nil {
		return err
	}
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(s.socketPath)

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return
	}

	var bind BindRequest
	if err := json.Unmarshal(line, &bind); err != nil || bind.Name == "" {
		_ = writeBindError(conn, "invalid bind request")
		return
	}

	proxy := s.proxy(bind.Name)
	if proxy == nil {
		_ = writeBindError(conn, "unknown MCP name: "+bind.Name)
		return
	}
	_ = writeBindOK(conn)

	stream := &boundConn{
		Reader: reader,
		Writer: conn,
	}
	_ = proxy.Run(ctx, stream, conn)
}

func (s *Server) proxy(name string) *wrapper.Proxy {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proxies[name]
}

func writeBindOK(w io.Writer) error {
	_, err := w.Write([]byte(`{"ok":true}` + "\n"))
	return err
}

func writeBindError(w io.Writer, message string) error {
	data, _ := json.Marshal(map[string]any{
		"ok":    false,
		"error": message,
	})
	_, err := w.Write(append(data, '\n'))
	return err
}

type boundConn struct {
	*bufio.Reader
	io.Writer
}
