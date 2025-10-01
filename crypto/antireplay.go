package crypto

import (
	"errors"
	"sync"
	"time"
)

// AntiReplayMode represents the anti-replay protection mode
type AntiReplayMode int

const (
	// AntiReplayNone - No anti-replay protection
	AntiReplayNone AntiReplayMode = iota

	// AntiReplaySimple - Simple counter-based protection
	AntiReplaySimple

	// AntiReplayWindow - Sliding window (like IPsec)
	AntiReplayWindow

	// AntiReplayBloom - Bloom filter for memory efficiency
	AntiReplayBloom
)

// AntiReplayConfig contains anti-replay configuration
type AntiReplayConfig struct {
	Mode AntiReplayMode

	// Window size for sliding window mode (default: 64)
	WindowSize uint64

	// Maximum age for replay detection (default: 60s)
	MaxAge time.Duration

	// Bloom filter size (for AntiReplayBloom mode)
	BloomSize uint32
	BloomHashCount int
}

// DefaultAntiReplayConfig returns a secure default configuration
func DefaultAntiReplayConfig() AntiReplayConfig {
	return AntiReplayConfig{
		Mode:       AntiReplayWindow,
		WindowSize: 64,
		MaxAge:     60 * time.Second,
	}
}

// AntiReplay provides replay attack protection
type AntiReplay struct {
	mu     sync.RWMutex
	mode   AntiReplayMode
	config AntiReplayConfig

	// For simple mode
	lastSeq uint64

	// For window mode
	windowBase uint64
	windowBits uint64 // Bitmap for window

	// For bloom filter mode
	bloom *bloomFilter

	// Timestamp tracking
	timestamps map[uint64]time.Time
	lastCleanup time.Time
}

// NewAntiReplay creates a new anti-replay filter
func NewAntiReplay(config AntiReplayConfig) *AntiReplay {
	ar := &AntiReplay{
		mode:   config.Mode,
		config: config,
	}

	if config.WindowSize == 0 {
		ar.config.WindowSize = 64
	}
	if config.MaxAge == 0 {
		ar.config.MaxAge = 60 * time.Second
	}

	switch config.Mode {
	case AntiReplayBloom:
		size := config.BloomSize
		if size == 0 {
			size = 65536 // 64KB default
		}
		hashCount := config.BloomHashCount
		if hashCount == 0 {
			hashCount = 4
		}
		ar.bloom = newBloomFilter(size, hashCount)

	case AntiReplayWindow:
		ar.timestamps = make(map[uint64]time.Time)
	}

	return ar
}

// Check validates a sequence number against replay attacks
func (ar *AntiReplay) Check(seq uint64) error {
	if ar.mode == AntiReplayNone {
		return nil
	}

	ar.mu.Lock()
	defer ar.mu.Unlock()

	now := time.Now()

	// Periodic cleanup
	if ar.config.MaxAge > 0 && now.Sub(ar.lastCleanup) > ar.config.MaxAge {
		ar.cleanup(now)
		ar.lastCleanup = now
	}

	switch ar.mode {
	case AntiReplaySimple:
		return ar.checkSimple(seq)

	case AntiReplayWindow:
		return ar.checkWindow(seq, now)

	case AntiReplayBloom:
		return ar.checkBloom(seq)

	default:
		return nil
	}
}

// Accept marks a sequence number as seen
func (ar *AntiReplay) Accept(seq uint64) {
	if ar.mode == AntiReplayNone {
		return
	}

	ar.mu.Lock()
	defer ar.mu.Unlock()

	switch ar.mode {
	case AntiReplaySimple:
		if seq > ar.lastSeq {
			ar.lastSeq = seq
		}

	case AntiReplayWindow:
		ar.acceptWindow(seq)

	case AntiReplayBloom:
		ar.bloom.add(seq)
	}
}

func (ar *AntiReplay) checkSimple(seq uint64) error {
	if seq <= ar.lastSeq {
		return errors.New("replay detected: sequence number too old")
	}
	return nil
}

func (ar *AntiReplay) checkWindow(seq uint64, now time.Time) error {
	// Check timestamp if available
	if ar.config.MaxAge > 0 {
		if ts, exists := ar.timestamps[seq]; exists {
			if now.Sub(ts) > ar.config.MaxAge {
				return errors.New("replay detected: packet too old")
			}
			return errors.New("replay detected: duplicate sequence number")
		}
	}

	// Too old - outside window
	if seq+ar.config.WindowSize < ar.windowBase {
		return errors.New("replay detected: sequence number too old")
	}

	// Future packet - accept and advance window
	if seq > ar.windowBase {
		return nil
	}

	// Within window - check bitmap
	diff := ar.windowBase - seq
	if diff < ar.config.WindowSize {
		bit := uint64(1) << diff
		if (ar.windowBits & bit) != 0 {
			return errors.New("replay detected: duplicate in window")
		}
	}

	return nil
}

func (ar *AntiReplay) acceptWindow(seq uint64) {
	// Update timestamp
	if ar.timestamps != nil {
		ar.timestamps[seq] = time.Now()
	}

	// Sequence number is newer than window base
	if seq > ar.windowBase {
		// Shift window forward
		diff := seq - ar.windowBase
		if diff < ar.config.WindowSize {
			ar.windowBits = ar.windowBits << diff
		} else {
			ar.windowBits = 0
		}
		ar.windowBase = seq
		ar.windowBits |= 1 // Mark current seq as seen
	} else {
		// Set bit in window
		diff := ar.windowBase - seq
		if diff < ar.config.WindowSize {
			ar.windowBits |= (uint64(1) << diff)
		}
	}
}

func (ar *AntiReplay) checkBloom(seq uint64) error {
	if ar.bloom.contains(seq) {
		return errors.New("replay detected: bloom filter match")
	}
	return nil
}

