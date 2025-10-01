package crypto

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	KeySize   = chacha20poly1305.KeySize
	NonceSize = chacha20poly1305.NonceSize
)

type CipherState struct {
	aead cipher.AEAD
}

func NewCipherState(key []byte) (*CipherState, error) {
	if len(key) < KeySize {
		return nil, errors.New("key too short for cipher state")
	}
	aead, err := chacha20poly1305.New(key[:KeySize])
	if err != nil {
		return nil, err
	}
	return &CipherState{aead: aead}, nil
}

func (c *CipherState) Seal(counter uint64, aad, plaintext []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, errors.New("cipher state not initialised")
	}
	nonce := make([]byte, NonceSize)
	binary.BigEndian.PutUint64(nonce[4:], counter)
	return c.aead.Seal(nil, nonce, plaintext, aad), nil
}

func (c *CipherState) Open(counter uint64, aad, ciphertext []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, errors.New("cipher state not initialised")
	}
	nonce := make([]byte, NonceSize)
	binary.BigEndian.PutUint64(nonce[4:], counter)
	return c.aead.Open(nil, nonce, ciphertext, aad)
}

func Encrypt(data []byte, key []byte) ([]byte, error) {
	if len(key) < KeySize {
		return nil, errors.New("encryption key too short")
	}
	aead, err := chacha20poly1305.New(key[:KeySize])
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := aead.Seal(nil, nonce, data, nil)
	out := make([]byte, len(nonce)+len(ciphertext))
	copy(out, nonce)
	copy(out[len(nonce):], ciphertext)
	return out, nil
}

func Decrypt(ciphertext []byte, key []byte) ([]byte, error) {
	if len(key) < KeySize {
		return nil, errors.New("decryption key too short")
	}
	if len(ciphertext) < NonceSize {
		return nil, errors.New("ciphertext too short")
	}
	aead, err := chacha20poly1305.New(key[:KeySize])
	if err != nil {
		return nil, err
	}
	nonce := ciphertext[:NonceSize]
	payload := ciphertext[NonceSize:]
	return aead.Open(nil, nonce, payload, nil)
}

func Obfuscate(data []byte, xorKey []byte) []byte {
	if len(xorKey) == 0 {
		return append([]byte(nil), data...)
	}
	out := make([]byte, len(data))
	for i := range data {
		out[i] = data[i] ^ xorKey[i%len(xorKey)]
	}
	return out
}
