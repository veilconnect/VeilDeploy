package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"stp/config"
	"stp/device"
	"stp/internal/logging"
	"stp/internal/management"
	"stp/internal/ratelimit"
	"stp/internal/state"
	"stp/transport"
)

func main() {
	var cfgPath string
	var overrideMode string
	flag.StringVar(&cfgPath, "config", "config.json", "Path to configuration file (or '-' for stdin)")
	flag.StringVar(&overrideMode, "mode", "", "Override mode (client/server)")
	flag.Parse()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	if overrideMode != "" {
		cfg.Mode = overrideMode
	}

	level := logging.ParseLevel(cfg.NormalisedLevel())
	baseLogger := logging.New(level, os.Stdout)
	componentLogger := baseLogger.With(map[string]interface{}{"component": "stp"})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	reloadTracker := state.NewReloadTracker(10)
	startConfigWatcher(ctx, cfgPath, baseLogger, reloadTracker, func(updated *config.Config) {
		baseLogger.SetLevel(logging.ParseLevel(updated.NormalisedLevel()))
	})

	switch strings.ToLower(cfg.Mode) {
	case "client":
		if err := runClient(ctx, cfgPath, cfg, baseLogger, reloadTracker); err != nil {
			componentLogger.Error("client exit", map[string]interface{}{"error": err.Error()})
			os.Exit(1)
		}
	case "server":
		if err := runServer(ctx, cfgPath, cfg, baseLogger, reloadTracker); err != nil {
			componentLogger.Error("server exit", map[string]interface{}{"error": err.Error()})
			os.Exit(1)
		}
	default:
		componentLogger.Error("unknown mode", map[string]interface{}{"mode": cfg.Mode})
		os.Exit(1)
	}
}

func runClient(ctx context.Context, cfgPath string, cfg *config.Config, baseLogger *logging.Logger, reloadTracker *state.ReloadTracker) error {
	componentLogger := baseLogger.With(map[string]interface{}{"component": "stp"})
	logger := componentLogger.With(map[string]interface{}{"role": "client"})
	dev, err := device.NewDevice(device.RoleClient, cfg, logger)
	if err != nil {
		return err
	}
	defer dev.Close()

	network, address := parseEndpoint(cfg.Endpoint)
	conn, err := transport.Dial(network, address)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := dev.Handshake(conn, cfg); err != nil {
		return err
	}

	mgmt, err := management.New(cfg.Management.Bind, func() interface{} {
		snapshot := dev.Snapshot()
		return map[string]interface{}{
			"device":  snapshot,
			"reloads": reloadTracker.GetHistory(),
		}
	}, logger, management.WithMetrics(dev.Metrics), management.WithACL(cfg.ManagementPrefixes()))
	if err != nil {
		return err
	}
	mgmt.Start()
	startConfigWatcher(ctx, cfgPath, logger, reloadTracker, func(updated *config.Config) {
		changes := []string{}

		// Update ACL
		mgmt.SetACL(updated.ManagementPrefixes())
		changes = append(changes, "management_acl")

		// Update logging level
		if updated.NormalisedLevel() != cfg.NormalisedLevel() {
			baseLogger.SetLevel(logging.ParseLevel(updated.NormalisedLevel()))
			changes = append(changes, "log_level")
		}

		// Update peers
		if peersChanged(cfg.Peers, updated.Peers) {
			if err := dev.UpdatePeers(updated.Peers); err != nil {
				logger.Warn("peer update failed", map[string]interface{}{"error": err.Error()})
			} else {
				changes = append(changes, "peers")
			}
		}

		// Update config reference
		cfg = updated

		reloadTracker.RecordSuccess(changes)
	})
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := mgmt.Close(shutdownCtx); err != nil {
			logger.Warn("management server close error", map[string]interface{}{"error": err.Error()})
		}
	}()

	done := make(chan struct{})
	go func() {
		dev.TunnelLoop(conn)
		close(done)
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received, closing client gracefully", nil)
		conn.Close()

		shutdownTimeout := time.NewTimer(5 * time.Second)
		defer shutdownTimeout.Stop()

		select {
		case <-done:
			logger.Info("client shutdown complete", nil)
		case <-shutdownTimeout.C:
			logger.Warn("client shutdown timeout, forcing exit", nil)
		}
	case <-done:
		logger.Info("tunnel loop ended", nil)
	}
	return nil
}

