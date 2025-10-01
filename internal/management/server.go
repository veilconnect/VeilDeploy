package management

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"stp/internal/logging"
)

type Server struct {
	snapshot func() interface{}
	metrics  func() map[string]float64
	logger   *logging.Logger
	server   *http.Server
	listener net.Listener
	acl      []netip.Prefix
	aclMu    sync.RWMutex
}

func New(bind string, snapshot func() interface{}, logger *logging.Logger, opts ...Option) (*Server, error) {
	if bind == "" {
		bind = "127.0.0.1:7777"
	}
	listener, err := net.Listen("tcp", bind)
	if err != nil {
		return nil, err
	}

	srv := &Server{
		snapshot: snapshot,
		logger:   logger,
		listener: listener,
	}
	for _, opt := range opts {
		opt(srv)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/state", srv.handleState)
	mux.HandleFunc("/healthz", srv.handleHealth)
	mux.HandleFunc("/metrics", srv.handleMetrics)

	srv.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return srv, nil
}

func (s *Server) Start() {
	go func() {
		s.logger.Info("management server started", map[string]interface{}{"addr": s.listener.Addr().String()})
		if err := s.server.Serve(s.listener); err != nil && err != http.ErrServerClosed {
			s.logger.Error("management server error", map[string]interface{}{"error": err.Error()})
		}
	}()
}

func (s *Server) Close(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func (s *Server) SetACL(prefixes []netip.Prefix) {
	s.aclMu.Lock()
	s.acl = append([]netip.Prefix(nil), prefixes...)
	s.aclMu.Unlock()
}

func (s *Server) allowed(remote string) bool {
	s.aclMu.RLock()
	acl := s.acl
	s.aclMu.RUnlock()
	if len(acl) == 0 {
		return true
	}
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	for _, prefix := range acl {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if !s.allowed(r.RemoteAddr) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	payload, err := json.Marshal(s.snapshot())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !s.allowed(r.RemoteAddr) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !s.allowed(r.RemoteAddr) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if s.metrics == nil {
		http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
		return
	}
	values := s.metrics()
	lines := make([]string, 0, len(values))
	for name, value := range values {
		sanitized := strings.ReplaceAll(name, " ", "_")
		lines = append(lines, sanitized+" "+formatFloat(value))
	}
	sort.Strings(lines)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	for _, line := range lines {
		_, _ = w.Write([]byte(line + "\n"))
	}
}

func formatFloat(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", v), "0"), ".")
}

// Option allows callers to customise the management server during construction.
type Option func(*Server)

// WithMetrics registers a metrics callback that will be exposed over the /metrics endpoint.
func WithMetrics(fn func() map[string]float64) Option {
	return func(s *Server) {
		s.metrics = fn
	}
}

func WithACL(prefixes []netip.Prefix) Option {
	return func(s *Server) {
		s.SetACL(prefixes)
	}
}
