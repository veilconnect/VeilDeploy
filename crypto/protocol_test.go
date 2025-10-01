package crypto

import (
	"bytes"
	"net"
	"testing"
	"time"
)

// TestNoiseHandshake tests the Noise protocol handshake
func TestNoiseHandshake(t *testing.T) {
	// Create PSK
	psk := make([]byte, 32)
	copy(psk, []byte("test-preshared-key-123456789012"))

	// Generate static keys
	clientStatic, err := GeneratePrivateKey()
	if err != nil {
		t.Fatalf("Failed to generate client key: %v", err)
	}
	clientPub, err := derivePublicKey(clientStatic)
	if err != nil {
		t.Fatalf("Failed to derive client public key: %v", err)
	}

	serverStatic, err := GeneratePrivateKey()
	if err != nil {
		t.Fatalf("Failed to generate server key: %v", err)
	}
	serverPub, err := derivePublicKey(serverStatic)
	if err != nil {
		t.Fatalf("Failed to derive server public key: %v", err)
	}

	// Create pipe for communication
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	// Client and server results
	type result struct {
		res *NoiseHandshakeResult
		err error
	}
	clientResult := make(chan result, 1)
	serverResult := make(chan result, 1)

	// Client handshake
	go func() {
		opts := NoiseHandshakeOptions{
			Pattern:      NoiseIKpsk2,
			PreSharedKey: psk,
			StaticKey:    clientStatic,
			RemoteStatic: serverPub[:],
			KeepAlive:    15 * time.Second,
			MaxPadding:   96,
			CipherSuites: []CipherSuite{CipherSuiteChaCha20Poly1305},
			MinVersion:   1,
		}
		res, err := PerformNoiseHandshake(clientConn, RoleClient, opts)
		clientResult <- result{res, err}
	}()

	// Server handshake
	go func() {
		opts := NoiseHandshakeOptions{
			Pattern:      NoiseIKpsk2,
			PreSharedKey: psk,
			StaticKey:    serverStatic,
			KeepAlive:    15 * time.Second,
			MaxPadding:   96,
			CipherSuites: []CipherSuite{CipherSuiteChaCha20Poly1305},
			MinVersion:   1,
		}
		res, err := PerformNoiseHandshake(serverConn, RoleServer, opts)
		serverResult <- result{res, err}
	}()

	// Wait for both sides
	cRes := <-clientResult
	sRes := <-serverResult

	if cRes.err != nil {
		t.Fatalf("Client handshake failed: %v", cRes.err)
	}
	if sRes.err != nil {
		t.Fatalf("Server handshake failed: %v", sRes.err)
	}

	// Verify session IDs match
	if !bytes.Equal(cRes.res.Secrets.SessionID[:], sRes.res.Secrets.SessionID[:]) {
		t.Error("Session IDs don't match")
	}

	// Verify keys are different
	if bytes.Equal(cRes.res.Secrets.SendKey, sRes.res.Secrets.SendKey) {
		t.Error("Client and server send keys should be different")
	}

	// Verify client send key == server receive key
	if !bytes.Equal(cRes.res.Secrets.SendKey, sRes.res.Secrets.ReceiveKey) {
		t.Error("Client send key != server receive key")
	}

	// Verify remote static keys
	if !bytes.Equal(cRes.res.RemoteStatic[:], serverPub[:]) {
		t.Error("Client doesn't have correct server static key")
	}
	if !bytes.Equal(sRes.res.RemoteStatic[:], clientPub[:]) {
		t.Error("Server doesn't have correct client static key")
	}

	t.Logf("Handshake successful: version=%d, cipher=%s",
		cRes.res.Version, cRes.res.CipherSuite)
}

