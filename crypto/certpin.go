package crypto

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
)

// CertificatePin represents a pinned certificate
type CertificatePin struct {
	Fingerprint string
	CommonName  string
	NotAfter    string
	PinType     PinType
}

// PinType defines the type of certificate pinning
type PinType uint8

const (
	// PinTypePublicKey pins the public key
	PinTypePublicKey PinType = iota
	// PinTypeCertificate pins the entire certificate
	PinTypeCertificate
	// PinTypeSubjectPublicKeyInfo pins the SubjectPublicKeyInfo
	PinTypeSubjectPublicKeyInfo
)

// CertificatePinner manages certificate pinning
type CertificatePinner struct {
	pins         map[string]*CertificatePin
	strictMode   bool // If true, reject connections if no pin matches
	allowBackup  bool // Allow backup pins
	mu           sync.RWMutex
}

// NewCertificatePinner creates a new certificate pinner
func NewCertificatePinner(strictMode bool) *CertificatePinner {
	return &CertificatePinner{
		pins:        make(map[string]*CertificatePin),
		strictMode:  strictMode,
		allowBackup: true,
	}
}

// AddPin adds a certificate pin
func (cp *CertificatePinner) AddPin(fingerprint, commonName string, pinType PinType) error {
	if fingerprint == "" {
		return errors.New("fingerprint cannot be empty")
	}

	cp.mu.Lock()
	defer cp.mu.Unlock()

	pin := &CertificatePin{
		Fingerprint: fingerprint,
		CommonName:  commonName,
		PinType:     pinType,
	}

	cp.pins[fingerprint] = pin
	return nil
}

// RemovePin removes a certificate pin
func (cp *CertificatePinner) RemovePin(fingerprint string) error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	if _, exists := cp.pins[fingerprint]; !exists {
		return fmt.Errorf("pin %s not found", fingerprint)
	}

	delete(cp.pins, fingerprint)
	return nil
}

// VerifyCertificate verifies a certificate against pinned certificates
func (cp *CertificatePinner) VerifyCertificate(cert *x509.Certificate, pinType PinType) error {
	if cert == nil {
		return errors.New("certificate is nil")
	}

	fingerprint, err := cp.computeFingerprint(cert, pinType)
	if err != nil {
		return err
	}

	cp.mu.RLock()
	defer cp.mu.RUnlock()

	// If no pins are configured and not in strict mode, allow
	if len(cp.pins) == 0 && !cp.strictMode {
		return nil
	}

	// Check if fingerprint matches any pin
	if pin, exists := cp.pins[fingerprint]; exists {
		if pin.PinType == pinType {
			return nil
		}
	}

	if cp.strictMode || len(cp.pins) > 0 {
		return fmt.Errorf("certificate fingerprint %s does not match any pinned certificate", fingerprint)
	}

	return nil
}

// VerifyCertificateChain verifies an entire certificate chain
func (cp *CertificatePinner) VerifyCertificateChain(chain []*x509.Certificate, pinType PinType) error {
	if len(chain) == 0 {
		return errors.New("certificate chain is empty")
	}

	// Try to match any certificate in the chain
	for _, cert := range chain {
		if err := cp.VerifyCertificate(cert, pinType); err == nil {
			return nil
		}
	}

	return fmt.Errorf("no certificate in chain matches pinned certificates")
}

// computeFingerprint computes the fingerprint of a certificate
func (cp *CertificatePinner) computeFingerprint(cert *x509.Certificate, pinType PinType) (string, error) {
	var data []byte

	switch pinType {
	case PinTypePublicKey:
		// Hash the public key bytes
		data = cert.RawSubjectPublicKeyInfo

	case PinTypeCertificate:
		// Hash the entire certificate
		data = cert.Raw

	case PinTypeSubjectPublicKeyInfo:
		// Hash the SubjectPublicKeyInfo
		data = cert.RawSubjectPublicKeyInfo

	default:
		return "", errors.New("unknown pin type")
	}

	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// ComputeCertificateFingerprint computes a certificate fingerprint (public function)
func ComputeCertificateFingerprint(cert *x509.Certificate, pinType PinType) (string, error) {
	pinner := NewCertificatePinner(false)
	return pinner.computeFingerprint(cert, pinType)
}

// ListPins returns all configured pins
func (cp *CertificatePinner) ListPins() []*CertificatePin {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	pins := make([]*CertificatePin, 0, len(cp.pins))
	for _, pin := range cp.pins {
		pinCopy := *pin
		pins = append(pins, &pinCopy)
	}
	return pins
}

// SetStrictMode enables or disables strict mode
func (cp *CertificatePinner) SetStrictMode(strict bool) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	cp.strictMode = strict
}

// IsStrictMode returns whether strict mode is enabled
func (cp *CertificatePinner) IsStrictMode() bool {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	return cp.strictMode
}

// Clear removes all pins
func (cp *CertificatePinner) Clear() {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	cp.pins = make(map[string]*CertificatePin)
}

// Count returns the number of configured pins
func (cp *CertificatePinner) Count() int {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	return len(cp.pins)
}

// String returns a string representation of a pin type
func (pt PinType) String() string {
	switch pt {
	case PinTypePublicKey:
		return "public-key"
	case PinTypeCertificate:
		return "certificate"
	case PinTypeSubjectPublicKeyInfo:
		return "spki"
	default:
		return "unknown"
	}
}

// ParsePinType converts a string to PinType
func ParsePinType(s string) (PinType, error) {
	switch s {
	case "public-key":
		return PinTypePublicKey, nil
	case "certificate":
		return PinTypeCertificate, nil
	case "spki":
		return PinTypeSubjectPublicKeyInfo, nil
	default:
		return PinTypePublicKey, fmt.Errorf("unknown pin type: %s", s)
	}
}
