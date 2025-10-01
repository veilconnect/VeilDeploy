package device

import (
	"net"
	"testing"
	"time"

	"stp/config"
	"stp/internal/dataplane"
	"stp/internal/logging"
	"stp/packet"
)

type stubConn struct {
	remote net.Addr
}

func (s stubConn) Read(_ []byte) (int, error)       { return 0, nil }
func (s stubConn) Write(_ []byte) (int, error)      { return 0, nil }
func (s stubConn) Close() error                     { return nil }
func (s stubConn) LocalAddr() net.Addr              { return &net.IPAddr{IP: net.ParseIP("127.0.0.1")} }
func (s stubConn) RemoteAddr() net.Addr             { return s.remote }
func (s stubConn) SetDeadline(time.Time) error      { return nil }
func (s stubConn) SetReadDeadline(time.Time) error  { return nil }
func (s stubConn) SetWriteDeadline(time.Time) error { return nil }

func TestDeviceHandleDataRouteByPeerAndCIDR(t *testing.T) {
	cfg := &config.Config{
		Mode: "client",
		PSK:  "0123456789abcdef0123456789abcdef",
		Peers: []config.PeerConfig{
			{Name: "alpha", AllowedIPs: []string{"10.0.0.0/24"}},
			{Name: "beta", AllowedIPs: []string{"10.0.1.0/24"}},
		},
		Tunnel:        config.TunnelConfig{Type: "loopback"},
		Logging:       config.LoggingConfig{Level: "error"},
		Management:    config.ManagementConfig{Bind: "127.0.0.1:0"},
		Keepalive:     config.Duration{Duration: time.Second},
		RekeyInterval: config.Duration{Duration: time.Minute},
	}
	logger := logging.New(logging.LevelError, nil)

	dev, err := NewDevice(RoleClient, cfg, logger)
	if err != nil {
		t.Fatalf("new device: %v", err)
	}
	loop := dev.plane.(*dataplane.Loopback)

	alphaSub, err := loop.Subscribe("alpha", 1)
	if err != nil {
		t.Fatalf("subscribe alpha: %v", err)
	}

	pkt, err := packet.NewDataPacket("alpha", []byte("payload-alpha"))
	if err != nil {
		t.Fatalf("new data packet: %v", err)
	}
	conn := stubConn{remote: &net.UDPAddr{IP: net.ParseIP("203.0.113.1"), Port: 5000}}
	if err := dev.handleData(packet.Encode(pkt), conn); err != nil {
		t.Fatalf("handle data: %v", err)
	}

	select {
	case data := <-alphaSub:
		if string(data) != "payload-alpha" {
			t.Fatalf("unexpected payload %q", data)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for alpha payload")
	}

	betaSub, err := loop.Subscribe("beta", 1)
	if err != nil {
		t.Fatalf("subscribe beta: %v", err)
	}

	ipv4 := make([]byte, 20)
	ipv4[0] = 0x45
	copy(ipv4[16:20], []byte{10, 0, 1, 42})
	raw := packet.Packet{Type: packet.TypeData, Flags: 0, Payload: ipv4}
	if err := dev.handleData(packet.Encode(&raw), conn); err != nil {
		t.Fatalf("handle data route: %v", err)
	}

	select {
	case data := <-betaSub:
		if len(data) != len(ipv4) {
			t.Fatalf("expected %d bytes, got %d", len(ipv4), len(data))
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for beta payload")
	}
}
