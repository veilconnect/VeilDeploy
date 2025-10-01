package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strings"
	"time"
)

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		return errors.New("empty duration")
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			d.Duration = 0
			return nil
		}
		dur, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("invalid duration string %q: %w", s, err)
		}
		d.Duration = dur
		return nil
	}
	var ms int64
	if err := json.Unmarshal(b, &ms); err != nil {
		return err
	}
	d.Duration = time.Duration(ms) * time.Millisecond
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

type TunnelConfig struct {
	Type         string   `json:"type"`
	Listen       string   `json:"listen,omitempty"`
	Name         string   `json:"name,omitempty"`         // TUN interface name
	MTU          int      `json:"mtu,omitempty"`          // TUN MTU (default 1420)
	Address      string   `json:"address,omitempty"`      // TUN local IP address (CIDR)
	AutoConfigure bool    `json:"autoConfigure,omitempty"` // Auto-configure interface
	Routes       []string `json:"routes,omitempty"`       // Additional routes to add
}

type Config struct {
	Mode            string           `json:"mode"`
	Listen          string           `json:"listen,omitempty"`
	Endpoint        string           `json:"endpoint,omitempty"`
	PSK             string           `json:"psk"`
	Keepalive       Duration         `json:"keepalive"`
	MaxPadding      uint8            `json:"maxPadding"`
	Peers           []PeerConfig     `json:"peers"`
	Management      ManagementConfig `json:"management"`
	Logging         LoggingConfig    `json:"logging"`
	RekeyInterval   Duration         `json:"rekeyInterval,omitempty"`
	RekeyBudget     uint64           `json:"rekeyBudget,omitempty"`
	MaxConnections  int              `json:"maxConnections,omitempty"`
	ConnectionRate  int              `json:"connectionRate,omitempty"`
	ConnectionBurst int              `json:"connectionBurst,omitempty"`
	Tunnel          TunnelConfig     `json:"tunnel"`
}

type PeerConfig struct {
	Name       string   `json:"name"`
	Endpoint   string   `json:"endpoint"`
	AllowedIPs []string `json:"allowedIPs"`
}

type ManagementConfig struct {
	Bind string   `json:"bind"`
	ACL  []string `json:"acl,omitempty"`
}

type LoggingConfig struct {
	Level  string `json:"level"`
	Output string `json:"output"`
}

