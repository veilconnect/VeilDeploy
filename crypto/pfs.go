package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

// PFSConfig contains configuration for Perfect Forward Secrecy
type PFSConfig struct {
	// RekeyInterval is the time between automatic rekeys (0 to disable)
	RekeyInterval time.Duration

	// RekeyAfterMessages triggers rekey after N messages (0 to disable)
	RekeyAfterMessages uint64

	// RekeyAfterBytes triggers rekey after N bytes (0 to disable)
	RekeyAfterBytes uint64

	// MaxEpochAge is the maximum age of a key epoch before forced rekey
	MaxEpochAge time.Duration
}

// DefaultPFSConfig returns a secure default configuration
func DefaultPFSConfig() PFSConfig {
	return PFSConfig{
		RekeyInterval:      5 * time.Minute,
		RekeyAfterMessages: 100000,
		RekeyAfterBytes:    1 << 30, // 1 GB
		MaxEpochAge:        15 * time.Minute,
	}
}

// PFSManager manages perfect forward secrecy and automatic rekeying
type PFSManager struct {
	mu sync.RWMutex

	// Current secrets
	current SessionSecrets

	// Previous secrets for decryption during transition
	previous *SessionSecrets

	// Statistics
	messagesSent     uint64
	messagesReceived uint64
	bytesSent        uint64
	bytesReceived    uint64
	lastRekey        time.Time

	// Configuration
	config PFSConfig

	// Pending rekey
	pendingRekey *RekeyContext

	// Role
	role HandshakeRole
}

// NewPFSManager creates a new PFS manager with initial secrets
func NewPFSManager(secrets SessionSecrets, role HandshakeRole, config PFSConfig) *PFSManager {
	return &PFSManager{
		current:   secrets,
		lastRekey: secrets.Established,
		config:    config,
		role:      role,
	}
}

// NeedsRekey checks if rekeying is needed based on policy
func (p *PFSManager) NeedsRekey() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Time-based rekey
	if p.config.RekeyInterval > 0 {
		if time.Since(p.lastRekey) >= p.config.RekeyInterval {
			return true
		}
	}

	// Message count rekey
	if p.config.RekeyAfterMessages > 0 {
		total := p.messagesSent + p.messagesReceived
		if total >= p.config.RekeyAfterMessages {
			return true
		}
	}

	// Byte count rekey
	if p.config.RekeyAfterBytes > 0 {
		total := p.bytesSent + p.bytesReceived
		if total >= p.config.RekeyAfterBytes {
			return true
		}
	}

	// Max epoch age (hard limit)
	if p.config.MaxEpochAge > 0 {
		if time.Since(p.lastRekey) >= p.config.MaxEpochAge {
			return true
		}
	}

	return false
}

// RecordSent updates statistics for sent data
func (p *PFSManager) RecordSent(messages, bytes uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messagesSent += messages
	p.bytesSent += bytes
}

// RecordReceived updates statistics for received data
func (p *PFSManager) RecordReceived(messages, bytes uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messagesReceived += messages
	p.bytesReceived += bytes
}

// GetSecrets returns the current session secrets (thread-safe)
func (p *PFSManager) GetSecrets() SessionSecrets {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.current
}

// GetSecretsForEpoch returns secrets for a specific epoch
func (p *PFSManager) GetSecretsForEpoch(epoch uint32) *SessionSecrets {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.current.Epoch == epoch {
		return &p.current
	}
	if p.previous != nil && p.previous.Epoch == epoch {
		return p.previous
	}
	return nil
}

// InitiateRekey starts a new rekey operation
func (p *PFSManager) InitiateRekey() (*RekeyContext, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.pendingRekey != nil {
		return nil, errors.New("rekey already in progress")
	}

	ctx, err := NewRekeyRequest(p.current, p.role)
	if err != nil {
		return nil, err
	}

	p.pendingRekey = ctx
	return ctx, nil
}

// ProcessRekeyMessage processes an incoming rekey message
func (p *PFSManager) ProcessRekeyMessage(payload []byte) (*SessionSecrets, []byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	newSecrets, response, err := ProcessRekey(p.current, payload, p.pendingRekey, p.role)
	if err != nil && err != ErrRekeyResponseRequired {
		return nil, nil, err
	}

	if newSecrets != nil {
		p.commitRekey(newSecrets)
		p.pendingRekey = nil
	}

	return newSecrets, response, err
}