func (ar *AntiReplay) cleanup(now time.Time) {
	if ar.timestamps == nil {
		return
	}

	// Remove old timestamps
	for seq, ts := range ar.timestamps {
		if now.Sub(ts) > ar.config.MaxAge {
			delete(ar.timestamps, seq)
		}
	}
}

// Reset clears the anti-replay state
func (ar *AntiReplay) Reset() {
	ar.mu.Lock()
	defer ar.mu.Unlock()

	ar.lastSeq = 0
	ar.windowBase = 0
	ar.windowBits = 0

	if ar.bloom != nil {
		ar.bloom.reset()
	}

	if ar.timestamps != nil {
		ar.timestamps = make(map[uint64]time.Time)
	}
}

// Stats returns anti-replay statistics
func (ar *AntiReplay) Stats() AntiReplayStats {
	ar.mu.RLock()
	defer ar.mu.RUnlock()

	stats := AntiReplayStats{
		Mode:       ar.mode,
		WindowBase: ar.windowBase,
	}

	if ar.bloom != nil {
		stats.BloomFillRate = ar.bloom.fillRate()
	}

	return stats
}

// AntiReplayStats contains anti-replay statistics
type AntiReplayStats struct {
	Mode          AntiReplayMode
	WindowBase    uint64
	BloomFillRate float64
}

// bloomFilter implements a simple counting bloom filter
type bloomFilter struct {
	bits      []byte
	size      uint32
	hashCount int
	count     uint64
}

func newBloomFilter(size uint32, hashCount int) *bloomFilter {
	return &bloomFilter{
		bits:      make([]byte, size),
		size:      size,
		hashCount: hashCount,
	}
}

func (bf *bloomFilter) add(seq uint64) {
	for i := 0; i < bf.hashCount; i++ {
		h := bf.hash(seq, uint32(i))
		idx := h / 8
		bit := byte(1) << (h % 8)

		if (bf.bits[idx] & bit) == 0 {
			bf.count++
		}
		bf.bits[idx] |= bit
	}
}

func (bf *bloomFilter) contains(seq uint64) bool {
	for i := 0; i < bf.hashCount; i++ {
		h := bf.hash(seq, uint32(i))
		idx := h / 8
		bit := byte(1) << (h % 8)

		if (bf.bits[idx] & bit) == 0 {
			return false
		}
	}
	return true
}

func (bf *bloomFilter) hash(seq uint64, seed uint32) uint32 {
	// Simple hash function
	h := uint32(seq ^ uint64(seed))
	h = ((h >> 16) ^ h) * 0x45d9f3b
	h = ((h >> 16) ^ h) * 0x45d9f3b
	h = (h >> 16) ^ h
	return h % (bf.size * 8)
}

func (bf *bloomFilter) fillRate() float64 {
	return float64(bf.count) / float64(bf.size*8)
}

func (bf *bloomFilter) reset() {
	for i := range bf.bits {
		bf.bits[i] = 0
	}
	bf.count = 0
}

// TimestampValidator validates message timestamps to prevent replay
type TimestampValidator struct {
	mu sync.RWMutex

	maxClockSkew time.Duration
	seenNonces   map[uint64]time.Time
	maxAge       time.Duration
}

// NewTimestampValidator creates a new timestamp validator
func NewTimestampValidator(maxClockSkew, maxAge time.Duration) *TimestampValidator {
	return &TimestampValidator{
		maxClockSkew: maxClockSkew,
		maxAge:       maxAge,
		seenNonces:   make(map[uint64]time.Time),
	}
}

// Validate checks if a timestamp is valid and not replayed
func (tv *TimestampValidator) Validate(timestamp time.Time, nonce uint64) error {
	tv.mu.Lock()
	defer tv.mu.Unlock()

	now := time.Now()

	// Check timestamp is within acceptable range
	if timestamp.After(now.Add(tv.maxClockSkew)) {
		return errors.New("timestamp too far in future")
	}

	if now.Sub(timestamp) > tv.maxAge {
		return errors.New("timestamp too old")
	}

	// Check for nonce reuse
	if seen, exists := tv.seenNonces[nonce]; exists {
		if timestamp.Equal(seen) || timestamp.Before(seen) {
			return errors.New("replay detected: nonce reused")
		}
	}

	// Record nonce
	tv.seenNonces[nonce] = timestamp

	// Cleanup old nonces
	tv.cleanup(now)

	return nil
}

func (tv *TimestampValidator) cleanup(now time.Time) {
	for nonce, ts := range tv.seenNonces {
		if now.Sub(ts) > tv.maxAge {
			delete(tv.seenNonces, nonce)
		}
	}
}

// CombinedAntiReplay combines multiple anti-replay mechanisms
type CombinedAntiReplay struct {
	sequence  *AntiReplay
	timestamp *TimestampValidator
}

// NewCombinedAntiReplay creates a combined anti-replay filter
func NewCombinedAntiReplay(config AntiReplayConfig) *CombinedAntiReplay {
	return &CombinedAntiReplay{
		sequence:  NewAntiReplay(config),
		timestamp: NewTimestampValidator(5*time.Second, config.MaxAge),
	}
}

// Check validates both sequence number and timestamp
func (car *CombinedAntiReplay) Check(seq uint64, timestamp time.Time, nonce uint64) error {
	// Check sequence number
	if err := car.sequence.Check(seq); err != nil {
		return err
	}

	// Check timestamp
	if err := car.timestamp.Validate(timestamp, nonce); err != nil {
		return err
	}

	return nil
}

// Accept marks a message as accepted
func (car *CombinedAntiReplay) Accept(seq uint64) {
	car.sequence.Accept(seq)
}