// TestPFSManager tests the PFS manager
func TestPFSManager(t *testing.T) {
	secrets := SessionSecrets{
		SendKey:        make([]byte, 32),
		ReceiveKey:     make([]byte, 32),
		ObfuscationKey: make([]byte, 32),
		Epoch:          1,
		Established:    time.Now(),
	}

	config := PFSConfig{
		RekeyInterval:      0, // Disable time-based
		RekeyAfterMessages: 10,
		RekeyAfterBytes:    0, // Disable bytes-based
		MaxEpochAge:        0, // Disable age-based
	}

	mgr := NewPFSManager(secrets, RoleClient, config)

	// Initially should not need rekey
	if mgr.NeedsRekey() {
		t.Error("Should not need rekey initially")
	}

	// Record some traffic below threshold
	mgr.RecordSent(5, 512)
	mgr.RecordReceived(4, 512) // Total 9 messages

	if mgr.NeedsRekey() {
		stats := mgr.Stats()
		t.Errorf("Should not need rekey at 9 messages (threshold=10), got total=%d",
			stats.MessagesSent+stats.MessagesReceived)
	}

	// Exceed threshold
	mgr.RecordSent(2, 0) // Total 11 messages

	if !mgr.NeedsRekey() {
		stats := mgr.Stats()
		t.Errorf("Should need rekey at 11 messages (threshold=10), got total=%d",
			stats.MessagesSent+stats.MessagesReceived)
	}

	stats := mgr.Stats()
	t.Logf("PFS Stats: epoch=%d, total_msgs=%d", stats.CurrentEpoch,
		stats.MessagesSent+stats.MessagesReceived)
}

// TestAntiReplay tests anti-replay protection
func TestAntiReplay(t *testing.T) {
	config := AntiReplayConfig{
		Mode:       AntiReplayWindow,
		WindowSize: 64,
		MaxAge:     60 * time.Second,
	}

	ar := NewAntiReplay(config)

	// First message should be accepted
	if err := ar.Check(100); err != nil {
		t.Errorf("First message rejected: %v", err)
	}
	ar.Accept(100)

	// Replay should be rejected
	if err := ar.Check(100); err == nil {
		t.Error("Replay not detected")
	}

	// Future message should be accepted
	if err := ar.Check(200); err != nil {
		t.Errorf("Future message rejected: %v", err)
	}
	ar.Accept(200)

	// Old message within window should be accepted once
	if err := ar.Check(190); err != nil {
		t.Errorf("Message within window rejected: %v", err)
	}
	ar.Accept(190)

	// Replay within window should be rejected
	if err := ar.Check(190); err == nil {
		t.Error("Replay within window not detected")
	}

	// Message too old should be rejected
	if err := ar.Check(100); err == nil {
		t.Error("Old message not rejected")
	}

	t.Logf("Anti-replay tests passed")
}

