package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"math"
	mrand "math/rand"
	"time"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/salsa20/salsa"
)

// ObfsMode represents the obfuscation mode
type ObfsMode int

const (
	// ObfsModeNone - No obfuscation
	ObfsModeNone ObfsMode = iota

	// ObfsModeXOR - Simple XOR obfuscation (legacy)
	ObfsModeXOR

	// ObfsModeOBFS4 - obfs4 polymorphic obfuscation
	ObfsModeOBFS4

	// ObfsModeTLS - TLS-like obfuscation
	ObfsModeTLS

	// ObfsModeRandom - Random padding and timing
	ObfsModeRandom
)

// ObfsConfig contains obfuscation configuration
type ObfsConfig struct {
	Mode ObfsMode

	// Seed for deterministic obfuscation
	Seed []byte

	// IAT (Inter-Arrival Time) mode for timing obfuscation
	IATMode      int // 0=off, 1=enabled, 2=paranoid
	IATMeanDelay time.Duration

	// Length obfuscation
	MinPadding uint16
	MaxPadding uint16

	// Protocol mimicry
	MimicProtocol string // "tls", "http", "ssh", etc.
}

// DefaultObfsConfig returns a secure default obfuscation config
func DefaultObfsConfig() ObfsConfig {
	return ObfsConfig{
		Mode:          ObfsModeOBFS4,
		IATMode:       1,
		IATMeanDelay:  10 * time.Millisecond,
		MinPadding:    0,
		MaxPadding:    1500,
		MimicProtocol: "tls",
	}
}

// Obfuscator provides polymorphic traffic obfuscation
type Obfuscator struct {
	mode   ObfsMode
	config ObfsConfig

	// Stream ciphers for obfuscation
	sendCipher cipher.Stream
	recvCipher cipher.Stream

	// Frame state
	sendNonce uint64
	recvNonce uint64

	// IAT obfuscation
	iatRng  *mrand.Rand
	iatDist *drbgDist

	// Length distribution
	lengthDist *drbgDist
}

// NewObfuscator creates a new obfuscator from session secrets
func NewObfuscator(secrets SessionSecrets, config ObfsConfig) (*Obfuscator, error) {
	o := &Obfuscator{
		mode:   config.Mode,
		config: config,
	}

	if config.Mode == ObfsModeNone {
		return o, nil
	}

	// Derive obfuscation keys
	sendKey, recvKey := deriveObfuscationKeys(secrets.ObfuscationKey, secrets.SessionID[:])

	var err error

	// Initialize stream ciphers based on mode
	switch config.Mode {
	case ObfsModeXOR:
		// Simple XOR (legacy mode)
		o.sendCipher = newXORCipher(sendKey)
		o.recvCipher = newXORCipher(recvKey)

	case ObfsModeOBFS4, ObfsModeRandom:
		// Use CTR-AES-256 for obfuscation
		o.sendCipher, err = newCTRCipher(sendKey)
		if err != nil {
			return nil, err
		}
		o.recvCipher, err = newCTRCipher(recvKey)
		if err != nil {
			return nil, err
		}

	case ObfsModeTLS:
		// TLS uses special framing
		o.sendCipher, err = newCTRCipher(sendKey)
		if err != nil {
			return nil, err
		}
		o.recvCipher, err = newCTRCipher(recvKey)
		if err != nil {
			return nil, err
		}
	}

	// Initialize IAT obfuscation
	if config.IATMode > 0 {
		seed := int64(binary.BigEndian.Uint64(secrets.SessionID[:8]))
		o.iatRng = mrand.New(mrand.NewSource(seed))
		o.iatDist = newDrbgDist(secrets.SessionID[:], config.IATMode)
	}

	// Initialize length distribution
	if config.MaxPadding > 0 {
		o.lengthDist = newDrbgDist(secrets.ObfuscationKey[:16], 1)
	}

	return o, nil
}

