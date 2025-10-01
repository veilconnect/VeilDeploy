package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// ProtocolVersion represents the protocol version
type ProtocolVersion uint8

const (
	// Protocol versions
	ProtocolVersionLegacy ProtocolVersion = 1 // Original protocol
	ProtocolVersionNoise  ProtocolVersion = 2 // Noise-based protocol
	ProtocolVersionCurrent = ProtocolVersionNoise

	// Minimum supported version
	ProtocolVersionMin = ProtocolVersionLegacy
)

// SecurityProfile represents a security profile combining multiple options
type SecurityProfile int

const (
	// SecurityProfileLegacy - Backward compatibility mode
	SecurityProfileLegacy SecurityProfile = iota

	// SecurityProfileBalanced - Good security/performance balance
	SecurityProfileBalanced

	// SecurityProfileStrict - Maximum security
	SecurityProfileStrict

	// SecurityProfileParanoid - Ultra-secure settings
	SecurityProfileParanoid
)

// NegotiationConfig contains protocol negotiation parameters
type NegotiationConfig struct {
	// Version constraints
	MinVersion ProtocolVersion
	MaxVersion ProtocolVersion

	// Cipher suite preferences (in order of preference)
	CipherSuites []CipherSuite

	// Security features
	RequirePFS       bool // Require perfect forward secrecy
	RequireAntiReplay bool // Require anti-replay protection
	RequireObfuscation bool // Require traffic obfuscation

	// Anti-downgrade protection
	DowngradeProtection bool
	SigningKey          []byte // Key for signing negotiation transcript
}

// GetSecurityProfile returns a predefined security profile
func GetSecurityProfile(profile SecurityProfile) NegotiationConfig {
	switch profile {
	case SecurityProfileLegacy:
		return NegotiationConfig{
			MinVersion:          ProtocolVersionLegacy,
			MaxVersion:          ProtocolVersionCurrent,
			CipherSuites:        []CipherSuite{CipherSuiteChaCha20Poly1305},
			RequirePFS:          false,
			RequireAntiReplay:   false,
			RequireObfuscation:  false,
			DowngradeProtection: false,
		}

	case SecurityProfileBalanced:
		return NegotiationConfig{
			MinVersion: ProtocolVersionNoise,
			MaxVersion: ProtocolVersionCurrent,
			CipherSuites: []CipherSuite{
				CipherSuiteChaCha20Poly1305,
				CipherSuiteAES256GCM,
			},
			RequirePFS:          true,
			RequireAntiReplay:   true,
			RequireObfuscation:  false,
			DowngradeProtection: true,
		}

	case SecurityProfileStrict:
		return NegotiationConfig{
			MinVersion: ProtocolVersionNoise,
			MaxVersion: ProtocolVersionCurrent,
			CipherSuites: []CipherSuite{
				CipherSuiteXChaCha20Poly1305,
				CipherSuiteChaCha20Poly1305,
			},
			RequirePFS:          true,
			RequireAntiReplay:   true,
			RequireObfuscation:  true,
			DowngradeProtection: true,
		}

	case SecurityProfileParanoid:
		return NegotiationConfig{
			MinVersion:          ProtocolVersionCurrent,
			MaxVersion:          ProtocolVersionCurrent,
			CipherSuites:        []CipherSuite{CipherSuiteXChaCha20Poly1305},
			RequirePFS:          true,
			RequireAntiReplay:   true,
			RequireObfuscation:  true,
			DowngradeProtection: true,
		}

	default:
		return GetSecurityProfile(SecurityProfileBalanced)
	}
}

// NegotiationState tracks the state of protocol negotiation
type NegotiationState struct {
	config NegotiationConfig
	role   HandshakeRole

	// Negotiated parameters
	agreedVersion   ProtocolVersion
	agreedCipher    CipherSuite
	agreedFeatures  FeatureFlags

	// Transcript for downgrade protection
	transcript []byte
}

// FeatureFlags represents negotiated features
type FeatureFlags uint32

