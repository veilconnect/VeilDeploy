package device

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"

	"stp/config"
	"stp/crypto"
	"stp/internal/dataplane"
	"stp/internal/logging"
	"stp/packet"
	"stp/peer"
	"stp/transport"
)

type Role int

const (
	RoleClient Role = iota
	RoleServer
)

type routeEntry struct {
	prefix netip.Prefix
	peer   string
}

type Device struct {
	role              Role
	privateKey        []byte
	transport         *transport.Transport
	secrets           crypto.SessionSecrets
	mu                sync.RWMutex
	keepaliveInterval time.Duration
	maxPadding        uint8
	rekeyInterval     time.Duration
	rekeyBudget       uint64
	logger            *logging.Logger
	messageCount      uint64
	pendingRekey      *crypto.RekeyContext

	plane        dataplane.Interface
	peers        map[string]*peer.Peer
	routes       []routeEntry
	outboundOnce sync.Once
	outboundStop chan struct{}
	outboundWG   sync.WaitGroup
	closed       bool
}

type State struct {
	Role         string          `json:"role"`
	Keepalive    time.Duration   `json:"keepalive"`
	MaxPadding   uint8           `json:"maxPadding"`
	SessionID    string          `json:"sessionId"`
	RekeyEpoch   uint32          `json:"rekeyEpoch"`
	Messages     uint64          `json:"messages"`
	PendingRekey bool            `json:"pendingRekey"`
	Peers        []peer.Snapshot `json:"peers"`
	LastSend     time.Time       `json:"lastSend"`
	LastReceive  time.Time       `json:"lastReceive"`
}

func NewDevice(role Role, cfg *config.Config, logger *logging.Logger) (*Device, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	privateKey, err := crypto.GeneratePrivateKey()
	if err != nil {
		return nil, err
	}

	keepalive := cfg.EffectiveKeepalive()
	maxPadding := cfg.EffectiveMaxPadding()

	peerMap := make(map[string]*peer.Peer, len(cfg.Peers))
	peerNames := make([]string, 0, len(cfg.Peers))
	routes := make([]routeEntry, 0)
	for _, peerCfg := range cfg.Peers {
		p := peer.NewPeer(peerCfg.Name, nil, peerCfg.AllowedIPs)
		peerMap[peerCfg.Name] = p
		peerNames = append(peerNames, peerCfg.Name)
		for _, cidr := range peerCfg.AllowedIPs {
			if prefix, err := netip.ParsePrefix(cidr); err == nil {
				routes = append(routes, routeEntry{prefix: prefix, peer: peerCfg.Name})
			}
		}
	}

	plane, err := createDataplane(cfg, peerNames)
	if err != nil {
		return nil, err
	}

	device := &Device{
		role:              role,
		privateKey:        privateKey,
		transport:         transport.NewTransport(logger),
		keepaliveInterval: keepalive,
		maxPadding:        maxPadding,
		rekeyInterval:     cfg.EffectiveRekeyInterval(),
		rekeyBudget:       cfg.EffectiveRekeyBudget(),
		logger:            logger,
		plane:             plane,
		peers:             peerMap,
		routes:            routes,
	}
	return device, nil
}

func createDataplane(cfg *config.Config, peers []string) (dataplane.Interface, error) {
	switch cfg.EffectiveTunnelType() {
	case "loopback":
		return dataplane.NewLoopback(peers), nil
	case "udp-bridge":
		listen := cfg.EffectiveTunnelListen()
		peerEndpoints := make(map[string]string, len(cfg.Peers))
		for _, peerCfg := range cfg.Peers {
			peerEndpoints[peerCfg.Name] = peerCfg.Endpoint
		}
		return dataplane.NewUDPBridge(listen, peerEndpoints)
	case "tun":
		name := cfg.EffectiveTunnelName()
		mtu := cfg.EffectiveTunnelMTU()
		return dataplane.NewTUNBridge(name, mtu, peers)
	default:
		return nil, fmt.Errorf("unsupported tunnel type %q", cfg.EffectiveTunnelType())
	}
}