func runServer(ctx context.Context, cfgPath string, cfg *config.Config, baseLogger *logging.Logger, reloadTracker *state.ReloadTracker) error {
	componentLogger := baseLogger.With(map[string]interface{}{"component": "stp"})
	logger := componentLogger.With(map[string]interface{}{"role": "server"})
	network, address := parseEndpoint(cfg.Listen)
	listener, err := transport.Listen(network, address)
	if err != nil {
		return err
	}
	defer listener.Close()

	limiter := ratelimit.NewConnectionLimiter(
		cfg.EffectiveMaxConnections(),
		cfg.EffectiveConnectionRate(),
		cfg.EffectiveConnectionBurst(),
	)

	var sessionID atomic.Uint64
	registry := &sessionRegistry{
		logger:  logger,
		limiter: limiter,
	}

	mgmt, err := management.New(cfg.Management.Bind, func() interface{} {
		snapshot := registry.snapshot()
		return map[string]interface{}{
			"server":  snapshot,
			"reloads": reloadTracker.GetHistory(),
		}
	}, logger, management.WithMetrics(registry.metrics), management.WithACL(cfg.ManagementPrefixes()))
	if err != nil {
		return err
	}
	mgmt.Start()
	startConfigWatcher(ctx, cfgPath, logger, reloadTracker, func(updated *config.Config) {
		changes := []string{}

		// Update ACL
		mgmt.SetACL(updated.ManagementPrefixes())
		changes = append(changes, "management_acl")

		// Update connection limits
		oldMax := cfg.EffectiveMaxConnections()
		oldRate := cfg.EffectiveConnectionRate()
		oldBurst := cfg.EffectiveConnectionBurst()
		newMax := updated.EffectiveMaxConnections()
		newRate := updated.EffectiveConnectionRate()
		newBurst := updated.EffectiveConnectionBurst()

		if oldMax != newMax || oldRate != newRate || oldBurst != newBurst {
			limiter.Update(newMax, newRate, newBurst)
			changes = append(changes, "connection_limits")
		}

		// Update logging level
		if updated.NormalisedLevel() != cfg.NormalisedLevel() {
			baseLogger.SetLevel(logging.ParseLevel(updated.NormalisedLevel()))
			changes = append(changes, "log_level")
		}

		// Update peers
		if peersChanged(cfg.Peers, updated.Peers) {
			if err := registry.updatePeers(updated.Peers); err != nil {
				logger.Warn("peer update failed", map[string]interface{}{"error": err.Error()})
			} else {
				changes = append(changes, "peers")
			}
		}

		// Update config reference for future comparisons
		cfg = updated

		reloadTracker.RecordSuccess(changes)
	})
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mgmt.Close(shutdownCtx); err != nil {
			logger.Warn("management server close error", map[string]interface{}{"error": err.Error()})
		}
	}()

	shutdownComplete := make(chan struct{})
	go func() {
		<-ctx.Done()
		logger.Info("shutdown signal received, stopping server gracefully", nil)
		_ = listener.Close()

		shutdownDeadline := time.NewTimer(30 * time.Second)
		defer shutdownDeadline.Stop()

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

	drain:
		for {
			if registry.activeSessions() == 0 {
				logger.Info("all sessions closed", nil)
				break
			}

			select {
			case <-shutdownDeadline.C:
				remaining := registry.forceShutdown()
				if remaining > 0 {
					logger.Warn("shutdown timeout, forcing close", map[string]interface{}{"remainingSessions": remaining})
				}
				break drain
			case <-ticker.C:
				logger.Info("waiting for sessions to close", map[string]interface{}{"remainingSessions": registry.activeSessions()})
			}
		}

		close(shutdownComplete)
	}()

	logger.Info("server listening", map[string]interface{}{
		"addr":           address,
		"network":        network,
		"maxConnections": cfg.EffectiveMaxConnections(),
		"rateLimit":      cfg.EffectiveConnectionRate(),
	})

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				<-shutdownComplete
				logger.Info("server shutdown complete", nil)
				return nil
			}
			logger.Warn("accept error", map[string]interface{}{"error": err.Error()})
			select {
			case <-time.After(500 * time.Millisecond):
			case <-ctx.Done():
				<-shutdownComplete
				return nil
			}
			continue
		}

		if !limiter.Allow() {
			current, max, tokens := limiter.Stats()
			logger.Warn("connection rejected", map[string]interface{}{
				"reason":             "rate limit or max connections",
				"currentConnections": current,
				"maxConnections":     max,
				"availableTokens":    tokens,
				"remote":             conn.RemoteAddr().String(),
			})
			conn.Close()
			continue
		}

		id := sessionID.Add(1)
		peerLogger := logger.With(map[string]interface{}{"session": id})
		dev, err := device.NewDevice(device.RoleServer, cfg, peerLogger)
		if err != nil {
			peerLogger.Error("device init failed", map[string]interface{}{"error": err.Error()})
			conn.Close()
			limiter.Release()
			continue
		}

		registry.add(id, dev, conn)

		go func(conn net.Conn, dev *device.Device, id uint64) {
			defer func() {
				conn.Close()
				if state := registry.remove(id); state != nil {
					state.device.Close()
					limiter.Release()
				} else {
					dev.Close()
				}
			}()

			if err := dev.Handshake(conn, cfg); err != nil {
				peerLogger.Error("handshake failed", map[string]interface{}{"error": err.Error()})
				return
			}
			dev.TunnelLoop(conn)
		}(conn, dev, id)
	}
}