const (
	FeaturePFS          FeatureFlags = 1 << 0
	FeatureAntiReplay   FeatureFlags = 1 << 1
	FeatureObfuscation  FeatureFlags = 1 << 2
	FeatureRekeying     FeatureFlags = 1 << 3
	FeatureCompression  FeatureFlags = 1 << 4
	FeatureDoubleRatchet FeatureFlags = 1 << 5
)

// NewNegotiationState creates a new negotiation state
func NewNegotiationState(config NegotiationConfig, role HandshakeRole) *NegotiationState {
	return &NegotiationState{
		config: config,
		role:   role,
	}
}

// CreateOffer creates a negotiation offer (client hello)
func (ns *NegotiationState) CreateOffer() ([]byte, error) {
	offer := &NegotiationOffer{
		MinVersion: ns.config.MinVersion,
		MaxVersion: ns.config.MaxVersion,
		CipherSuites: ns.config.CipherSuites,
		Features: ns.getOfferedFeatures(),
	}

	encoded := encodeOffer(offer)
	ns.transcript = append(ns.transcript, encoded...)

	return encoded, nil
}

// ProcessOffer processes a negotiation offer and creates a response
func (ns *NegotiationState) ProcessOffer(offerData []byte) ([]byte, error) {
	ns.transcript = append(ns.transcript, offerData...)

	offer, err := decodeOffer(offerData)
	if err != nil {
		return nil, err
	}

	// Validate version range
	if offer.MaxVersion < ns.config.MinVersion {
		return nil, fmt.Errorf("incompatible protocol versions: client max %d < server min %d",
			offer.MaxVersion, ns.config.MinVersion)
	}
	if offer.MinVersion > ns.config.MaxVersion {
		return nil, fmt.Errorf("incompatible protocol versions: client min %d > server max %d",
			offer.MinVersion, ns.config.MaxVersion)
	}

	// Select version (highest mutually supported)
	ns.agreedVersion = min8(offer.MaxVersion, ns.config.MaxVersion)
	if ns.agreedVersion < ns.config.MinVersion {
		ns.agreedVersion = ns.config.MinVersion
	}

	// Select cipher suite (first match from our preference list)
	cipherFound := false
	for _, ourCipher := range ns.config.CipherSuites {
		for _, theirCipher := range offer.CipherSuites {
			if ourCipher == theirCipher {
				ns.agreedCipher = ourCipher
				cipherFound = true
				break
			}
		}
		if cipherFound {
			break
		}
	}

	if !cipherFound {
		return nil, errors.New("no mutually supported cipher suites")
	}

	// Negotiate features (intersection of offered and required)
	ns.agreedFeatures = ns.negotiateFeatures(offer.Features)

	// Validate required features
	if err := ns.validateRequiredFeatures(); err != nil {
		return nil, err
	}

	// Create response
	response := &NegotiationResponse{
		Version:      ns.agreedVersion,
		CipherSuite:  ns.agreedCipher,
		Features:     ns.agreedFeatures,
	}

	// Add downgrade protection if enabled
	if ns.config.DowngradeProtection {
		response.DowngradeProof = ns.computeDowngradeProof()
	}

	encoded := encodeResponse(response)
	ns.transcript = append(ns.transcript, encoded...)

	return encoded, nil
}

// ProcessResponse processes a negotiation response
func (ns *NegotiationState) ProcessResponse(responseData []byte) error {
	ns.transcript = append(ns.transcript, responseData...)

	response, err := decodeResponse(responseData)
	if err != nil {
		return err
	}

	// Validate agreed version
	if response.Version < ns.config.MinVersion || response.Version > ns.config.MaxVersion {
		return fmt.Errorf("server selected invalid version: %d", response.Version)
	}
	ns.agreedVersion = response.Version

	// Validate agreed cipher
	validCipher := false
	for _, suite := range ns.config.CipherSuites {
		if suite == response.CipherSuite {
			validCipher = true
			break
		}
	}
	if !validCipher {
		return fmt.Errorf("server selected unsupported cipher suite: 0x%04x", response.CipherSuite)
	}
	ns.agreedCipher = response.CipherSuite

	ns.agreedFeatures = response.Features

	// Validate required features
	if err := ns.validateRequiredFeatures(); err != nil {
		return err
	}

	// Verify downgrade protection
	if ns.config.DowngradeProtection {
		if err := ns.verifyDowngradeProof(response.DowngradeProof); err != nil {
			return fmt.Errorf("downgrade attack detected: %w", err)
		}
	}

	return nil
}