func (d *Device) Handshake(conn net.Conn, cfg *config.Config) error {
	if d.privateKey == nil {
		return errors.New("device not initialised")
	}
	if cfg == nil {
		return errors.New("config required for handshake")
	}

	psk := resolvePSK(cfg.PSK)
	opts := crypto.HandshakeOptions{PreSharedKey: psk}
	if d.role == RoleServer {
		opts.KeepAlive = d.keepaliveInterval
		opts.MaxPadding = d.maxPadding
	}

	result, err := crypto.PerformHandshake(d.privateKey, conn, crypto.HandshakeRole(d.role), opts)
	if err != nil {
		return err
	}

	if err := d.transport.InstallSession(result.Secrets, result.Parameters); err != nil {
		return err
	}

	d.mu.Lock()
	d.secrets = result.Secrets
	d.keepaliveInterval = result.Parameters.KeepAlive
	d.maxPadding = result.Parameters.MaxPadding
	d.messageCount = 0
	d.pendingRekey = nil
	d.mu.Unlock()

	if err := d.transport.SendBind(conn); err != nil {
		d.logger.Warn("bind send failed", map[string]interface{}{"error": err.Error()})
	}

	d.logger.Info("handshake complete", map[string]interface{}{
		"remote":    conn.RemoteAddr().String(),
		"sessionId": hex.EncodeToString(result.Secrets.SessionID[:]),
		"role":      d.role.String(),
	})

	d.recordHandshake(conn.RemoteAddr(), result.Secrets)
	d.startOutboundPump(conn)
	return nil
}

func (d *Device) TunnelLoop(conn net.Conn) {
	stopKeepalive := d.startKeepalive(conn)
	defer stopKeepalive()

	rekeyTicker := time.NewTicker(d.rekeyInterval)
	defer rekeyTicker.Stop()

	for {
		frame, err := d.transport.Receive(conn)
		if err != nil {
			d.logger.Error("receive failed", map[string]interface{}{"error": err.Error()})
			return
		}

		switch frame.Flags {
		case transport.FlagData:
			if err := d.handleData(frame.Payload, conn); err != nil {
				d.logger.Warn("data delivery failed", map[string]interface{}{"error": err.Error()})
			}
		case transport.FlagKeepAlive:
			d.logger.Debug("keepalive received", map[string]interface{}{"remote": conn.RemoteAddr().String()})
		case transport.FlagRekey:
			if err := d.handleRekey(frame.Payload, conn); err != nil {
				d.logger.Error("rekey failed", map[string]interface{}{"error": err.Error()})
				return
			}
		case transport.FlagBind:
			d.logger.Info("transport bind acknowledged", map[string]interface{}{"remote": conn.RemoteAddr().String()})
		default:
			d.logger.Warn("unknown frame flag", map[string]interface{}{"flag": frame.Flags})
		}

		d.mu.Lock()
		d.messageCount++
		needsRekey := d.messageCount >= d.rekeyBudget
		d.mu.Unlock()

		select {
		case <-rekeyTicker.C:
			needsRekey = true
		default:
		}
		if needsRekey {
			if err := d.initiateRekey(conn); err != nil {
				d.logger.Error("rekey initiate failed", map[string]interface{}{"error": err.Error()})
				return
			}
			d.mu.Lock()
			d.messageCount = 0
			d.pendingRekey = nil
			d.mu.Unlock()
			rekeyTicker.Reset(d.rekeyInterval)
		}
	}
}

func (d *Device) handleData(payload []byte, conn net.Conn) error {
	pkt, err := packet.Decode(payload)
	if err != nil {
		return err
	}
	if pkt.Type != packet.TypeData {
		d.logger.Warn("unexpected packet type", map[string]interface{}{"type": pkt.Type})
		return nil
	}

	peerName, data, err := packet.ExtractData(pkt)
	if err != nil {
		return err
	}
	if peerName == "" {
		if dest, derr := destinationIP(data); derr == nil {
			peerName = d.lookupPeerByIP(dest)
		}
	}
	if peerName == "" {
		return errors.New("no route for payload")
	}

	p := d.ensurePeer(peerName, conn.RemoteAddr())
	if p != nil {
		p.TouchReceive()
	}

	if err := d.plane.Deliver(peerName, data); err != nil {
		return err
	}
	return nil
}

func (d *Device) handleRekey(payload []byte, conn net.Conn) error {
	d.mu.Lock()
	currentSecrets := d.secrets
	pending := d.pendingRekey
	d.mu.Unlock()

	updated, response, err := crypto.ProcessRekey(currentSecrets, payload, pending, crypto.HandshakeRole(d.role))
	if err != nil {
		return err
	}
	if updated == nil {
		return errors.New("rekey yielded no new secrets")
	}

	if err := d.transport.UpdateSessionKeys(*updated); err != nil {
		return err
	}

	epoch := updated.Epoch
	d.mu.Lock()
	d.secrets = *updated
	d.pendingRekey = nil
	d.mu.Unlock()

	d.broadcastRekey(epoch)
	d.logger.Info("rekey applied", map[string]interface{}{"epoch": epoch})

	if len(response) > 0 {
		if err := d.transport.SendRekey(conn, response); err != nil {
			return err
		}
		d.logger.Info("rekey response processed", map[string]interface{}{"epoch": epoch})
	}
	return nil
}

