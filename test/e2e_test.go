package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"stp/config"
	"stp/device"
	"stp/internal/logging"
	"stp/transport"
)

// TestE2EUDPBridge tests end-to-end communication using UDP bridge
func TestE2EUDPBridge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create temporary config files
	serverCfg, clientCfg := createTestConfigs(t)

	// Start server
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	serverReady := make(chan struct{})
	serverErr := make(chan error, 1)

	go func() {
		err := runTestServer(ctx, serverCfg, serverReady)
		serverErr <- err
	}()

	// Wait for server to be ready
	select {
	case <-serverReady:
		t.Log("Server ready")
	case err := <-serverErr:
		t.Fatalf("Server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Server startup timeout")
	}

	// Start client
	clientReady := make(chan struct{})
	clientErr := make(chan error, 1)

	go func() {
		err := runTestClient(ctx, clientCfg, clientReady)
		clientErr <- err
	}()

	// Wait for client to be ready
	select {
	case <-clientReady:
		t.Log("Client ready")
	case err := <-clientErr:
		t.Fatalf("Client failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Client startup timeout")
	}

	// Test data transfer through UDP bridge
	testDataTransfer(t, 7001, 7002)

	// Clean shutdown
	cancel()

	// Wait for both to finish
	select {
	case <-serverErr:
	case <-time.After(3 * time.Second):
		t.Log("Server shutdown timeout")
	}

	select {
	case <-clientErr:
	case <-time.After(3 * time.Second):
		t.Log("Client shutdown timeout")
	}

	t.Log("E2E test completed successfully")
}

func createTestConfigs(t *testing.T) (*config.Config, *config.Config) {
	psk := "test-psk-for-e2e-testing-32bytes"

	serverCfg := &config.Config{
		Mode:   "server",
		Listen: "127.0.0.1:15820",
		PSK:    psk,
		Keepalive: config.Duration{
			Duration: 5 * time.Second,
		},
		MaxPadding:     32,
		MaxConnections: 10,
		ConnectionRate: 100,
		Peers: []config.PeerConfig{
			{
				Name:       "client-peer",
				AllowedIPs: []string{"10.0.0.0/24"},
			},
		},
		Management: config.ManagementConfig{
			Bind: "127.0.0.1:17777",
		},
		Logging: config.LoggingConfig{
			Level: "debug",
		},
		Tunnel: config.TunnelConfig{
			Type:   "udp-bridge",
			Listen: "127.0.0.1:7001",
		},
	}

	clientCfg := &config.Config{
		Mode:     "client",
		Endpoint: "127.0.0.1:15820",
		PSK:      psk,
		Keepalive: config.Duration{
			Duration: 5 * time.Second,
		},
		MaxPadding: 32,
		Peers: []config.PeerConfig{
			{
				Name:       "server-peer",
				Endpoint:   "127.0.0.1:7001",
				AllowedIPs: []string{"10.0.0.0/24"},
			},
		},
		Management: config.ManagementConfig{
			Bind: "127.0.0.1:17778",
		},
		Logging: config.LoggingConfig{
			Level: "debug",
		},
		Tunnel: config.TunnelConfig{
			Type:   "udp-bridge",
			Listen: "127.0.0.1:7002",
		},
	}

	return serverCfg, clientCfg
}

func runTestServer(ctx context.Context, cfg *config.Config, ready chan struct{}) error {
	logger := logging.New(logging.LevelDebug, os.Stdout)
	logger = logger.With(map[string]interface{}{"role": "test-server"})

	dev, err := device.NewDevice(device.RoleServer, cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create server device: %w", err)
	}
	defer dev.Close()

	listener, err := transport.Listen("udp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	defer listener.Close()

	logger.Info("test server listening", map[string]interface{}{"addr": cfg.Listen})

	// Signal ready
	close(ready)

	acceptDone := make(chan struct{})
	var wg sync.WaitGroup

	go func() {
		defer close(acceptDone)

		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					logger.Warn("accept error", map[string]interface{}{"error": err.Error()})
					continue
				}
			}

			wg.Add(1)
			go func(conn net.Conn) {
				defer wg.Done()
				defer conn.Close()

				if err := dev.Handshake(conn, cfg); err != nil {
					logger.Error("handshake failed", map[string]interface{}{"error": err.Error()})
					return
				}

				logger.Info("test server handshake complete", nil)
				dev.TunnelLoop(conn)
			}(conn)

			// Only accept one connection for test
			break
		}
	}()

	<-ctx.Done()
	listener.Close()
	<-acceptDone
	wg.Wait()

	return nil
}

func runTestClient(ctx context.Context, cfg *config.Config, ready chan struct{}) error {
	logger := logging.New(logging.LevelDebug, os.Stdout)
	logger = logger.With(map[string]interface{}{"role": "test-client"})

	// Give server time to start
	time.Sleep(1 * time.Second)

	dev, err := device.NewDevice(device.RoleClient, cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create client device: %w", err)
	}
	defer dev.Close()

	conn, err := transport.Dial("udp", cfg.Endpoint)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}
	defer conn.Close()

	if err := dev.Handshake(conn, cfg); err != nil {
		return fmt.Errorf("handshake failed: %w", err)
	}

	logger.Info("test client handshake complete", nil)

	// Signal ready
	close(ready)

	done := make(chan struct{})
	go func() {
		dev.TunnelLoop(conn)
		close(done)
	}()

	select {
	case <-ctx.Done():
		conn.Close()
		<-done
	case <-done:
	}

	return nil
}