func (ns *NegotiationState) getOfferedFeatures() FeatureFlags {
	var features FeatureFlags

	if ns.config.RequirePFS {
		features |= FeaturePFS
	}
	if ns.config.RequireAntiReplay {
		features |= FeatureAntiReplay
	}
	if ns.config.RequireObfuscation {
		features |= FeatureObfuscation
	}

	// Always offer these features
	features |= FeatureRekeying
	features |= FeatureDoubleRatchet

	return features
}

func (ns *NegotiationState) negotiateFeatures(offered FeatureFlags) FeatureFlags {
	// Start with our required features
	negotiated := ns.getOfferedFeatures()

	// Add optional features that are mutually supported
	if (offered & FeatureCompression) != 0 {
		negotiated |= FeatureCompression
	}

	// Both sides must agree on critical features
	negotiated &= offered

	return negotiated
}

func (ns *NegotiationState) validateRequiredFeatures() error {
	if ns.config.RequirePFS && (ns.agreedFeatures&FeaturePFS) == 0 {
		return errors.New("perfect forward secrecy required but not negotiated")
	}
	if ns.config.RequireAntiReplay && (ns.agreedFeatures&FeatureAntiReplay) == 0 {
		return errors.New("anti-replay protection required but not negotiated")
	}
	if ns.config.RequireObfuscation && (ns.agreedFeatures&FeatureObfuscation) == 0 {
		return errors.New("traffic obfuscation required but not negotiated")
	}
	return nil
}

func (ns *NegotiationState) computeDowngradeProof() []byte {
	// Create a MAC over the entire negotiation transcript
	// This binds the negotiation to the selected parameters
	mac := hmac.New(sha256.New, ns.config.SigningKey)
	mac.Write(ns.transcript)
	mac.Write([]byte("downgrade protection"))
	mac.Write([]byte{byte(ns.agreedVersion)})
	binary.Write(mac, binary.BigEndian, uint16(ns.agreedCipher))
	binary.Write(mac, binary.BigEndian, uint32(ns.agreedFeatures))

	return mac.Sum(nil)[:16]
}

func (ns *NegotiationState) verifyDowngradeProof(proof []byte) error {
	expected := ns.computeDowngradeProof()
	if !hmac.Equal(proof, expected) {
		return errors.New("downgrade proof verification failed")
	}
	return nil
}

// GetNegotiatedParams returns the negotiated parameters
func (ns *NegotiationState) GetNegotiatedParams() NegotiatedParams {
	return NegotiatedParams{
		Version:     ns.agreedVersion,
		CipherSuite: ns.agreedCipher,
		Features:    ns.agreedFeatures,
	}
}

// NegotiatedParams contains the final negotiated parameters
type NegotiatedParams struct {
	Version     ProtocolVersion
	CipherSuite CipherSuite
	Features    FeatureFlags
}

// HasFeature checks if a feature was negotiated
func (np NegotiatedParams) HasFeature(feature FeatureFlags) bool {
	return (np.Features & feature) != 0
}

// NegotiationOffer represents a client hello
type NegotiationOffer struct {
	MinVersion   ProtocolVersion
	MaxVersion   ProtocolVersion
	CipherSuites []CipherSuite
	Features     FeatureFlags
}

// NegotiationResponse represents a server hello
type NegotiationResponse struct {
	Version         ProtocolVersion
	CipherSuite     CipherSuite
	Features        FeatureFlags
	DowngradeProof  []byte
}