// ObfuscateFrame obfuscates an outgoing frame
func (o *Obfuscator) ObfuscateFrame(plaintext []byte) ([]byte, error) {
	if o.mode == ObfsModeNone {
		return plaintext, nil
	}

	// Add length obfuscation padding
	padded := plaintext
	if o.config.MaxPadding > 0 {
		padLen := o.samplePaddingLength()
		if padLen > 0 {
			padding := make([]byte, padLen)
			rand.Read(padding)

			// Encode with length prefix
			buf := make([]byte, 2+len(plaintext)+len(padding))
			binary.BigEndian.PutUint16(buf[0:2], uint16(len(plaintext)))
			copy(buf[2:], plaintext)
			copy(buf[2+len(plaintext):], padding)
			padded = buf
		}
	}

	// Apply stream cipher obfuscation
	obfuscated := make([]byte, len(padded))
	if o.sendCipher != nil {
		o.sendCipher.XORKeyStream(obfuscated, padded)
	} else {
		copy(obfuscated, padded)
	}

	// Add protocol-specific framing
	switch o.mode {
	case ObfsModeTLS:
		return o.wrapTLSFrame(obfuscated)
	case ObfsModeOBFS4:
		return o.wrapOBFS4Frame(obfuscated)
	default:
		return obfuscated, nil
	}
}

// DeobfuscateFrame deobfuscates an incoming frame
func (o *Obfuscator) DeobfuscateFrame(ciphertext []byte) ([]byte, error) {
	if o.mode == ObfsModeNone {
		return ciphertext, nil
	}

	// Remove protocol-specific framing
	var payload []byte
	var err error

	switch o.mode {
	case ObfsModeTLS:
		payload, err = o.unwrapTLSFrame(ciphertext)
	case ObfsModeOBFS4:
		payload, err = o.unwrapOBFS4Frame(ciphertext)
	default:
		payload = ciphertext
	}

	if err != nil {
		return nil, err
	}

	// Deobfuscate
	plaintext := make([]byte, len(payload))
	if o.recvCipher != nil {
		o.recvCipher.XORKeyStream(plaintext, payload)
	} else {
		copy(plaintext, payload)
	}

	// Remove padding if present
	if o.config.MaxPadding > 0 && len(plaintext) >= 2 {
		dataLen := binary.BigEndian.Uint16(plaintext[0:2])
		if int(dataLen)+2 <= len(plaintext) {
			return plaintext[2 : 2+dataLen], nil
		}
	}

	return plaintext, nil
}

// GetIATDelay returns the Inter-Arrival Time delay for timing obfuscation
func (o *Obfuscator) GetIATDelay() time.Duration {
	if o.config.IATMode == 0 || o.iatDist == nil {
		return 0
	}

	// Sample from distribution
	sample := o.iatDist.sample()

	// Scale to configured mean
	delay := time.Duration(float64(o.config.IATMeanDelay) * sample)

	// Clamp to reasonable bounds
	maxDelay := o.config.IATMeanDelay * 10
	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}

func (o *Obfuscator) samplePaddingLength() uint16 {
	if o.lengthDist == nil {
		return 0
	}

	sample := o.lengthDist.sample()
	padLen := uint16(sample * float64(o.config.MaxPadding-o.config.MinPadding))
	return o.config.MinPadding + padLen
}

// wrapTLSFrame wraps data in a TLS-like record
func (o *Obfuscator) wrapTLSFrame(data []byte) ([]byte, error) {
	// TLS record: [type:1][version:2][length:2][data]
	record := make([]byte, 5+len(data))
	record[0] = 0x17 // Application data
	record[1] = 0x03 // TLS 1.2
	record[2] = 0x03
	binary.BigEndian.PutUint16(record[3:5], uint16(len(data)))
	copy(record[5:], data)
	return record, nil
}

func (o *Obfuscator) unwrapTLSFrame(record []byte) ([]byte, error) {
	if len(record) < 5 {
		return nil, errors.New("TLS record too short")
	}
	dataLen := binary.BigEndian.Uint16(record[3:5])
	if len(record) < 5+int(dataLen) {
		return nil, errors.New("TLS record truncated")
	}
	return record[5 : 5+dataLen], nil
}

// wrapOBFS4Frame wraps data in an obfs4-style polymorphic frame
func (o *Obfuscator) wrapOBFS4Frame(data []byte) ([]byte, error) {
	// obfs4 frame: [length:2][type:1][data][mac:16]
	frameLen := 2 + 1 + len(data) + 16

	frame := make([]byte, frameLen)
	binary.BigEndian.PutUint16(frame[0:2], uint16(len(data)))
	frame[2] = 0x00 // Data frame type
	copy(frame[3:], data)

	// Compute MAC
	mac := hmac.New(sha256.New, o.config.Seed)
	nonceBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBuf, o.sendNonce)
	mac.Write(nonceBuf)
	mac.Write(frame[0 : frameLen-16])
	copy(frame[frameLen-16:], mac.Sum(nil)[:16])

	o.sendNonce++
	return frame, nil
}

