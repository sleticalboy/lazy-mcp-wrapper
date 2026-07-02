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
	Force   bool   `json:"force,omitempty"`
}

type Status struct {
	SocketPath       string         `json:"socket_path"`
	DaemonConfigPath string         `json:"daemon_config_path,omitempty"`
	DaemonPID        int            `json:"daemon_pid"`
	StartedAt        time.Time      `json:"started_at"`
	Uptime           string         `json:"uptime"`
	Clients          int64          `json:"clients"`
	TotalCalls       int64          `json:"total_calls"`
	LastError        string         `json:"last_error,omitempty"`
	ActiveClients    []ClientStatus `json:"active_clients,omitempty"`
	Servers          []ServerStatus `json:"servers"`
}

type ClientStatus struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ConnectedAt time.Time `json:"connected_at"`
	RemoteAddr  string    `json:"remote_addr,omitempty"`
}

type ServerStatus struct {
	wrapper.ProxyStatus
	Calls          int64      `json:"calls"`
	Errors         int64      `json:"errors"`
	LastLatencyMS  int64      `json:"last_latency_ms,omitempty"`
	AvgLatencyMS   int64      `json:"avg_latency_ms,omitempty"`
	MaxLatencyMS   int64      `json:"max_latency_ms,omitempty"`
	LastMethod     string     `json:"last_method,omitempty"`
	LastCallAt     *time.Time `json:"last_call_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	LastErrorAt    *time.Time `json:"last_error_at,omitempty"`
	LastReloadedAt *time.Time `json:"last_reloaded_at,omitempty"`
}

type ControlResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

type Server struct {
	socketPath       string
	daemonConfigPath string
	proxies          map[string]*wrapper.Proxy
	loggers          map[string]*log.Logger
	closers          map[string]io.Closer
	stats            map[string]*proxyStats
	activeClients    map[string]ClientStatus
	startedAt        time.Time
	cancel           context.CancelFunc
	clients          atomic.Int64
	nextClientID     atomic.Uint64
	totalCalls       atomic.Int64
	lastError        string
	mu               sync.Mutex
}

type proxyStats struct {
	calls          atomic.Int64
	errors         atomic.Int64
	totalLatencyNS atomic.Int64
	maxLatencyNS   atomic.Int64
	lastReloadedAt time.Time
	mu             sync.Mutex
	lastMethod     string
	lastCallAt     time.Time
	lastError      string
	lastErrorAt    time.Time
	lastLatency    time.Duration
}

func NewServer(socketPath string, configs []wrapper.Config, loggers map[string]*log.Logger) (*Server, error) {
	if socketPath == "" {
		return nil, fmt.Errorf("socket path is required")
	}
	if len(configs) == 0 {
		return nil, fmt.Errorf("at least one config is required")
	}

	server := &Server{
		socketPath:    socketPath,
		proxies:       make(map[string]*wrapper.Proxy, len(configs)),
		loggers:       make(map[string]*log.Logger, len(configs)),
		closers:       map[string]io.Closer{},
		stats:         make(map[string]*proxyStats, len(configs)),
		activeClients: make(map[string]ClientStatus),
		startedAt:     time.Now(),
	}
	for _, cfg := range configs {
		var logger *log.Logger
		if loggers != nil {
			logger = loggers[cfg.Name]
		}
		if err := server.addProxyLocked(cfg, logger, nil, time.Time{}); err != nil {
			return nil, err
		}
	}

	return server, nil
}

func NewServerFromConfig(path string) (*Server, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	if cfg.SocketPath == "" {
		return nil, fmt.Errorf("socket path is required")
	}
	if len(cfg.ConfigPaths) == 0 {
		return nil, fmt.Errorf("at least one config is required")
	}

	server := &Server{
		socketPath:       cfg.SocketPath,
		daemonConfigPath: path,
		proxies:          make(map[string]*wrapper.Proxy, len(cfg.ConfigPaths)),
		loggers:          make(map[string]*log.Logger, len(cfg.ConfigPaths)),
		closers:          make(map[string]io.Closer, len(cfg.ConfigPaths)),
		stats:            make(map[string]*proxyStats, len(cfg.ConfigPaths)),
		activeClients:    make(map[string]ClientStatus),
		startedAt:        time.Now(),
	}
	if err := server.reloadFromConfigLocked(time.Time{}); err != nil {
		server.closeResources()
		return nil, err
	}
	return server, nil
}

func (s *Server) addProxyLocked(cfg wrapper.Config, logger *log.Logger, closer io.Closer, reloadedAt time.Time) error {
	if _, exists := s.proxies[cfg.Name]; exists {
		if closer != nil {
			_ = closer.Close()
		}
		return fmt.Errorf("duplicate MCP name: %s", cfg.Name)
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	name := cfg.Name
	proxy, stats := s.buildProxy(cfg, logger, reloadedAt)
	s.proxies[name] = proxy
	s.stats[name] = stats
	s.loggers[name] = logger
	if closer != nil {
		s.closers[name] = closer
	}
	return nil
}

func (s *Server) buildProxy(cfg wrapper.Config, logger *log.Logger, reloadedAt time.Time) (*wrapper.Proxy, *proxyStats) {
	name := cfg.Name
	stats := &proxyStats{lastReloadedAt: reloadedAt}
	proxy := wrapper.NewProxyWithOptions(cfg, logger, wrapper.ProxyOptions{
		KeepRealOnClientClose: true,
		OnRequest: func(method string) {
			if method != "initialize" && method != "ping" {
				s.totalCalls.Add(1)
				stats.calls.Add(1)
			}
			stats.recordCall(method)
			logger.Printf("client request name=%s method=%s", name, method)
		},
		OnResponse: func(method string, duration time.Duration, hasError bool, errorMessage string) {
			if method != "initialize" && method != "ping" {
				stats.recordLatency(duration)
			}
			if hasError {
				stats.recordError(method, errorMessage)
				s.setLastError(fmt.Errorf("%s %s: %s", name, method, errorMessage))
				logger.Printf("client response error name=%s method=%s latency=%s error=%s", name, method, duration, errorMessage)
				return
			}
			logger.Printf("client response name=%s method=%s latency=%s", name, method, duration)
		},
	})
	return proxy, stats
}

func (s *Server) reloadFromConfigLocked(reloadedAt time.Time) error {
	cfg, err := LoadConfig(s.daemonConfigPath)
	if err != nil {
		return err
	}
	if cfg.SocketPath != s.socketPath {
		return fmt.Errorf("reload cannot change socket path from %s to %s", s.socketPath, cfg.SocketPath)
	}

	proxies := make(map[string]*wrapper.Proxy, len(cfg.ConfigPaths))
	loggers := make(map[string]*log.Logger, len(cfg.ConfigPaths))
	closers := make(map[string]io.Closer, len(cfg.ConfigPaths))
	stats := make(map[string]*proxyStats, len(cfg.ConfigPaths))
	next := &Server{
		socketPath: s.socketPath,
		proxies:    proxies,
		loggers:    loggers,
		closers:    closers,
		stats:      stats,
	}
	for _, path := range cfg.ConfigPaths {
		mcpConfig, err := wrapper.LoadConfig(path)
		if err != nil {
			next.closeResources()
			return fmt.Errorf("load config %s: %w", path, err)
		}
		logger, closer, err := wrapper.NewLogger(mcpConfig.LogFile)
		if err != nil {
			next.closeResources()
			return fmt.Errorf("open log for %s: %w", mcpConfig.Name, err)
		}
		if _, exists := proxies[mcpConfig.Name]; exists {
			if closer != nil {
				_ = closer.Close()
			}
			next.closeResources()
			return fmt.Errorf("duplicate MCP name: %s", mcpConfig.Name)
		}
		proxy, stat := s.buildProxy(mcpConfig, logger, reloadedAt)
		proxies[mcpConfig.Name] = proxy
		loggers[mcpConfig.Name] = logger
		stats[mcpConfig.Name] = stat
		if closer != nil {
			closers[mcpConfig.Name] = closer
		}
	}
	s.closeResources()
	s.proxies = next.proxies
	s.loggers = next.loggers
	s.closers = next.closers
	s.stats = next.stats
	return nil
}

func (s *Server) closeResources() {
	for _, proxy := range s.proxies {
		proxy.Close()
	}
	for _, closer := range s.closers {
		_ = closer.Close()
	}
}

func (s *proxyStats) recordCall(method string) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastMethod = method
	s.lastCallAt = now
}

func (s *proxyStats) recordError(method, message string) {
	now := time.Now()
	s.errors.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastMethod = method
	s.lastCallAt = now
	s.lastError = message
	s.lastErrorAt = now
}

func (s *proxyStats) recordLatency(duration time.Duration) {
	s.totalLatencyNS.Add(duration.Nanoseconds())
	for {
		current := s.maxLatencyNS.Load()
		if duration.Nanoseconds() <= current || s.maxLatencyNS.CompareAndSwap(current, duration.Nanoseconds()) {
			break
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastLatency = duration
}

func (s *proxyStats) status() (calls int64, errors int64, lastLatencyMS int64, avgLatencyMS int64, maxLatencyMS int64, lastMethod string, lastCallAt *time.Time, lastError string, lastErrorAt *time.Time, lastReloadedAt *time.Time) {
	calls = s.calls.Load()
	errors = s.errors.Load()
	if calls > 0 {
		avgLatencyMS = durationMillis(time.Duration(s.totalLatencyNS.Load() / calls))
	}
	maxLatencyMS = durationMillis(time.Duration(s.maxLatencyNS.Load()))
	s.mu.Lock()
	defer s.mu.Unlock()
	lastLatencyMS = durationMillis(s.lastLatency)
	lastMethod = s.lastMethod
	if !s.lastCallAt.IsZero() {
		value := s.lastCallAt
		lastCallAt = &value
	}
	lastError = s.lastError
	if !s.lastErrorAt.IsZero() {
		value := s.lastErrorAt
		lastErrorAt = &value
	}
	if !s.lastReloadedAt.IsZero() {
		value := s.lastReloadedAt
		lastReloadedAt = &value
	}
	return calls, errors, lastLatencyMS, avgLatencyMS, maxLatencyMS, lastMethod, lastCallAt, lastError, lastErrorAt, lastReloadedAt
}

func durationMillis(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	ms := d.Milliseconds()
	if ms == 0 {
		return 1
	}
	return ms
}

func (s *Server) Serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	defer cancel()
	defer s.closeResources()

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
			if err := s.reload(bind.Force); err != nil {
				_ = s.writeControl(conn, ControlResponse{OK: false, Error: err.Error()})
				return
			}
			_ = s.writeControl(conn, ControlResponse{OK: true, Message: "daemon config reloaded"})
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
	client := s.registerClient(bind.Name, conn.RemoteAddr().String())
	defer s.unregisterClient(client.ID)
	logger := s.logger(bind.Name)
	logger.Printf("client connected id=%s name=%s remote=%s", client.ID, bind.Name, client.RemoteAddr)
	defer logger.Printf("client disconnected id=%s name=%s", client.ID, bind.Name)

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

func (s *Server) proxyAndStats(name string) (*wrapper.Proxy, *proxyStats) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proxies[name], s.stats[name]
}

func (s *Server) logger(name string) *log.Logger {
	s.mu.Lock()
	defer s.mu.Unlock()
	if logger := s.loggers[name]; logger != nil {
		return logger
	}
	return log.New(io.Discard, "", 0)
}

func (s *Server) registerClient(name, remoteAddr string) ClientStatus {
	id := fmt.Sprintf("client-%d", s.nextClientID.Add(1))
	client := ClientStatus{
		ID:          id,
		Name:        name,
		ConnectedAt: time.Now(),
		RemoteAddr:  remoteAddr,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeClients[id] = client
	return client
}

func (s *Server) unregisterClient(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.activeClients, id)
}

func (s *Server) Status() Status {
	s.mu.Lock()
	startedAt := s.startedAt
	lastError := s.lastError
	daemonConfigPath := s.daemonConfigPath
	activeClients := make([]ClientStatus, 0, len(s.activeClients))
	for _, client := range s.activeClients {
		activeClients = append(activeClients, client)
	}
	sort.Slice(activeClients, func(i, j int) bool {
		return activeClients[i].ID < activeClients[j].ID
	})

	names := make([]string, 0, len(s.proxies))
	for name := range s.proxies {
		names = append(names, name)
	}
	sort.Strings(names)
	s.mu.Unlock()

	status := Status{
		SocketPath:       s.socketPath,
		DaemonConfigPath: daemonConfigPath,
		DaemonPID:        os.Getpid(),
		StartedAt:        startedAt,
		Uptime:           time.Since(startedAt).Round(time.Second).String(),
		Clients:          s.clients.Load(),
		TotalCalls:       s.totalCalls.Load(),
		LastError:        lastError,
		ActiveClients:    activeClients,
		Servers:          make([]ServerStatus, 0, len(names)),
	}
	for _, name := range names {
		proxy, stats := s.proxyAndStats(name)
		if proxy == nil {
			continue
		}
		serverStatus := ServerStatus{ProxyStatus: proxy.Status()}
		if stats != nil {
			serverStatus.Calls, serverStatus.Errors, serverStatus.LastLatencyMS, serverStatus.AvgLatencyMS, serverStatus.MaxLatencyMS, serverStatus.LastMethod, serverStatus.LastCallAt, serverStatus.LastError, serverStatus.LastErrorAt, serverStatus.LastReloadedAt = stats.status()
		}
		status.Servers = append(status.Servers, serverStatus)
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

func (s *Server) reload(force bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.daemonConfigPath == "" {
		return fmt.Errorf("reload requires daemon to start with --daemon-config")
	}
	if !force && len(s.activeClients) > 0 {
		return fmt.Errorf("reload busy: %d active client(s); retry later or use --force", len(s.activeClients))
	}
	if err := s.reloadFromConfigLocked(time.Now()); err != nil {
		s.lastError = err.Error()
		return err
	}
	s.lastError = ""
	return nil
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