func (d *Device) initiateRekey(conn net.Conn) error {
	d.mu.Lock()
	if d.pendingRekey != nil {
		d.mu.Unlock()
		return nil
	}
	secrets := d.secrets
	d.mu.Unlock()

	ctx, err := crypto.NewRekeyRequest(secrets, crypto.HandshakeRole(d.role))
	if err != nil {
		return err
	}
	if err := d.transport.SendRekey(conn, ctx.Payload); err != nil {
		return err
	}

	d.mu.Lock()
	d.pendingRekey = ctx
	d.mu.Unlock()

	d.logger.Info("rekey request sent", map[string]interface{}{"epoch": secrets.Epoch + 1})
	return nil
}

func (d *Device) startKeepalive(conn net.Conn) func() {
	interval := d.keepaliveInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := d.transport.SendKeepAlive(conn); err != nil {
					d.logger.Warn("keepalive send failed", map[string]interface{}{"error": err.Error()})
					return
				}
			case <-stop:
				return
			}
		}
	}()
	return func() {
		close(stop)
	}
}

func (d *Device) Snapshot() State {
	send, recv := d.transport.LastActivity()
	d.mu.RLock()
	defer d.mu.RUnlock()

	peers := make([]peer.Snapshot, 0, len(d.peers))
	for _, p := range d.peers {
		snap := p.Snapshot()
		peers = append(peers, snap)
	}

	state := State{
		Role:         d.role.String(),
		Keepalive:    d.keepaliveInterval,
		MaxPadding:   d.maxPadding,
		SessionID:    hex.EncodeToString(d.secrets.SessionID[:]),
		RekeyEpoch:   d.secrets.Epoch,
		Messages:     d.messageCount,
		PendingRekey: d.pendingRekey != nil,
		Peers:        peers,
		LastSend:     send,
		LastReceive:  recv,
	}
	return state
}

func (d *Device) Metrics() map[string]float64 {
	send, recv := d.transport.LastActivity()
	d.mu.RLock()
	messages := d.messageCount
	epoch := d.secrets.Epoch
	pending := d.pendingRekey != nil
	peerCount := len(d.peers)
	keepalive := d.keepaliveInterval
	d.mu.RUnlock()

	metrics := map[string]float64{
		"device_messages_total":    float64(messages),
		"device_rekey_epoch":       float64(epoch),
		"device_pending_rekey":     boolToFloat(pending),
		"device_peer_count":        float64(peerCount),
		"device_keepalive_seconds": keepalive.Seconds(),
	}
	if !send.IsZero() {
		metrics["device_last_send_age_seconds"] = time.Since(send).Seconds()
	}
	if !recv.IsZero() {
		metrics["device_last_receive_age_seconds"] = time.Since(recv).Seconds()
	}
	return metrics
}

func (d *Device) Close() error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	stop := d.outboundStop
	d.mu.Unlock()

	if stop != nil {
		close(stop)
	}
	d.outboundWG.Wait()
	if d.plane != nil {
		return d.plane.Close()
	}
	return nil
}

func (d *Device) recordHandshake(remote net.Addr, secrets crypto.SessionSecrets) {
	if remote == nil {
		return
	}
	d.mu.Lock()
	for _, p := range d.peers {
		p.UpdateHandshake(secrets.SessionID, remote, secrets.Epoch)
	}
	d.mu.Unlock()
}

func (d *Device) startOutboundPump(conn net.Conn) {
	d.outboundOnce.Do(func() {
		d.mu.Lock()
		d.outboundStop = make(chan struct{})
		stop := d.outboundStop
		d.mu.Unlock()

		d.outboundWG.Add(1)
		go func() {
			defer d.outboundWG.Done()
			for {
				select {
				case frame, ok := <-d.plane.Outbound():
					if !ok {
						return
					}
					peerName := frame.Peer
					payload := frame.Payload
					if peerName == "" {
						if dest, err := destinationIP(payload); err == nil {
							peerName = d.lookupPeerByIP(dest)
						}
					}
					if peerName == "" {
						d.logger.Warn("drop outbound payload", map[string]interface{}{"reason": "no-route", "bytes": len(payload)})
						continue
					}
					pkt, err := packet.NewDataPacket(peerName, payload)
					if err != nil {
						d.logger.Warn("drop outbound payload", map[string]interface{}{"reason": err.Error()})
						continue
					}
					if err := d.transport.SendPayload(conn, packet.Encode(pkt)); err != nil {
						d.logger.Error("send payload failed", map[string]interface{}{"error": err.Error()})
						return
					}
					if p := d.ensurePeer(peerName, conn.RemoteAddr()); p != nil {
						p.TouchSend()
					}
				case <-stop:
					return
				}
			}
		}()
	})
}

