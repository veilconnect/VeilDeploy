package crypto

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

// NoisePattern represents the Noise protocol pattern being used
type NoisePattern int

const (
	// NoiseIKpsk2 - IK pattern with PSK (pre-shared key) mixed in
	// Provides mutual authentication, identity hiding for responder, and PSK security
	NoiseIKpsk2 NoisePattern = iota

	// NoiseXXpsk3 - XX pattern with PSK for mutual identity hiding
	NoiseXXpsk3

	noiseVersion = 2 // Protocol version
)

// NoiseHandshakeState maintains state during the handshake
type NoiseHandshakeState struct {
	pattern        NoisePattern
	role           HandshakeRole
	staticPrivate  [32]byte
	staticPublic   [32]byte
	ephemeralPriv  [32]byte
	ephemeralPub   [32]byte
	remoteStatic   [32]byte
	remoteEphemeral [32]byte

	// Symmetric state
	chainingKey    [32]byte
	hash           [32]byte
	psk            []byte

	// Anti-downgrade
	minVersion     uint8
	maxVersion     uint8
	agreedVersion  uint8
}

// NoiseHandshakeOptions contains options for the Noise handshake
type NoiseHandshakeOptions struct {
	Pattern        NoisePattern
	PreSharedKey   []byte
	StaticKey      []byte          // Static long-term key
	RemoteStatic   []byte          // Known remote static key (for IK pattern)
	KeepAlive      time.Duration
	MaxPadding     uint8
	CookieTTL      time.Duration
	CipherSuites   []CipherSuite   // Supported cipher suites in preference order
	MinVersion     uint8           // Minimum acceptable protocol version
}

// NoiseHandshakeResult contains the result of a successful Noise handshake
type NoiseHandshakeResult struct {
	Secrets        SessionSecrets
	Parameters     TransportParameters
	RemoteStatic   [32]byte        // Peer's static public key
	Pattern        NoisePattern
	CipherSuite    CipherSuite
	Version        uint8
}

const (
	noiseProtocolName = "Noise_IKpsk2_25519_ChaChaPoly_SHA256"
	noiseHeaderSize   = 5
)

// PerformNoiseHandshake executes a Noise protocol handshake
func PerformNoiseHandshake(conn net.Conn, role HandshakeRole, opts NoiseHandshakeOptions) (*NoiseHandshakeResult, error) {
	if len(opts.PreSharedKey) == 0 {
		return nil, errors.New("pre-shared key required")
	}

	if opts.MinVersion == 0 {
		opts.MinVersion = 1
	}
	if len(opts.CipherSuites) == 0 {
		opts.CipherSuites = []CipherSuite{CipherSuiteChaCha20Poly1305}
	}

	switch opts.Pattern {
	case NoiseIKpsk2:
		return performNoiseIKpsk2(conn, role, opts)
	case NoiseXXpsk3:
		return performNoiseXXpsk3(conn, role, opts)
	default:
		return nil, fmt.Errorf("unsupported noise pattern: %d", opts.Pattern)
	}
}

// performNoiseIKpsk2 implements the Noise_IKpsk2_25519_ChaChaPoly_SHA256 handshake
func performNoiseIKpsk2(conn net.Conn, role HandshakeRole, opts NoiseHandshakeOptions) (*NoiseHandshakeResult, error) {
	hs := &NoiseHandshakeState{
		pattern:    NoiseIKpsk2,
		role:       role,
		psk:        opts.PreSharedKey,
		minVersion: opts.MinVersion,
		maxVersion: noiseVersion,
	}

	// Initialize static keypair
	if len(opts.StaticKey) == 32 {
		copy(hs.staticPrivate[:], opts.StaticKey)
		pub, err := derivePublicKey(hs.staticPrivate[:])
		if err != nil {
			return nil, err
		}
		hs.staticPublic = pub
	} else {
		priv, err := GeneratePrivateKey()
		if err != nil {
			return nil, err
		}
		copy(hs.staticPrivate[:], priv)
		pub, err := derivePublicKey(priv)
		if err != nil {
			return nil, err
		}
		hs.staticPublic = pub
	}

	// Initialize chaining key and hash
	initializeSymmetric(hs, noiseProtocolName)

	if role == RoleClient {
		return noiseIKpsk2Initiator(conn, hs, opts)
	}
	return noiseIKpsk2Responder(conn, hs, opts)
}