type sessionState struct {
	device *device.Device
	conn   net.Conn
}

type sessionRegistry struct {
	mu       sync.RWMutex
	sessions map[uint64]*sessionState
	logger   *logging.Logger
	limiter  *ratelimit.ConnectionLimiter
}

func (r *sessionRegistry) add(id uint64, dev *device.Device, conn net.Conn) {
	r.mu.Lock()
	if r.sessions == nil {
		r.sessions = make(map[uint64]*sessionState)
	}
	r.sessions[id] = &sessionState{device: dev, conn: conn}
	r.mu.Unlock()
	r.logger.Info("session added", map[string]interface{}{"session": id})
}

func (r *sessionRegistry) remove(id uint64) *sessionState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessions == nil {
		return nil
	}
	state, ok := r.sessions[id]
	if !ok {
		return nil
	}
	delete(r.sessions, id)
	r.logger.Info("session removed", map[string]interface{}{"session": id})
	return state
}

func (r *sessionRegistry) snapshot() interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var current, max int
	var tokens float64
	if r.limiter != nil {
		current, max, tokens = r.limiter.Stats()
	}

	out := struct {
		Sessions        []device.State `json:"sessions"`
		Count           int            `json:"count"`
		CurrentConns    int            `json:"currentConnections"`
		MaxConns        int            `json:"maxConnections"`
		AvailableTokens float64        `json:"availableTokens"`
	}{
		CurrentConns:    current,
		MaxConns:        max,
		AvailableTokens: tokens,
	}

	for _, state := range r.sessions {
		out.Sessions = append(out.Sessions, state.device.Snapshot())
	}
	out.Count = len(out.Sessions)
	return out
}

func (r *sessionRegistry) activeSessions() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions)
}

