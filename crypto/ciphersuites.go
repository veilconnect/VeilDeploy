package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// CipherSuite represents a cryptographic cipher suite
type CipherSuite uint16

const (
	// CipherSuiteChaCha20Poly1305 - Original cipher (default)
	CipherSuiteChaCha20Poly1305 CipherSuite = 0x0001

	// CipherSuiteAES256GCM - AES-256-GCM for hardware acceleration
	CipherSuiteAES256GCM CipherSuite = 0x0002

	// CipherSuiteXChaCha20Poly1305 - Extended nonce ChaCha20-Poly1305
	CipherSuiteXChaCha20Poly1305 CipherSuite = 0x0003
)

// CipherSuiteInfo contains metadata about a cipher suite
type CipherSuiteInfo struct {
	ID          CipherSuite
	Name        string
	KeySize     int
	NonceSize   int
	TagSize     int
	Description string
}

var supportedCipherSuites = map[CipherSuite]CipherSuiteInfo{
	CipherSuiteChaCha20Poly1305: {
		ID:          CipherSuiteChaCha20Poly1305,
		Name:        "ChaCha20-Poly1305",
		KeySize:     chacha20poly1305.KeySize,
		NonceSize:   chacha20poly1305.NonceSize,
		TagSize:     16,
		Description: "Fast software-based AEAD cipher",
	},
	CipherSuiteAES256GCM: {
		ID:          CipherSuiteAES256GCM,
		Name:        "AES-256-GCM",
		KeySize:     32,
		NonceSize:   12,
		TagSize:     16,
		Description: "Hardware-accelerated AES with GCM mode",
	},
	CipherSuiteXChaCha20Poly1305: {
		ID:          CipherSuiteXChaCha20Poly1305,
		Name:        "XChaCha20-Poly1305",
		KeySize:     chacha20poly1305.KeySize,
		NonceSize:   chacha20poly1305.NonceSizeX,
		TagSize:     16,
		Description: "Extended nonce ChaCha20-Poly1305",
	},
}

// GetCipherSuiteInfo returns information about a cipher suite
func GetCipherSuiteInfo(suite CipherSuite) (CipherSuiteInfo, error) {
	info, ok := supportedCipherSuites[suite]
	if !ok {
		return CipherSuiteInfo{}, fmt.Errorf("unsupported cipher suite: 0x%04x", suite)
	}
	return info, nil
}

// ListSupportedCipherSuites returns all supported cipher suites
func ListSupportedCipherSuites() []CipherSuiteInfo {
	suites := make([]CipherSuiteInfo, 0, len(supportedCipherSuites))
	for _, info := range supportedCipherSuites {
		suites = append(suites, info)
	}
	return suites
}

// NewAEAD creates an AEAD cipher for the specified suite
func NewAEAD(suite CipherSuite, key []byte) (cipher.AEAD, error) {
	info, err := GetCipherSuiteInfo(suite)
	if err != nil {
		return nil, err
	}

	if len(key) < info.KeySize {
		return nil, fmt.Errorf("key too short for %s: need %d bytes, got %d",
			info.Name, info.KeySize, len(key))
	}

	switch suite {
	case CipherSuiteChaCha20Poly1305:
		return chacha20poly1305.New(key[:info.KeySize])

	case CipherSuiteAES256GCM:
		block, err := aes.NewCipher(key[:info.KeySize])
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(block)

	case CipherSuiteXChaCha20Poly1305:
		return chacha20poly1305.NewX(key[:info.KeySize])

	default:
		return nil, errors.New("cipher suite not implemented")
	}
}

// CipherSuiteState wraps AEAD with suite information
type CipherSuiteState struct {
	suite CipherSuite
	info  CipherSuiteInfo
	aead  cipher.AEAD
}

// NewCipherSuiteState creates a new cipher state with the specified suite
func NewCipherSuiteState(suite CipherSuite, key []byte) (*CipherSuiteState, error) {
	info, err := GetCipherSuiteInfo(suite)
	if err != nil {
		return nil, err
	}

	aead, err := NewAEAD(suite, key)
	if err != nil {
		return nil, err
	}

	return &CipherSuiteState{
		suite: suite,
		info:  info,
		aead:  aead,
	}, nil
}

// Suite returns the cipher suite ID
func (cs *CipherSuiteState) Suite() CipherSuite {
	return cs.suite
}

// Info returns cipher suite information
func (cs *CipherSuiteState) Info() CipherSuiteInfo {
	return cs.info
}

// Seal encrypts and authenticates plaintext
func (cs *CipherSuiteState) Seal(nonce, plaintext, additionalData []byte) ([]byte, error) {
	if len(nonce) != cs.info.NonceSize {
		return nil, fmt.Errorf("invalid nonce size: expected %d, got %d",
			cs.info.NonceSize, len(nonce))
	}
	return cs.aead.Seal(nil, nonce, plaintext, additionalData), nil
}

// Open decrypts and verifies ciphertext
func (cs *CipherSuiteState) Open(nonce, ciphertext, additionalData []byte) ([]byte, error) {
	if len(nonce) != cs.info.NonceSize {
		return nil, fmt.Errorf("invalid nonce size: expected %d, got %d",
			cs.info.NonceSize, len(nonce))
	}
	return cs.aead.Open(nil, nonce, ciphertext, additionalData)
}