// commitRekey atomically updates secrets after successful rekey
func (p *PFSManager) commitRekey(newSecrets *SessionSecrets) {
	// Keep previous for a grace period
	prev := p.current
	p.previous = &prev

	p.current = *newSecrets
	p.lastRekey = time.Now()

	// Reset counters
	p.messagesSent = 0
	p.messagesReceived = 0
	p.bytesSent = 0
	p.bytesReceived = 0

	// Schedule cleanup of old secrets after grace period
	go func(old *SessionSecrets) {
		time.Sleep(30 * time.Second)
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.previous != nil && p.previous.Epoch == old.Epoch {
			// Zero out old keys
			for i := range p.previous.SendKey {
				p.previous.SendKey[i] = 0
			}
			for i := range p.previous.ReceiveKey {
				p.previous.ReceiveKey[i] = 0
			}
			p.previous = nil
		}
	}(&prev)
}

// Stats returns current PFS statistics
func (p *PFSManager) Stats() PFSStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return PFSStats{
		CurrentEpoch:     p.current.Epoch,
		LastRekey:        p.lastRekey,
		MessagesSent:     p.messagesSent,
		MessagesReceived: p.messagesReceived,
		BytesSent:        p.bytesSent,
		BytesReceived:    p.bytesReceived,
		TimeUntilRekey:   p.timeUntilRekey(),
	}
}

func (p *PFSManager) timeUntilRekey() time.Duration {
	if p.config.RekeyInterval == 0 {
		return 0
	}
	elapsed := time.Since(p.lastRekey)
	if elapsed >= p.config.RekeyInterval {
		return 0
	}
	return p.config.RekeyInterval - elapsed
}

// PFSStats contains PFS statistics
type PFSStats struct {
	CurrentEpoch     uint32
	LastRekey        time.Time
	MessagesSent     uint64
	MessagesReceived uint64
	BytesSent        uint64
	BytesReceived    uint64
	TimeUntilRekey   time.Duration
}

// DoubleRatchet implements the Double Ratchet algorithm for continuous key rotation
type DoubleRatchet struct {
	mu sync.RWMutex

	// DH ratchet state
	dhPrivate [32]byte
	dhPublic  [32]byte
	dhRemote  [32]byte

	// Symmetric ratchets
	rootKey       [32]byte
	sendChainKey  [32]byte
	recvChainKey  [32]byte
	sendChainN    uint32
	recvChainN    uint32
	previousN     uint32

	// Skipped message keys for out-of-order messages
	skippedKeys map[uint32][32]byte
	maxSkip     uint32

	role HandshakeRole
}

// NewDoubleRatchet initializes a Double Ratchet from handshake secrets
func NewDoubleRatchet(secrets SessionSecrets, role HandshakeRole) (*DoubleRatchet, error) {
	dr := &DoubleRatchet{
		skippedKeys: make(map[uint32][32]byte),
		maxSkip:     1000, // Maximum messages to skip
		role:        role,
	}

	// Initialize root key from handshake
	copy(dr.rootKey[:], secrets.SendKey[:32])

	// Initialize chain keys
	if role == RoleClient {
		copy(dr.sendChainKey[:], secrets.SendKey)
		copy(dr.recvChainKey[:], secrets.ReceiveKey)
	} else {
		copy(dr.sendChainKey[:], secrets.ReceiveKey)
		copy(dr.recvChainKey[:], secrets.SendKey)
	}

	// Generate initial DH keypair
	priv, err := GeneratePrivateKey()
	if err != nil {
		return nil, err
	}
	copy(dr.dhPrivate[:], priv)

	pub, err := derivePublicKey(priv)
	if err != nil {
		return nil, err
	}
	dr.dhPublic = pub

	copy(dr.dhRemote[:], secrets.PeerPublicKey[:])

	return dr, nil
}

// RatchetEncrypt encrypts a message and advances the sending ratchet
func (dr *DoubleRatchet) RatchetEncrypt(plaintext []byte, aad []byte) (header []byte, ciphertext []byte, err error) {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	// Derive message key
	messageKey := dr.kdfCK(dr.sendChainKey[:])

	// Advance chain
	dr.sendChainKey = dr.kdfCK(dr.sendChainKey[:])
	n := dr.sendChainN
	dr.sendChainN++

	// Encrypt
	cipher, err := NewCipherState(messageKey[:])
	if err != nil {
		return nil, nil, err
	}

	ct, err := cipher.Seal(uint64(n), aad, plaintext)
	if err != nil {
		return nil, nil, err
	}

	// Build header
	header = dr.encodeHeader(dr.dhPublic, dr.previousN, n)

	return header, ct, nil
}