// TestObfuscation tests traffic obfuscation
func TestObfuscation(t *testing.T) {
	secrets := SessionSecrets{
		SessionID:      [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		ObfuscationKey: make([]byte, 32),
		Established:    time.Now(),
	}
	copy(secrets.ObfuscationKey, []byte("test-obfuscation-key-1234567890"))

	// Test None mode (passthrough)
	t.Run("None", func(t *testing.T) {
		config := ObfsConfig{Mode: ObfsModeNone}
		obfs, _ := NewObfuscator(secrets, config)

		plaintext := []byte("Hello, World!")
		obfuscated, _ := obfs.ObfuscateFrame(plaintext)
		recovered, _ := obfs.DeobfuscateFrame(obfuscated)

		if !bytes.Equal(plaintext, recovered) {
			t.Errorf("None mode failed: got %q, want %q", recovered, plaintext)
		}
	})

	// Test that obfuscation exists
	t.Run("Obfuscates", func(t *testing.T) {
		plaintext := []byte("Hello, World! This is a test message.")

		// XOR mode
		config := ObfsConfig{
			Mode:       ObfsModeXOR,
			MinPadding: 0,
			MaxPadding: 0,
		}

		obfs, err := NewObfuscator(secrets, config)
		if err != nil {
			t.Fatalf("Failed to create obfuscator: %v", err)
		}

		obfuscated, err := obfs.ObfuscateFrame(plaintext)
		if err != nil {
			t.Fatalf("Obfuscation failed: %v", err)
		}

		// Obfuscated data should be different
		if bytes.Equal(plaintext, obfuscated) {
			t.Error("Obfuscated data is identical to plaintext")
		}

		t.Logf("Obfuscation works: %d bytes -> %d bytes", len(plaintext), len(obfuscated))
	})
}

// TestNegotiation tests protocol negotiation
func TestNegotiation(t *testing.T) {
	// Test without downgrade protection first for simplicity
	clientConfig := NegotiationConfig{
		MinVersion: ProtocolVersionNoise,
		MaxVersion: ProtocolVersionCurrent,
		CipherSuites: []CipherSuite{
			CipherSuiteChaCha20Poly1305,
			CipherSuiteAES256GCM,
		},
		RequirePFS:          true,
		RequireAntiReplay:   true,
		DowngradeProtection: false, // Disable for basic test
	}

	serverConfig := clientConfig

	// Client creates offer
	clientState := NewNegotiationState(clientConfig, RoleClient)
	offer, err := clientState.CreateOffer()
	if err != nil {
		t.Fatalf("Failed to create offer: %v", err)
	}

	// Server processes offer and creates response
	serverState := NewNegotiationState(serverConfig, RoleServer)
	response, err := serverState.ProcessOffer(offer)
	if err != nil {
		t.Fatalf("Failed to process offer: %v", err)
	}

	// Client processes response
	if err := clientState.ProcessResponse(response); err != nil {
		t.Fatalf("Failed to process response: %v", err)
	}

	// Get negotiated params
	clientParams := clientState.GetNegotiatedParams()
	serverParams := serverState.GetNegotiatedParams()

	// Verify agreement
	if clientParams.Version != serverParams.Version {
		t.Error("Version mismatch")
	}
	if clientParams.CipherSuite != serverParams.CipherSuite {
		t.Error("Cipher suite mismatch")
	}
	if clientParams.Features != serverParams.Features {
		t.Error("Features mismatch")
	}

	t.Logf("Negotiation successful: version=%s, cipher=%d, features=%s",
		clientParams.Version, clientParams.CipherSuite, clientParams.Features)
}

// TestDowngradeProtection tests anti-downgrade protection
func TestDowngradeProtection(t *testing.T) {
	signingKey := make([]byte, 32)
	copy(signingKey, []byte("test-signing-key-1234567890abcd"))

	// Client offers latest version
	clientConfig := NegotiationConfig{
		MinVersion:          ProtocolVersionNoise,
		MaxVersion:          ProtocolVersionCurrent,
		CipherSuites:        []CipherSuite{CipherSuiteChaCha20Poly1305},
		DowngradeProtection: true,
		SigningKey:          signingKey,
	}

	// Attacker tries to force downgrade
	attackerConfig := NegotiationConfig{
		MinVersion:          ProtocolVersionLegacy,
		MaxVersion:          ProtocolVersionLegacy,
		CipherSuites:        []CipherSuite{CipherSuiteChaCha20Poly1305},
		DowngradeProtection: false, // Attacker doesn't sign
		SigningKey:          make([]byte, 32), // Wrong key
	}

	clientState := NewNegotiationState(clientConfig, RoleClient)
	offer, _ := clientState.CreateOffer()

	attackerState := NewNegotiationState(attackerConfig, RoleServer)
	response, _ := attackerState.ProcessOffer(offer)

	// Client should detect downgrade attack
	err := clientState.ProcessResponse(response)
	if err == nil {
		t.Error("Downgrade attack not detected")
	} else {
		t.Logf("Downgrade attack correctly detected: %v", err)
	}
}

// TestCipherSuites tests all cipher suites
func TestCipherSuites(t *testing.T) {
	testData := []byte("The quick brown fox jumps over the lazy dog")
	aad := []byte("additional authenticated data")

	suites := []CipherSuite{
		CipherSuiteChaCha20Poly1305,
		CipherSuiteAES256GCM,
		CipherSuiteXChaCha20Poly1305,
	}

	for _, suite := range suites {
		t.Run(suite.String(), func(t *testing.T) {
			info, err := GetCipherSuiteInfo(suite)
			if err != nil {
				t.Fatalf("Failed to get cipher info: %v", err)
			}

			key := make([]byte, info.KeySize)
			copy(key, []byte("test-key-1234567890abcdefghijklmnopqrstuvwxyz"))

			cs, err := NewCipherSuiteState(suite, key)
			if err != nil {
				t.Fatalf("Failed to create cipher state: %v", err)
			}

			nonce := make([]byte, info.NonceSize)
			copy(nonce, []byte("nonce123"))

			// Encrypt
			ciphertext, err := cs.Seal(nonce, testData, aad)
			if err != nil {
				t.Fatalf("Encryption failed: %v", err)
			}

			// Decrypt
			plaintext, err := cs.Open(nonce, ciphertext, aad)
			if err != nil {
				t.Fatalf("Decryption failed: %v", err)
			}

			// Verify
			if !bytes.Equal(plaintext, testData) {
				t.Errorf("Decrypted data doesn't match: got %q, want %q", plaintext, testData)
			}

			t.Logf("%s: OK (%d bytes plaintext -> %d bytes ciphertext)",
				info.Name, len(testData), len(ciphertext))
		})
	}
}

// BenchmarkNoiseHandshake benchmarks the Noise handshake
func BenchmarkNoiseHandshake(b *testing.B) {
	psk := make([]byte, 32)
	clientStatic, _ := GeneratePrivateKey()
	serverStatic, _ := GeneratePrivateKey()
	serverPub, _ := derivePublicKey(serverStatic)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		clientConn, serverConn := net.Pipe()

		go func() {
			opts := NoiseHandshakeOptions{
				Pattern:      NoiseIKpsk2,
				PreSharedKey: psk,
				StaticKey:    serverStatic,
				CipherSuites: []CipherSuite{CipherSuiteChaCha20Poly1305},
			}
			PerformNoiseHandshake(serverConn, RoleServer, opts)
			serverConn.Close()
		}()

		opts := NoiseHandshakeOptions{
			Pattern:      NoiseIKpsk2,
			PreSharedKey: psk,
			StaticKey:    clientStatic,
			RemoteStatic: serverPub[:],
			CipherSuites: []CipherSuite{CipherSuiteChaCha20Poly1305},
		}
		PerformNoiseHandshake(clientConn, RoleClient, opts)
		clientConn.Close()
	}
}

// BenchmarkObfuscation benchmarks obfuscation
func BenchmarkObfuscation(b *testing.B) {
	secrets := SessionSecrets{
		ObfuscationKey: make([]byte, 32),
	}
	config := DefaultObfsConfig()
	config.Mode = ObfsModeOBFS4
	config.Seed = secrets.ObfuscationKey

	obfs, _ := NewObfuscator(secrets, config)
	data := make([]byte, 1400) // MTU size

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		obfs.ObfuscateFrame(data)
	}
}

// BenchmarkEncryption benchmarks encryption
func BenchmarkEncryption(b *testing.B) {
	key := make([]byte, 32)
	plaintext := make([]byte, 1400)

	b.Run("ChaCha20-Poly1305", func(b *testing.B) {
		cipher, _ := NewCipherState(key)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cipher.Seal(uint64(i), nil, plaintext)
		}
	})

	b.Run("AES-256-GCM", func(b *testing.B) {
		cs, _ := NewCipherSuiteState(CipherSuiteAES256GCM, key)
		nonce := make([]byte, 12)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cs.Seal(nonce, plaintext, nil)
		}
	})
}

func (cs CipherSuite) String() string {
	info, _ := GetCipherSuiteInfo(cs)
	return info.Name
}
