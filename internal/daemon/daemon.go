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
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/binlee/lazy-mcp-wrapper/internal/wrapper"
)

type BindRequest struct {
	Name    string `json:"name"`
	Control string `json:"control"`
}

type Status struct {
	SocketPath string                `json:"socket_path"`
	DaemonPID  int                   `json:"daemon_pid"`
	StartedAt  time.Time             `json:"started_at"`
	Uptime     string                `json:"uptime"`
	Clients    int64                 `json:"clients"`
	TotalCalls int64                 `json:"total_calls"`
	LastError  string                `json:"last_error,omitempty"`
	Servers    []wrapper.ProxyStatus `json:"servers"`
}

type ControlResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

type Server struct {
	socketPath string
	proxies    map[string]*wrapper.Proxy
	loggers    map[string]*log.Logger
	startedAt  time.Time
	cancel     context.CancelFunc
	clients    atomic.Int64
	totalCalls atomic.Int64
	lastError  string
	mu         sync.Mutex
}

func NewServer(socketPath string, configs []wrapper.Config, loggers map[string]*log.Logger) (*Server, error) {
	if socketPath == "" {
		return nil, fmt.Errorf("socket path is required")
	}
	if len(configs) == 0 {
		return nil, fmt.Errorf("at least one config is required")
	}

	server := &Server{
		socketPath: socketPath,
		proxies:    make(map[string]*wrapper.Proxy, len(configs)),
		loggers:    loggers,
		startedAt:  time.Now(),
	}
	for _, cfg := range configs {
		if _, exists := server.proxies[cfg.Name]; exists {
			return nil, fmt.Errorf("duplicate MCP name: %s", cfg.Name)
		}
		logger := loggers[cfg.Name]
		if logger == nil {
			logger = log.New(io.Discard, "", 0)
		}
		name := cfg.Name
		server.proxies[name] = wrapper.NewProxyWithOptions(cfg, logger, wrapper.ProxyOptions{
			KeepRealOnClientClose: true,
			OnRequest: func(method string) {
				if method != "initialize" && method != "ping" {
					server.totalCalls.Add(1)
				}
				logger.Printf("client request name=%s method=%s", name, method)
			},
		})
	}

	return server, nil
}

func (s *Server) Serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	defer cancel()

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
			s.setLastError(err)
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
		switch bind.Control {
		case "status":
			_ = s.writeStatus(conn)
			return
		case "stop":
			_ = s.writeControl(conn, ControlResponse{OK: true, Message: "stopping daemon"})
			s.stop()
			return
		case "reload":
			_ = s.writeControl(conn, ControlResponse{OK: false, Error: "hot reload is not supported; restart the LaunchAgent"})
			return
		}
		_ = writeBindError(conn, "invalid bind request")
		return
	}

	proxy := s.proxy(bind.Name)
	if proxy == nil {
		s.setLastError(fmt.Errorf("unknown MCP name: %s", bind.Name))
		_ = writeBindError(conn, "unknown MCP name: "+bind.Name)
		return
	}
	_ = writeBindOK(conn)
	s.clients.Add(1)
	defer s.clients.Add(-1)
	logger := s.logger(bind.Name)
	logger.Printf("client connected name=%s", bind.Name)
	defer logger.Printf("client disconnected name=%s", bind.Name)

	stream := &boundConn{
		Reader: reader,
		Writer: conn,
	}
	if err := proxy.Run(ctx, stream, conn); err != nil {
		logger.Printf("client session ended name=%s error=%v", bind.Name, err)
		s.setLastError(err)
	}
}

func (s *Server) proxy(name string) *wrapper.Proxy {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proxies[name]
}

func (s *Server) logger(name string) *log.Logger {
	s.mu.Lock()
	defer s.mu.Unlock()
	if logger := s.loggers[name]; logger != nil {
		return logger
	}
	return log.New(io.Discard, "", 0)
}

func (s *Server) Status() Status {
	s.mu.Lock()
	startedAt := s.startedAt
	lastError := s.lastError

	names := make([]string, 0, len(s.proxies))
	for name := range s.proxies {
		names = append(names, name)
	}
	sort.Strings(names)
	s.mu.Unlock()

	status := Status{
		SocketPath: s.socketPath,
		DaemonPID:  os.Getpid(),
		StartedAt:  startedAt,
		Uptime:     time.Since(startedAt).Round(time.Second).String(),
		Clients:    s.clients.Load(),
		TotalCalls: s.totalCalls.Load(),
		LastError:  lastError,
		Servers:    make([]wrapper.ProxyStatus, 0, len(names)),
	}
	for _, name := range names {
		if proxy := s.proxy(name); proxy != nil {
			status.Servers = append(status.Servers, proxy.Status())
		}
	}
	return status
}

func (s *Server) writeStatus(w io.Writer) error {
	data, err := json.Marshal(s.Status())
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

func (s *Server) writeControl(w io.Writer, resp ControlResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

func (s *Server) stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		go cancel()
	}
}

func (s *Server) setLastError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastError = err.Error()
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