func Load(path string) (*Config, error) {
	var reader io.ReadCloser
	if path == "-" {
		reader = io.NopCloser(os.Stdin)
	} else {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		reader = file
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	c.Mode = strings.ToLower(strings.TrimSpace(c.Mode))
	switch c.Mode {
	case "client", "server":
	default:
		return fmt.Errorf("unsupported mode %q", c.Mode)
	}

	if len(c.PSK) == 0 {
		return errors.New("psk must be provided")
	}
	if len(c.PSK) < 16 {
		return errors.New("psk must be at least 16 characters for security")
	}
	if c.PSK == "0123456789abcdef0123456789abcdef" {
		return errors.New("default PSK detected - please use a secure random PSK")
	}

	if c.Mode == "client" {
		if c.Endpoint == "" {
			return errors.New("client mode requires endpoint")
		}
		if err := validateEndpoint(c.Endpoint); err != nil {
			return fmt.Errorf("invalid endpoint: %w", err)
		}
	}
	if c.Mode == "server" {
		if c.Listen == "" {
			return errors.New("server mode requires listen address")
		}
		if err := validateEndpoint(c.Listen); err != nil {
			return fmt.Errorf("invalid listen address: %w", err)
		}
		if c.MaxConnections <= 0 {
			c.MaxConnections = 1000
		}
		if c.ConnectionRate <= 0 {
			c.ConnectionRate = 100
		}
		if c.ConnectionBurst <= 0 {
			c.ConnectionBurst = 10
		}
	}

	if len(c.Peers) == 0 {
		return errors.New("at least one peer must be configured")
	}

	seenNames := make(map[string]struct{})
	for _, peer := range c.Peers {
		name := strings.TrimSpace(peer.Name)
		if name == "" {
			return errors.New("peer name must be provided")
		}
		normalized := strings.ToLower(name)
		if _, exists := seenNames[normalized]; exists {
			return fmt.Errorf("duplicate peer name %q", name)
		}
		seenNames[normalized] = struct{}{}

		if c.Tunnel.Type == "udp-bridge" {
			if strings.TrimSpace(peer.Endpoint) == "" {
				return fmt.Errorf("peer %q requires endpoint for udp-bridge", name)
			}
		}

		for _, cidr := range peer.AllowedIPs {
			if _, err := netip.ParsePrefix(cidr); err != nil {
				return fmt.Errorf("peer %q has invalid allowed IP %q: %w", name, cidr, err)
			}
		}
	}

	if c.Keepalive.Duration < 0 {
		return errors.New("keepalive duration cannot be negative")
	}
	if c.Keepalive.Duration > 0 && c.Keepalive.Duration < 5*time.Second {
		return errors.New("keepalive duration must be at least 5 seconds if specified")
	}

	if c.RekeyInterval.Duration < 0 {
		return errors.New("rekey interval cannot be negative")
	}
	if c.RekeyInterval.Duration > 0 && c.RekeyInterval.Duration < time.Minute {
		return errors.New("rekey interval must be at least 1 minute if specified")
	}
	if c.RekeyBudget > 0 && c.RekeyBudget < 1000 {
		return errors.New("rekey budget must be at least 1000 messages if specified")
	}

	if c.Management.Bind == "" {
		c.Management.Bind = "127.0.0.1:7777"
	}

	if len(c.Management.ACL) == 0 {
		c.Management.ACL = []string{"127.0.0.0/8"}
	}
	for _, entry := range c.Management.ACL {
		if _, err := netip.ParsePrefix(entry); err != nil {
			return fmt.Errorf("invalid management acl entry %q: %w", entry, err)
		}
	}

	c.Tunnel.Type = strings.ToLower(strings.TrimSpace(c.Tunnel.Type))
	if c.Tunnel.Type == "" {
		c.Tunnel.Type = "loopback"
	}
	switch c.Tunnel.Type {
	case "loopback", "udp-bridge", "tun":
	default:
		return fmt.Errorf("unsupported tunnel type %q", c.Tunnel.Type)
	}

	if c.Tunnel.Type == "udp-bridge" {
		if c.Tunnel.Listen == "" {
			return errors.New("tunnel.listen is required for udp-bridge")
		}
	}

	if c.Tunnel.Type == "tun" {
		if c.Tunnel.Name == "" {
			c.Tunnel.Name = "stp0"
		}
		if c.Tunnel.MTU <= 0 {
			c.Tunnel.MTU = 1420
		}
		if c.Tunnel.MTU < 576 || c.Tunnel.MTU > 65535 {
			return fmt.Errorf("tun mtu %d out of valid range (576-65535)", c.Tunnel.MTU)
		}

		// Validate TUN address if autoconfigure is enabled
		if c.Tunnel.AutoConfigure && c.Tunnel.Address != "" {
			if _, err := netip.ParsePrefix(c.Tunnel.Address); err != nil {
				return fmt.Errorf("invalid tun address %q: %w", c.Tunnel.Address, err)
			}
		}

		// Validate routes
		for _, route := range c.Tunnel.Routes {
			if _, err := netip.ParsePrefix(route); err != nil {
				return fmt.Errorf("invalid tun route %q: %w", route, err)
			}
		}
	}

	return nil
}

func (c *Config) EffectiveKeepalive() time.Duration {
	if c.Keepalive.Duration <= 0 {
		return 15 * time.Second
	}
	return c.Keepalive.Duration
}

func (c *Config) EffectiveMaxPadding() uint8 {
	if c.MaxPadding == 0 {
		return 96
	}
	return c.MaxPadding
}

func (c *Config) NormalisedLevel() string {
	return strings.ToLower(strings.TrimSpace(c.Logging.Level))
}

func (c *Config) EffectiveRekeyInterval() time.Duration {
	if c.RekeyInterval.Duration <= 0 {
		return 30 * time.Minute
	}
	return c.RekeyInterval.Duration
}

func (c *Config) EffectiveRekeyBudget() uint64 {
	if c.RekeyBudget == 0 {
		return 16000
	}
	return c.RekeyBudget
}

func (c *Config) EffectiveMaxConnections() int {
	if c.MaxConnections <= 0 {
		return 1000
	}
	return c.MaxConnections
}

func (c *Config) EffectiveConnectionRate() int {
	if c.ConnectionRate <= 0 {
		return 100
	}
	return c.ConnectionRate
}

func (c *Config) EffectiveConnectionBurst() int {
	if c.ConnectionBurst <= 0 {
		return 10
	}
	return c.ConnectionBurst
}

func (c *Config) EffectiveTunnelType() string {
	if c.Tunnel.Type == "" {
		return "loopback"
	}
	return c.Tunnel.Type
}

func (c *Config) EffectiveTunnelListen() string {
	return strings.TrimSpace(c.Tunnel.Listen)
}

func (c *Config) EffectiveTunnelName() string {
	if c.Tunnel.Name == "" {
		return "stp0"
	}
	return c.Tunnel.Name
}

func (c *Config) EffectiveTunnelMTU() int {
	if c.Tunnel.MTU <= 0 {
		return 1420
	}
	return c.Tunnel.MTU
}

func (c *Config) ManagementPrefixes() []netip.Prefix {
	out := make([]netip.Prefix, 0, len(c.Management.ACL))
	for _, entry := range c.Management.ACL {
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			out = append(out, prefix)
		}
	}
	return out
}

func validateEndpoint(endpoint string) error {
	addr := endpoint
	if strings.Contains(endpoint, "://") {
		parts := strings.SplitN(endpoint, "://", 2)
		protocol := parts[0]
		addr = parts[1]
		validProtocols := map[string]bool{
			"tcp": true, "tcp4": true, "tcp6": true,
			"udp": true, "udp4": true, "udp6": true,
			"ws": true, "wss": true,
		}
		if !validProtocols[protocol] {
			return fmt.Errorf("unsupported protocol %q", protocol)
		}
	}

	host, port, err := splitHostPort(addr)
	if err != nil {
		return err
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("port %d out of valid range (1-65535)", port)
	}
	if host != "" && host != "0.0.0.0" && host != "::" {
		if strings.Contains(host, " ") {
			return errors.New("invalid hostname: contains spaces")
		}
	}
	return nil
}

func splitHostPort(addr string) (host string, port int, err error) {
	if strings.HasPrefix(addr, "[") {
		idx := strings.Index(addr, "]:")
		if idx == -1 {
			return "", 0, errors.New("invalid address format")
		}
		host = addr[1:idx]
		portStr := addr[idx+2:]
		var p int64
		p, err = parseInt(portStr)
		return host, int(p), err
	}
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return "", 0, errors.New("address must be in host:port format")
	}
	var p int64
	p, err = parseInt(parts[1])
	return parts[0], int(p), err
}

func parseInt(s string) (int64, error) {
	if s == "" {
		return 0, errors.New("empty port")
	}
	var result int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid port %q", s)
		}
		result = result*10 + int64(c-'0')
		if result > 65535 {
			return 0, fmt.Errorf("port too large")
		}
	}
	return result, nil
}