func (r *sessionRegistry) metrics() map[string]float64 {
	var devices []*device.Device
	r.mu.RLock()
	for _, state := range r.sessions {
		devices = append(devices, state.device)
	}
	r.mu.RUnlock()

	totalMessages := 0.0
	for _, dev := range devices {
		if dev == nil {
			continue
		}
		if stats := dev.Metrics(); stats != nil {
			if value, ok := stats["device_messages_total"]; ok {
				totalMessages += value
			}
		}
	}

	metrics := map[string]float64{
		"server_sessions":       float64(len(devices)),
		"server_messages_total": totalMessages,
	}
	if r.limiter != nil {
		current, max, tokens := r.limiter.Stats()
		metrics["server_current_connections"] = float64(current)
		metrics["server_max_connections"] = float64(max)
		metrics["server_available_tokens"] = tokens
	}
	return metrics
}

func (r *sessionRegistry) forceShutdown() int {
	r.mu.Lock()
	sessions := r.sessions
	r.sessions = make(map[uint64]*sessionState)
	r.mu.Unlock()

	closed := 0
	for id, state := range sessions {
		if state.conn != nil {
			state.conn.Close()
		}
		state.device.Close()
		if r.limiter != nil {
			r.limiter.Release()
		}
		r.logger.Info("session removed", map[string]interface{}{"session": id, "forced": true})
		closed++
	}
	return closed
}

func (r *sessionRegistry) updatePeers(peerConfigs []config.PeerConfig) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, state := range r.sessions {
		if err := state.device.UpdatePeers(peerConfigs); err != nil {
			return err
		}
	}
	return nil
}

func parseEndpoint(endpoint string) (network, address string) {
	network = "udp"
	address = endpoint
	if strings.Contains(endpoint, "://") {
		parts := strings.SplitN(endpoint, "://", 2)
		network = parts[0]
		address = parts[1]
	}
	return network, address
}

const configWatchInterval = 5 * time.Second

func startConfigWatcher(ctx context.Context, path string, logger *logging.Logger, tracker *state.ReloadTracker, apply func(*config.Config)) {
	if path == "" || path == "-" || apply == nil {
		return
	}
	info, err := os.Stat(path)
	lastMod := time.Time{}
	if err != nil {
		logger.Warn("config watcher stat failed", map[string]interface{}{"error": err.Error(), "path": path})
	} else {
		lastMod = info.ModTime()
	}
	ticker := time.NewTicker(configWatchInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				info, err := os.Stat(path)
				if err != nil {
					logger.Warn("config watcher stat failed", map[string]interface{}{"error": err.Error(), "path": path})
					continue
				}
				mod := info.ModTime()
				if !mod.After(lastMod) {
					continue
				}
				cfg, err := config.Load(path)
				if err != nil {
					logger.Warn("config reload failed", map[string]interface{}{"error": err.Error()})
					if tracker != nil {
						tracker.RecordFailure(err)
					}
					continue
				}
				apply(cfg)
				lastMod = mod
				logger.Info("config reloaded", map[string]interface{}{"path": path})
			}
		}
	}()
}

func peersChanged(old, new []config.PeerConfig) bool {
	if len(old) != len(new) {
		return true
	}

	oldMap := make(map[string]config.PeerConfig)
	for _, p := range old {
		oldMap[p.Name] = p
	}

	for _, newPeer := range new {
		oldPeer, exists := oldMap[newPeer.Name]
		if !exists {
			return true
		}

		// Check if AllowedIPs changed
		if len(oldPeer.AllowedIPs) != len(newPeer.AllowedIPs) {
			return true
		}

		allowedMap := make(map[string]bool)
		for _, ip := range oldPeer.AllowedIPs {
			allowedMap[ip] = true
		}
		for _, ip := range newPeer.AllowedIPs {
			if !allowedMap[ip] {
				return true
			}
		}

		// Check if Endpoint changed
		if oldPeer.Endpoint != newPeer.Endpoint {
			return true
		}
	}

	return false
}