func (d *Device) lookupPeerByIP(addr netip.Addr) string {
	for _, route := range d.routes {
		if route.prefix.Contains(addr) {
			return route.peer
		}
	}
	return ""
}

func (d *Device) ensurePeer(name string, remote net.Addr) *peer.Peer {
	if name == "" {
		return nil
	}
	d.mu.Lock()
	p, exists := d.peers[name]
	if !exists {
		p = peer.NewPeer(name, remote, nil)
		d.peers[name] = p
		if loop, ok := d.plane.(*dataplane.Loopback); ok {
			loop.EnsurePeer(name)
		}
	}
	if remote != nil {
		p.UpdateHandshake(d.secrets.SessionID, remote, d.secrets.Epoch)
	}
	d.mu.Unlock()
	return p
}

func (d *Device) broadcastRekey(epoch uint32) {
	d.mu.Lock()
	for _, p := range d.peers {
		p.UpdateRekey(epoch)
	}
	d.mu.Unlock()
}

func boolToFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

func destinationIP(payload []byte) (netip.Addr, error) {
	if len(payload) == 0 {
		return netip.Addr{}, errors.New("empty payload")
	}
	version := payload[0] >> 4
	switch version {
	case 4:
		if len(payload) < 20 {
			return netip.Addr{}, errors.New("ipv4 header truncated")
		}
		var dst [4]byte
		copy(dst[:], payload[16:20])
		return netip.AddrFrom4(dst), nil
	case 6:
		if len(payload) < 40 {
			return netip.Addr{}, errors.New("ipv6 header truncated")
		}
		var dst [16]byte
		copy(dst[:], payload[24:40])
		return netip.AddrFrom16(dst), nil
	default:
		return netip.Addr{}, errors.New("unsupported ip version")
	}
}

func resolvePSK(input string) []byte {
	if value := os.Getenv("STP_PSK"); value != "" {
		input = value
	}
	if input == "" {
		panic("PSK must be explicitly configured - no default PSK available")
	}
	if len(input) >= crypto.KeySize {
		return []byte(input)[:crypto.KeySize]
	}
	sum := sha256.Sum256([]byte(input))
	return sum[:]
}

func (r Role) String() string {
	switch r {
	case RoleClient:
		return "client"
	case RoleServer:
		return "server"
	default:
		return "unknown"
	}
}

// UpdatePeers updates the peer configuration dynamically
func (d *Device) UpdatePeers(peerConfigs []config.PeerConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Build new peer map and routes
	newPeerMap := make(map[string]*peer.Peer, len(peerConfigs))
	newRoutes := make([]routeEntry, 0)
	newPeerNames := make([]string, 0, len(peerConfigs))

	for _, peerCfg := range peerConfigs {
		newPeerNames = append(newPeerNames, peerCfg.Name)

		// Preserve existing peer state if it exists
		if existingPeer, exists := d.peers[peerCfg.Name]; exists {
			// Update AllowedIPs
			existingPeer.UpdateAllowedIPs(peerCfg.AllowedIPs)
			newPeerMap[peerCfg.Name] = existingPeer
		} else {
			// Create new peer
			newPeerMap[peerCfg.Name] = peer.NewPeer(peerCfg.Name, nil, peerCfg.AllowedIPs)
		}

		// Build routes
		for _, cidr := range peerCfg.AllowedIPs {
			if prefix, err := netip.ParsePrefix(cidr); err == nil {
				newRoutes = append(newRoutes, routeEntry{prefix: prefix, peer: peerCfg.Name})
			}
		}
	}

	// Update device state
	d.peers = newPeerMap
	d.routes = newRoutes

	// Update dataplane peers
	if loop, ok := d.plane.(*dataplane.Loopback); ok {
		for _, name := range newPeerNames {
			loop.EnsurePeer(name)
		}
	}

	return nil
}