func (o *Obfuscator) unwrapOBFS4Frame(frame []byte) ([]byte, error) {
	if len(frame) < 3+16 {
		return nil, errors.New("obfs4 frame too short")
	}

	dataLen := binary.BigEndian.Uint16(frame[0:2])
	if len(frame) < 3+int(dataLen)+16 {
		return nil, errors.New("obfs4 frame truncated")
	}

	// Verify MAC
	expectedMac := frame[len(frame)-16:]
	mac := hmac.New(sha256.New, o.config.Seed)
	nonceBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBuf, o.recvNonce)
	mac.Write(nonceBuf)
	mac.Write(frame[0 : len(frame)-16])
	computedMac := mac.Sum(nil)[:16]

	if !hmac.Equal(expectedMac, computedMac) {
		return nil, errors.New("obfs4 MAC verification failed")
	}

	o.recvNonce++
	return frame[3 : 3+dataLen], nil
}

func deriveObfuscationKeys(masterKey, sessionID []byte) (send, recv []byte) {
	// Derive independent send/recv keys
	reader := hkdf.New(sha256.New, masterKey, sessionID, []byte("obfuscation"))

	send = make([]byte, 32)
	recv = make([]byte, 32)
	io.ReadFull(reader, send)
	io.ReadFull(reader, recv)

	return send, recv
}

func newCTRCipher(key []byte) (cipher.Stream, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	iv := make([]byte, aes.BlockSize)
	// Derive IV from key
	h := sha256.Sum256(key)
	copy(iv, h[:aes.BlockSize])

	return cipher.NewCTR(block, iv), nil
}

// xorCipher implements a simple XOR stream cipher
type xorCipher struct {
	key []byte
	pos int
}

func newXORCipher(key []byte) cipher.Stream {
	return &xorCipher{key: key}
}

func (x *xorCipher) XORKeyStream(dst, src []byte) {
	for i := range src {
		dst[i] = src[i] ^ x.key[x.pos%len(x.key)]
		x.pos++
	}
}

// drbgDist implements a DRBG-based distribution for obfuscation
type drbgDist struct {
	key   [32]byte
	nonce [16]byte
	mode  int
}

func newDrbgDist(seed []byte, mode int) *drbgDist {
	d := &drbgDist{mode: mode}

	h := sha256.Sum256(seed)
	copy(d.key[:], h[:])

	h2 := sha256.Sum256(h[:])
	copy(d.nonce[:], h2[:16])

	return d
}

func (d *drbgDist) sample() float64 {
	// Generate random bytes using Salsa20
	var output [8]byte
	var input [16]byte
	copy(input[:], d.nonce[:])

	salsa.XORKeyStream(output[:], output[:], &input, &d.key)

	// Increment nonce
	for i := 0; i < 16; i++ {
		d.nonce[i]++
		if d.nonce[i] != 0 {
			break
		}
	}

	// Convert to float64 in [0, 1)
	val := binary.BigEndian.Uint64(output[:])
	return float64(val) / float64(math.MaxUint64)
}

// TrafficShaper shapes traffic to avoid detection
type TrafficShaper struct {
	obfs      *Obfuscator
	sendQueue chan []byte
	iatMode   int
}

// NewTrafficShaper creates a traffic shaper
func NewTrafficShaper(obfs *Obfuscator) *TrafficShaper {
	return &TrafficShaper{
		obfs:      obfs,
		sendQueue: make(chan []byte, 100),
		iatMode:   obfs.config.IATMode,
	}
}

// Shape applies traffic shaping to outgoing data
func (ts *TrafficShaper) Shape(data []byte) <-chan []byte {
	out := make(chan []byte, 1)

	go func() {
		defer close(out)

		// Apply IAT delay
		if ts.iatMode > 0 {
			delay := ts.obfs.GetIATDelay()
			if delay > 0 {
				time.Sleep(delay)
			}
		}

		// Obfuscate
		obfuscated, err := ts.obfs.ObfuscateFrame(data)
		if err == nil {
			out <- obfuscated
		}
	}()

	return out
}

// BatchShape processes multiple frames with traffic shaping
func (ts *TrafficShaper) BatchShape(frames [][]byte) <-chan []byte {
	out := make(chan []byte, len(frames))

	go func() {
		defer close(out)

		for _, frame := range frames {
			// Apply IAT delay between frames
			if ts.iatMode > 0 {
				delay := ts.obfs.GetIATDelay()
				if delay > 0 {
					time.Sleep(delay)
				}
			}

			obfuscated, err := ts.obfs.ObfuscateFrame(frame)
			if err == nil {
				out <- obfuscated
			}
		}
	}()

	return out
}