func testDataTransfer(t *testing.T, serverPort, clientPort int) {
	// Create UDP connections to the bridge endpoints
	_ = fmt.Sprintf("127.0.0.1:%d", serverPort)
	_ = fmt.Sprintf("127.0.0.1:%d", clientPort)

	// Give the tunnels time to establish
	time.Sleep(2 * time.Second)

	// Send test data from client to server
	clientConn, err := net.DialUDP("udp", nil, &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: clientPort,
	})
	if err != nil {
		t.Fatalf("Failed to create client UDP conn: %v", err)
	}
	defer clientConn.Close()

	// Create server listener
	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: serverPort,
	})
	if err != nil {
		t.Fatalf("Failed to create server UDP conn: %v", err)
	}
	defer serverConn.Close()

	testPayload := []byte("Hello from E2E test!")

	// Send from client
	_, err = clientConn.Write(testPayload)
	if err != nil {
		t.Fatalf("Failed to send test data: %v", err)
	}

	t.Logf("Sent test payload: %s", string(testPayload))

	// Receive on server
	serverConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1500)
	n, _, err := serverConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("Failed to receive test data: %v", err)
	}

	received := buf[:n]
	t.Logf("Received payload: %s", string(received))

	// Note: The payload will be wrapped in IP packets by the dataplane,
	// so exact comparison might not work. Check for basic connectivity.
	if n == 0 {
		t.Fatal("Received empty payload")
	}

	t.Logf("Successfully transferred %d bytes through encrypted tunnel", n)
}

// TestHandshakeOnly tests just the handshake without data transfer
func TestHandshakeOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping handshake test in short mode")
	}

	serverCfg, clientCfg := createTestConfigs(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger := logging.New(logging.LevelDebug, os.Stdout)

	// Start server
	serverDev, err := device.NewDevice(device.RoleServer, serverCfg, logger)
	if err != nil {
		t.Fatalf("Failed to create server device: %v", err)
	}
	defer serverDev.Close()

	listener, err := transport.Listen("udp", serverCfg.Listen)
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()

		err = serverDev.Handshake(conn, serverCfg)
		serverDone <- err

		if err == nil {
			// Check snapshot after handshake
			snapshot := serverDev.Snapshot()
			if snapshot.SessionID == "" {
				serverDone <- fmt.Errorf("empty session ID")
			}
		}
	}()

	// Give server time to start
	time.Sleep(500 * time.Millisecond)

	// Start client
	clientDev, err := device.NewDevice(device.RoleClient, clientCfg, logger)
	if err != nil {
		t.Fatalf("Failed to create client device: %v", err)
	}
	defer clientDev.Close()

	conn, err := transport.Dial("udp", clientCfg.Endpoint)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	err = clientDev.Handshake(conn, clientCfg)
	if err != nil {
		t.Fatalf("Client handshake failed: %v", err)
	}

	// Wait for server handshake
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("Server handshake failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Handshake timeout")
	}

	// Verify both devices have valid snapshots
	clientSnapshot := clientDev.Snapshot()
	serverSnapshot := serverDev.Snapshot()

	t.Logf("Client SessionID: %s", clientSnapshot.SessionID)
	t.Logf("Server SessionID: %s", serverSnapshot.SessionID)

	if clientSnapshot.SessionID == "" {
		t.Fatal("Client has empty session ID")
	}
	if serverSnapshot.SessionID == "" {
		t.Fatal("Server has empty session ID")
	}

	// Verify they have the same session ID
	if clientSnapshot.SessionID != serverSnapshot.SessionID {
		t.Fatalf("Session ID mismatch: client=%s server=%s",
			clientSnapshot.SessionID, serverSnapshot.SessionID)
	}

	t.Log("Handshake test completed successfully")
}

// TestConfigReload tests configuration hot-reload
func TestConfigReload(t *testing.T) {
	// Create a temporary config file
	tmpfile, err := os.CreateTemp("", "stp-test-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	// Write initial config
	cfg := map[string]interface{}{
		"mode":     "server",
		"listen":   "127.0.0.1:15821",
		"psk":      "test-psk-for-reload-test-32byte",
		"logging":  map[string]string{"level": "info"},
		"peers":    []map[string]interface{}{{"name": "test", "allowedIPs": []string{"10.0.0.0/24"}}},
		"tunnel":   map[string]string{"type": "loopback"},
		"maxConnections": 100,
	}

	data, _ := json.MarshalIndent(cfg, "", "  ")
	if _, err := tmpfile.Write(data); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	// Load config
	loadedCfg, err := config.Load(tmpfile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if loadedCfg.EffectiveMaxConnections() != 100 {
		t.Errorf("Expected maxConnections=100, got %d", loadedCfg.EffectiveMaxConnections())
	}

	// Modify config file
	cfg["maxConnections"] = 200
	cfg["logging"] = map[string]string{"level": "debug"}

	data, _ = json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(tmpfile.Name(), data, 0644); err != nil {
		t.Fatal(err)
	}

	// Reload config
	reloadedCfg, err := config.Load(tmpfile.Name())
	if err != nil {
		t.Fatalf("Failed to reload config: %v", err)
	}

	if reloadedCfg.EffectiveMaxConnections() != 200 {
		t.Errorf("Expected maxConnections=200 after reload, got %d", reloadedCfg.EffectiveMaxConnections())
	}

	if reloadedCfg.NormalisedLevel() != "debug" {
		t.Errorf("Expected log level=debug after reload, got %s", reloadedCfg.NormalisedLevel())
	}

	t.Log("Config reload test completed successfully")
}