func encodeOffer(offer *NegotiationOffer) []byte {
	buf := make([]byte, 0, 64)

	buf = append(buf, byte(offer.MinVersion))
	buf = append(buf, byte(offer.MaxVersion))
	buf = append(buf, byte(len(offer.CipherSuites)))

	for _, suite := range offer.CipherSuites {
		buf = binary.BigEndian.AppendUint16(buf, uint16(suite))
	}

	buf = binary.BigEndian.AppendUint32(buf, uint32(offer.Features))

	return buf
}

func decodeOffer(data []byte) (*NegotiationOffer, error) {
	if len(data) < 3 {
		return nil, errors.New("offer too short")
	}

	offset := 0
	offer := &NegotiationOffer{
		MinVersion: ProtocolVersion(data[offset]),
		MaxVersion: ProtocolVersion(data[offset+1]),
	}
	offset += 2

	suiteCount := int(data[offset])
	offset++

	if len(data) < offset+suiteCount*2+4 {
		return nil, errors.New("offer truncated")
	}

	offer.CipherSuites = make([]CipherSuite, suiteCount)
	for i := 0; i < suiteCount; i++ {
		offer.CipherSuites[i] = CipherSuite(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2
	}

	offer.Features = FeatureFlags(binary.BigEndian.Uint32(data[offset : offset+4]))

	return offer, nil
}

func encodeResponse(response *NegotiationResponse) []byte {
	buf := make([]byte, 0, 32)

	buf = append(buf, byte(response.Version))
	buf = binary.BigEndian.AppendUint16(buf, uint16(response.CipherSuite))
	buf = binary.BigEndian.AppendUint32(buf, uint32(response.Features))

	if len(response.DowngradeProof) > 0 {
		buf = append(buf, byte(len(response.DowngradeProof)))
		buf = append(buf, response.DowngradeProof...)
	} else {
		buf = append(buf, 0)
	}

	return buf
}

func decodeResponse(data []byte) (*NegotiationResponse, error) {
	if len(data) < 8 {
		return nil, errors.New("response too short")
	}

	offset := 0
	response := &NegotiationResponse{
		Version:     ProtocolVersion(data[offset]),
		CipherSuite: CipherSuite(binary.BigEndian.Uint16(data[offset+1 : offset+3])),
		Features:    FeatureFlags(binary.BigEndian.Uint32(data[offset+3 : offset+7])),
	}
	offset += 7

	proofLen := int(data[offset])
	offset++

	if proofLen > 0 {
		if len(data) < offset+proofLen {
			return nil, errors.New("response truncated")
		}
		response.DowngradeProof = make([]byte, proofLen)
		copy(response.DowngradeProof, data[offset:offset+proofLen])
	}

	return response, nil
}

func min8(a, b ProtocolVersion) ProtocolVersion {
	if a < b {
		return a
	}
	return b
}

// VersionString returns a human-readable version string
func (v ProtocolVersion) String() string {
	switch v {
	case ProtocolVersionLegacy:
		return "Legacy (v1)"
	case ProtocolVersionNoise:
		return "Noise (v2)"
	default:
		return fmt.Sprintf("Unknown (v%d)", v)
	}
}

// String returns a human-readable feature flags string
func (f FeatureFlags) String() string {
	features := []string{}

	if (f & FeaturePFS) != 0 {
		features = append(features, "PFS")
	}
	if (f & FeatureAntiReplay) != 0 {
		features = append(features, "AntiReplay")
	}
	if (f & FeatureObfuscation) != 0 {
		features = append(features, "Obfuscation")
	}
	if (f & FeatureRekeying) != 0 {
		features = append(features, "Rekeying")
	}
	if (f & FeatureCompression) != 0 {
		features = append(features, "Compression")
	}
	if (f & FeatureDoubleRatchet) != 0 {
		features = append(features, "DoubleRatchet")
	}

	if len(features) == 0 {
		return "None"
	}

	result := features[0]
	for i := 1; i < len(features); i++ {
		result += ", " + features[i]
	}
	return result
}
