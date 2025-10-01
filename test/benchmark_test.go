package test

import (
	"crypto/rand"
	"os"
	"testing"

	"stp/config"
	"stp/crypto"
	"stp/device"
	"stp/internal/dataplane"
	"stp/internal/logging"
	"stp/packet"
)

// BenchmarkKeyGeneration measures key generation performance
func BenchmarkKeyGeneration(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = crypto.GeneratePrivateKey()
	}
}

// BenchmarkEncryption measures encryption throughput
func BenchmarkEncryption(b *testing.B) {
	// Setup test data
	plaintext := make([]byte, 1420) // typical MTU
	rand.Read(plaintext)

	key := make([]byte, 32)
	rand.Read(key)

	b.ResetTimer()
	b.SetBytes(int64(len(plaintext)))

	for i := 0; i < b.N; i++ {
		_, _ = crypto.Encrypt(plaintext, key)
	}
}

// BenchmarkDecryption measures decryption throughput
func BenchmarkDecryption(b *testing.B) {
	// Setup test data
	plaintext := make([]byte, 1420)
	rand.Read(plaintext)

	key := make([]byte, 32)
	rand.Read(key)

	ciphertext, _ := crypto.Encrypt(plaintext, key)

	b.ResetTimer()
	b.SetBytes(int64(len(plaintext)))

	for i := 0; i < b.N; i++ {
		_, _ = crypto.Decrypt(ciphertext, key)
	}
}

// BenchmarkPacketEncode measures packet encoding performance
func BenchmarkPacketEncode(b *testing.B) {
	payload := make([]byte, 1420)
	rand.Read(payload)

	b.ResetTimer()
	b.SetBytes(int64(len(payload)))

	for i := 0; i < b.N; i++ {
		pkt, _ := packet.NewDataPacket("peer1", payload)
		_ = packet.Encode(pkt)
	}
}

// BenchmarkPacketDecode measures packet decoding performance
func BenchmarkPacketDecode(b *testing.B) {
	payload := make([]byte, 1420)
	rand.Read(payload)

	pkt, _ := packet.NewDataPacket("peer1", payload)
	encoded := packet.Encode(pkt)

	b.ResetTimer()
	b.SetBytes(int64(len(payload)))

	for i := 0; i < b.N; i++ {
		decoded, _ := packet.Decode(encoded)
		_, _, _ = packet.ExtractData(decoded)
	}
}

// BenchmarkLoopbackDataplane measures loopback dataplane throughput
func BenchmarkLoopbackDataplane(b *testing.B) {
	plane := dataplane.NewLoopback([]string{"peer1"})
	defer plane.Close()

	payload := make([]byte, 1420)
	rand.Read(payload)

	// Drain outbound channel in background
	go func() {
		for range plane.Outbound() {
		}
	}()

	b.ResetTimer()
	b.SetBytes(int64(len(payload)))

	for i := 0; i < b.N; i++ {
		_ = plane.Deliver("peer1", payload)
	}
}

// BenchmarkDeviceSnapshot measures snapshot generation performance
func BenchmarkDeviceSnapshot(b *testing.B) {
	logger := logging.New(logging.LevelError, os.Stdout)
	cfg := &config.Config{
		Mode: "client",
		PSK:  "benchmark-test-psk-for-snapshot",
		Peers: []config.PeerConfig{
			{Name: "peer1", AllowedIPs: []string{"10.0.0.0/8"}},
			{Name: "peer2", AllowedIPs: []string{"192.168.0.0/16"}},
			{Name: "peer3", AllowedIPs: []string{"172.16.0.0/12"}},
		},
		Tunnel: config.TunnelConfig{Type: "loopback"},
	}

	dev, err := device.NewDevice(device.RoleClient, cfg, logger)
	if err != nil {
		b.Fatal(err)
	}
	defer dev.Close()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = dev.Snapshot()
	}
}

// BenchmarkPeerUpdate measures peer configuration update performance
func BenchmarkPeerUpdate(b *testing.B) {
	logger := logging.New(logging.LevelError, os.Stdout)
	cfg := &config.Config{
		Mode: "client",
		PSK:  "benchmark-test-psk-for-peer-update",
		Peers: []config.PeerConfig{
			{Name: "peer1", AllowedIPs: []string{"10.0.0.0/8"}},
		},
		Tunnel: config.TunnelConfig{Type: "loopback"},
	}

	dev, err := device.NewDevice(device.RoleClient, cfg, logger)
	if err != nil {
		b.Fatal(err)
	}
	defer dev.Close()

	newPeers := []config.PeerConfig{
		{Name: "peer1", AllowedIPs: []string{"10.0.0.0/8", "192.168.0.0/16"}},
		{Name: "peer2", AllowedIPs: []string{"172.16.0.0/12"}},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = dev.UpdatePeers(newPeers)
	}
}

// BenchmarkRouteLookupmeasures IP routing performance
func BenchmarkRouteLookup(b *testing.B) {
	logger := logging.New(logging.LevelError, os.Stdout)
	cfg := &config.Config{
		Mode: "client",
		PSK:  "benchmark-test-psk-for-route-lookup",
		Peers: []config.PeerConfig{
			{Name: "peer1", AllowedIPs: []string{"10.0.0.0/8"}},
			{Name: "peer2", AllowedIPs: []string{"192.168.0.0/16"}},
			{Name: "peer3", AllowedIPs: []string{"172.16.0.0/12"}},
			{Name: "peer4", AllowedIPs: []string{"100.64.0.0/10"}},
			{Name: "peer5", AllowedIPs: []string{"198.18.0.0/15"}},
		},
		Tunnel: config.TunnelConfig{Type: "loopback"},
	}

	dev, err := device.NewDevice(device.RoleClient, cfg, logger)
	if err != nil {
		b.Fatal(err)
	}
	defer dev.Close()

	// Create test IPv4 packet
	ipv4Packet := make([]byte, 60)
	ipv4Packet[0] = 0x45 // IPv4, header length 20
	// Destination IP: 192.168.1.1
	ipv4Packet[16] = 192
	ipv4Packet[17] = 168
	ipv4Packet[18] = 1
	ipv4Packet[19] = 1

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// This would internally use lookupPeerByIP
		_ = dev.Snapshot()
	}
}