func initializeSymmetric(hs *NoiseHandshakeState, protocolName string) {
	if len(protocolName) <= 32 {
		copy(hs.hash[:], protocolName)
		for i := len(protocolName); i < 32; i++ {
			hs.hash[i] = 0
		}
	} else {
		h := sha256.Sum256([]byte(protocolName))
		hs.hash = h
	}
	hs.chainingKey = hs.hash
}

func mixHash(hs *NoiseHandshakeState, data []byte) {
	h := sha256.New()
	h.Write(hs.hash[:])
	h.Write(data)
	copy(hs.hash[:], h.Sum(nil))
}

func mixKey(hs *NoiseHandshakeState, inputKeyMaterial []byte) {
	// HKDF with chaining key as salt
	output := hkdf.Extract(sha256.New, inputKeyMaterial, hs.chainingKey[:])
	copy(hs.chainingKey[:], output)
}

func mixKeyAndHash(hs *NoiseHandshakeState, inputKeyMaterial []byte) {
	// Split output into three parts
	reader := hkdf.Expand(sha256.New, inputKeyMaterial, hs.chainingKey[:])
	temp := make([]byte, 32)
	io.ReadFull(reader, temp)

	mixHash(hs, inputKeyMaterial)
	copy(hs.chainingKey[:], temp)
}

func encryptAndHash(hs *NoiseHandshakeState, plaintext []byte) ([]byte, error) {
	// Derive encryption key from chaining key
	key := make([]byte, 32)
	reader := hkdf.Expand(sha256.New, hs.chainingKey[:], []byte("encryption"))
	io.ReadFull(reader, key)

	cipher, err := NewCipherState(key)
	if err != nil {
		return nil, err
	}

	// Use hash as nonce (truncated to 8 bytes counter)
	nonce := binary.BigEndian.Uint64(hs.hash[24:])
	ciphertext, err := cipher.Seal(nonce, hs.hash[:], plaintext)
	if err != nil {
		return nil, err
	}

	mixHash(hs, ciphertext)
	return ciphertext, nil
}

func decryptAndHash(hs *NoiseHandshakeState, ciphertext []byte) ([]byte, error) {
	key := make([]byte, 32)
	reader := hkdf.Expand(sha256.New, hs.chainingKey[:], []byte("encryption"))
	io.ReadFull(reader, key)

	cipher, err := NewCipherState(key)
	if err != nil {
		return nil, err
	}

	nonce := binary.BigEndian.Uint64(hs.hash[24:])
	plaintext, err := cipher.Open(nonce, hs.hash[:], ciphertext)
	if err != nil {
		return nil, err
	}

	mixHash(hs, ciphertext)
	return plaintext, nil
}