// RatchetDecrypt decrypts a message and advances the receiving ratchet
func (dr *DoubleRatchet) RatchetDecrypt(header, ciphertext, aad []byte) ([]byte, error) {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	dhRemote, _, n, err := dr.decodeHeader(header)
	if err != nil {
		return nil, err
	}

	// Check if we need to perform DH ratchet
	if dhRemote != dr.dhRemote {
		if err := dr.dhRatchet(dhRemote); err != nil {
			return nil, err
		}
	}

	// Try skipped message keys first
	if key, ok := dr.skippedKeys[n]; ok {
		delete(dr.skippedKeys, n)
		return dr.tryDecrypt(key[:], n, ciphertext, aad)
	}

	// Skip messages if needed
	if n > dr.recvChainN {
		if n-dr.recvChainN > dr.maxSkip {
			return nil, errors.New("too many skipped messages")
		}
		for dr.recvChainN < n {
			key := dr.kdfCK(dr.recvChainKey[:])
			dr.skippedKeys[dr.recvChainN] = key
			dr.recvChainKey = dr.kdfCK(dr.recvChainKey[:])
			dr.recvChainN++
		}
	}

	// Derive message key
	messageKey := dr.kdfCK(dr.recvChainKey[:])
	dr.recvChainKey = dr.kdfCK(dr.recvChainKey[:])
	dr.recvChainN++

	return dr.tryDecrypt(messageKey[:], n, ciphertext, aad)
}

func (dr *DoubleRatchet) dhRatchet(remotePublic [32]byte) error {
	// Save previous chain length
	dr.previousN = dr.sendChainN

	// Perform DH
	dhOutput, err := curve25519.X25519(dr.dhPrivate[:], remotePublic[:])
	if err != nil {
		return err
	}

	// KDF root key
	reader := hkdf.New(sha256.New, dhOutput, dr.rootKey[:], []byte("ratchet"))
	newRoot := make([]byte, 32)
	newChain := make([]byte, 32)
	io.ReadFull(reader, newRoot)
	io.ReadFull(reader, newChain)

	copy(dr.rootKey[:], newRoot)
	copy(dr.recvChainKey[:], newChain)
	dr.recvChainN = 0
	dr.dhRemote = remotePublic

	// Generate new DH keypair
	newPriv, err := GeneratePrivateKey()
	if err != nil {
		return err
	}
	copy(dr.dhPrivate[:], newPriv)

	newPub, err := derivePublicKey(newPriv)
	if err != nil {
		return err
	}
	dr.dhPublic = newPub

	// Second DH ratchet
	dhOutput, err = curve25519.X25519(dr.dhPrivate[:], dr.dhRemote[:])
	if err != nil {
		return err
	}

	reader = hkdf.New(sha256.New, dhOutput, dr.rootKey[:], []byte("ratchet"))
	io.ReadFull(reader, newRoot)
	io.ReadFull(reader, newChain)

	copy(dr.rootKey[:], newRoot)
	copy(dr.sendChainKey[:], newChain)
	dr.sendChainN = 0

	return nil
}

func (dr *DoubleRatchet) kdfCK(chainKey []byte) [32]byte {
	mac := hmac.New(sha256.New, chainKey)
	mac.Write([]byte{0x01})
	var key [32]byte
	copy(key[:], mac.Sum(nil))
	return key
}

func (dr *DoubleRatchet) encodeHeader(dhPub [32]byte, pn, n uint32) []byte {
	header := make([]byte, 32+4+4)
	copy(header[0:32], dhPub[:])
	binary.BigEndian.PutUint32(header[32:36], pn)
	binary.BigEndian.PutUint32(header[36:40], n)
	return header
}

func (dr *DoubleRatchet) decodeHeader(header []byte) (dhPub [32]byte, _ uint32, n uint32, err error) {
	if len(header) < 40 {
		return [32]byte{}, 0, 0, errors.New("header too short")
	}
	copy(dhPub[:], header[0:32])
	_ = binary.BigEndian.Uint32(header[32:36]) // pn (previous N) - not used in current impl
	n = binary.BigEndian.Uint32(header[36:40])
	return dhPub, 0, n, nil
}

func (dr *DoubleRatchet) tryDecrypt(key []byte, n uint32, ciphertext, aad []byte) ([]byte, error) {
	cipher, err := NewCipherState(key)
	if err != nil {
		return nil, err
	}
	return cipher.Open(uint64(n), aad, ciphertext)
}

// CleanupSkippedKeys removes old skipped keys (call periodically)
func (dr *DoubleRatchet) CleanupSkippedKeys(maxAge uint32) {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	for n := range dr.skippedKeys {
		if dr.recvChainN-n > maxAge {
			// Zero out before delete
			key := dr.skippedKeys[n]
			for i := range key {
				key[i] = 0
			}
			delete(dr.skippedKeys, n)
		}
	}
}