// noiseIKpsk2Initiator performs the initiator side of Noise_IKpsk2
func noiseIKpsk2Initiator(conn net.Conn, hs *NoiseHandshakeState, opts NoiseHandshakeOptions) (*NoiseHandshakeResult, error) {
	// Generate ephemeral keypair
	ephemPriv, err := GeneratePrivateKey()
	if err != nil {
		return nil, err
	}
	copy(hs.ephemeralPriv[:], ephemPriv)
	ephemPub, err := derivePublicKey(ephemPriv)
	if err != nil {
		return nil, err
	}
	hs.ephemeralPub = ephemPub

	// Check remote static key is provided
	if len(opts.RemoteStatic) != 32 {
		return nil, errors.New("remote static key required for IK pattern")
	}
	copy(hs.remoteStatic[:], opts.RemoteStatic)

	sessionID, err := randomSessionID()
	if err != nil {
		return nil, err
	}

	// Message 1: -> e, es, s, ss
	// Mix remote static public key
	mixHash(hs, hs.remoteStatic[:])

	msg1 := bytes.NewBuffer(nil)
	msg1.WriteByte(noiseVersion)                    // Protocol version
	msg1.WriteByte(uint8(opts.MinVersion))          // Min version
	msg1.WriteByte(uint8(len(opts.CipherSuites)))   // Number of cipher suites
	for _, suite := range opts.CipherSuites {
		binary.Write(msg1, binary.BigEndian, uint16(suite))
	}
	msg1.Write(sessionID[:])

	// e
	msg1.Write(hs.ephemeralPub[:])
	mixHash(hs, hs.ephemeralPub[:])

	// es
	dhResult, err := curve25519.X25519(hs.ephemeralPriv[:], hs.remoteStatic[:])
	if err != nil {
		return nil, err
	}
	mixKey(hs, dhResult)

	// Mix PSK
	mixKeyAndHash(hs, hs.psk)

	// s (encrypted)
	encStatic, err := encryptAndHash(hs, hs.staticPublic[:])
	if err != nil {
		return nil, err
	}
	msg1.Write(encStatic)

	// ss
	dhResult, err = curve25519.X25519(hs.staticPrivate[:], hs.remoteStatic[:])
	if err != nil {
		return nil, err
	}
	mixKey(hs, dhResult)

	// Add random padding
	padding, err := randomPadding(opts.MaxPadding)
	if err != nil {
		return nil, err
	}
	msg1.WriteByte(uint8(len(padding)))
	msg1.Write(padding)

	if err := writeRecord(conn, msg1.Bytes()); err != nil {
		return nil, err
	}

	// Message 2: <- e, ee, se, psk
	msg2, err := readRecord(conn)
	if err != nil {
		return nil, err
	}

	if len(msg2) < 1+1+1+2+32 {
		return nil, errors.New("message 2 too short")
	}

	offset := 0
	version := msg2[offset]
	offset++
	if version < opts.MinVersion || version > noiseVersion {
		return nil, fmt.Errorf("unsupported protocol version: %d", version)
	}
	hs.agreedVersion = version

	// Cipher suite negotiation
	suiteCount := int(msg2[offset])
	offset++
	if suiteCount == 0 {
		return nil, errors.New("no cipher suites offered")
	}

	selectedSuite := CipherSuiteChaCha20Poly1305
	found := false
	for i := 0; i < suiteCount; i++ {
		suite := CipherSuite(binary.BigEndian.Uint16(msg2[offset : offset+2]))
		offset += 2
		for _, s := range opts.CipherSuites {
			if s == suite {
				selectedSuite = suite
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	// e
	copy(hs.remoteEphemeral[:], msg2[offset:offset+32])
	offset += 32
	mixHash(hs, hs.remoteEphemeral[:])

	// ee
	dhResult, err = curve25519.X25519(hs.ephemeralPriv[:], hs.remoteEphemeral[:])
	if err != nil {
		return nil, err
	}
	mixKey(hs, dhResult)

	// se
	dhResult, err = curve25519.X25519(hs.staticPrivate[:], hs.remoteEphemeral[:])
	if err != nil {
		return nil, err
	}
	mixKey(hs, dhResult)

	// Mix PSK again
	mixKeyAndHash(hs, hs.psk)

	// Read transport parameters (encrypted)
	paramsLen := binary.BigEndian.Uint16(msg2[offset : offset+2])
	offset += 2
	encParams := msg2[offset : offset+int(paramsLen)]
	offset += int(paramsLen)

	paramsData, err := decryptAndHash(hs, encParams)
	if err != nil {
		return nil, errors.New("failed to decrypt transport parameters")
	}

	params, err := decodeTransportParams(paramsData)
	if err != nil {
		return nil, err
	}

	// Split for final keys
	sendKey, recvKey := splitKeys(hs, RoleClient)

	secrets := SessionSecrets{
		SessionID:      sessionID,
		SendKey:        sendKey,
		ReceiveKey:     recvKey,
		ObfuscationKey: deriveObfuscationKey(hs.chainingKey[:]),
		PeerPublicKey:  hs.remoteStatic,
		Epoch:          1,
		Established:    time.Now().UTC(),
	}

	return &NoiseHandshakeResult{
		Secrets:      secrets,
		Parameters:   params,
		RemoteStatic: hs.remoteStatic,
		Pattern:      NoiseIKpsk2,
		CipherSuite:  selectedSuite,
		Version:      hs.agreedVersion,
	}, nil
}

// noiseIKpsk2Responder performs the responder side of Noise_IKpsk2
func noiseIKpsk2Responder(conn net.Conn, hs *NoiseHandshakeState, opts NoiseHandshakeOptions) (*NoiseHandshakeResult, error) {
	// Message 1: -> e, es, s, ss
	msg1, err := readRecord(conn)
	if err != nil {
		return nil, err
	}

	if len(msg1) < 1+1+1+16+32 {
		return nil, errors.New("message 1 too short")
	}

	offset := 0
	version := msg1[offset]
	offset++

	minVersion := msg1[offset]
	offset++
	if noiseVersion < minVersion {
		return nil, fmt.Errorf("version too old: need >= %d", minVersion)
	}
	hs.agreedVersion = min(noiseVersion, version)

	// Parse cipher suites
	suiteCount := int(msg1[offset])
	offset++
	clientSuites := make([]CipherSuite, suiteCount)
	for i := 0; i < suiteCount; i++ {
		clientSuites[i] = CipherSuite(binary.BigEndian.Uint16(msg1[offset : offset+2]))
		offset += 2
	}

	// Select cipher suite (first match)
	selectedSuite := CipherSuiteChaCha20Poly1305
	found := false
	for _, cs := range clientSuites {
		for _, supported := range opts.CipherSuites {
			if cs == supported {
				selectedSuite = cs
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	var sessionID [16]byte
	copy(sessionID[:], msg1[offset:offset+16])
	offset += 16

	// Mix our static public key
	mixHash(hs, hs.staticPublic[:])

	// e
	copy(hs.remoteEphemeral[:], msg1[offset:offset+32])
	offset += 32
	mixHash(hs, hs.remoteEphemeral[:])

	// es
	dhResult, err := curve25519.X25519(hs.staticPrivate[:], hs.remoteEphemeral[:])
	if err != nil {
		return nil, err
	}
	mixKey(hs, dhResult)

	// Mix PSK
	mixKeyAndHash(hs, hs.psk)

	// s (encrypted)
	encStaticLen := 32 + 16 // public key + poly1305 tag
	if len(msg1) < offset+encStaticLen {
		return nil, errors.New("message 1 truncated")
	}
	encStatic := msg1[offset : offset+encStaticLen]
	offset += encStaticLen

	remoteStaticBytes, err := decryptAndHash(hs, encStatic)
	if err != nil {
		return nil, errors.New("failed to decrypt remote static key")
	}
	copy(hs.remoteStatic[:], remoteStaticBytes)

	// ss
	dhResult, err = curve25519.X25519(hs.staticPrivate[:], hs.remoteStatic[:])
	if err != nil {
		return nil, err
	}
	mixKey(hs, dhResult)

	// Message 2: <- e, ee, se, psk
	ephemPriv, err := GeneratePrivateKey()
	if err != nil {
		return nil, err
	}
	copy(hs.ephemeralPriv[:], ephemPriv)
	ephemPub, err := derivePublicKey(ephemPriv)
	if err != nil {
		return nil, err
	}
	hs.ephemeralPub = ephemPub

	msg2 := bytes.NewBuffer(nil)
	msg2.WriteByte(hs.agreedVersion)
	msg2.WriteByte(1) // 1 cipher suite selected
	binary.Write(msg2, binary.BigEndian, uint16(selectedSuite))

	// e
	msg2.Write(hs.ephemeralPub[:])
	mixHash(hs, hs.ephemeralPub[:])

	// ee
	dhResult, err = curve25519.X25519(hs.ephemeralPriv[:], hs.remoteEphemeral[:])
	if err != nil {
		return nil, err
	}
	mixKey(hs, dhResult)

	// se
	dhResult, err = curve25519.X25519(hs.ephemeralPriv[:], hs.remoteStatic[:])
	if err != nil {
		return nil, err
	}
	mixKey(hs, dhResult)

	// Mix PSK again
	mixKeyAndHash(hs, hs.psk)

	// Transport parameters
	params := TransportParameters{
		KeepAlive:  opts.KeepAlive,
		MaxPadding: opts.MaxPadding,
	}
	if params.KeepAlive == 0 {
		params.KeepAlive = 15 * time.Second
	}
	if params.MaxPadding == 0 {
		params.MaxPadding = 96
	}

	paramsData := encodeTransportParams(params)
	encParams, err := encryptAndHash(hs, paramsData)
	if err != nil {
		return nil, err
	}

	binary.Write(msg2, binary.BigEndian, uint16(len(encParams)))
	msg2.Write(encParams)

	if err := writeRecord(conn, msg2.Bytes()); err != nil {
		return nil, err
	}

	// Split for final keys
	sendKey, recvKey := splitKeys(hs, RoleServer)

	secrets := SessionSecrets{
		SessionID:      sessionID,
		SendKey:        sendKey,
		ReceiveKey:     recvKey,
		ObfuscationKey: deriveObfuscationKey(hs.chainingKey[:]),
		PeerPublicKey:  hs.remoteStatic,
		Epoch:          1,
		Established:    time.Now().UTC(),
	}

	return &NoiseHandshakeResult{
		Secrets:      secrets,
		Parameters:   params,
		RemoteStatic: hs.remoteStatic,
		Pattern:      NoiseIKpsk2,
		CipherSuite:  selectedSuite,
		Version:      hs.agreedVersion,
	}, nil
}

func splitKeys(hs *NoiseHandshakeState, role HandshakeRole) ([]byte, []byte) {
	// Derive two keys from chaining key
	reader := hkdf.Expand(sha256.New, hs.chainingKey[:], hs.hash[:])

	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	io.ReadFull(reader, key1)
	io.ReadFull(reader, key2)

	if role == RoleClient {
		return key1, key2
	}
	return key2, key1
}

func deriveObfuscationKey(chainingKey []byte) []byte {
	reader := hkdf.Expand(sha256.New, chainingKey, []byte("obfuscation"))
	key := make([]byte, 32)
	io.ReadFull(reader, key)
	return key
}

func encodeTransportParams(params TransportParameters) []byte {
	buf := bytes.NewBuffer(nil)
	binary.Write(buf, binary.BigEndian, uint16(params.KeepAlive/time.Millisecond))
	buf.WriteByte(params.MaxPadding)
	return buf.Bytes()
}

func decodeTransportParams(data []byte) (TransportParameters, error) {
	if len(data) < 3 {
		return TransportParameters{}, errors.New("transport params too short")
	}
	keepAlive := binary.BigEndian.Uint16(data[0:2])
	maxPad := data[2]
	return TransportParameters{
		KeepAlive:  time.Duration(keepAlive) * time.Millisecond,
		MaxPadding: maxPad,
	}, nil
}

// performNoiseXXpsk3 implements Noise_XXpsk3 for mutual identity hiding
func performNoiseXXpsk3(conn net.Conn, role HandshakeRole, opts NoiseHandshakeOptions) (*NoiseHandshakeResult, error) {
	// TODO: Implement XX pattern for cases where both parties don't know each other's static keys
	return nil, errors.New("Noise_XXpsk3 not yet implemented")
}

func min(a, b uint8) uint8 {
	if a < b {
		return a
	}
	return b
}
