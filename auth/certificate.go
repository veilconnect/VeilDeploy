package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"os"
	"sync"
	"time"
)

// CertificateAuth 证书认证器
type CertificateAuth struct {
	ca          *x509.Certificate
	caKey       *rsa.PrivateKey
	certPool    *x509.CertPool
	mu          sync.RWMutex
	certs       map[string]*CertificateInfo
	revoked     map[string]time.Time // 吊销列表
	autoRenew   bool
	renewBefore time.Duration
}

// CertificateInfo 证书信息
type CertificateInfo struct {
	SerialNumber string    `json:"serial_number"`
	CommonName   string    `json:"common_name"`
	Organization string    `json:"organization"`
	IssuedAt     time.Time `json:"issued_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	Revoked      bool      `json:"revoked"`
	RevokedAt    time.Time `json:"revoked_at,omitempty"`
}

// CertificateRequest 证书请求
type CertificateRequest struct {
	CommonName         string
	Organization       string
	OrganizationalUnit string
	Country            string
	Province           string
	Locality           string
	EmailAddress       string
	ValidFor           time.Duration
	IsCA               bool
	KeySize            int
}

// NewCertificateAuth 创建证书认证器
func NewCertificateAuth(caCert *x509.Certificate, caKey *rsa.PrivateKey) *CertificateAuth {
	certPool := x509.NewCertPool()
	certPool.AddCert(caCert)

	return &CertificateAuth{
		ca:          caCert,
		caKey:       caKey,
		certPool:    certPool,
		certs:       make(map[string]*CertificateInfo),
		revoked:     make(map[string]time.Time),
		autoRenew:   true,
		renewBefore: 30 * 24 * time.Hour, // 30天前自动续期
	}
}

// GenerateCA 生成CA证书
func GenerateCA(req *CertificateRequest) (*x509.Certificate, *rsa.PrivateKey, error) {
	if req.ValidFor == 0 {
		req.ValidFor = 10 * 365 * 24 * time.Hour // 10年
	}

	if req.KeySize == 0 {
		req.KeySize = 4096
	}

	// 生成私钥
	privateKey, err := rsa.GenerateKey(rand.Reader, req.KeySize)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// 生成证书模板
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:         req.CommonName,
			Organization:       []string{req.Organization},
			OrganizationalUnit: []string{req.OrganizationalUnit},
			Country:            []string{req.Country},
			Province:           []string{req.Province},
			Locality:           []string{req.Locality},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(req.ValidFor),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            2,
	}

	// 自签名
	certBytes, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// 解析证书
	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return cert, privateKey, nil
}

// IssueCertificate 签发客户端证书
func (ca *CertificateAuth) IssueCertificate(req *CertificateRequest) (*x509.Certificate, *rsa.PrivateKey, error) {
	if req.ValidFor == 0 {
		req.ValidFor = 365 * 24 * time.Hour // 1年
	}

	if req.KeySize == 0 {
		req.KeySize = 2048
	}

	// 生成私钥
	privateKey, err := rsa.GenerateKey(rand.Reader, req.KeySize)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// 生成序列号
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	// 证书模板
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:         req.CommonName,
			Organization:       []string{req.Organization},
			OrganizationalUnit: []string{req.OrganizationalUnit},
			Country:            []string{req.Country},
			Province:           []string{req.Province},
			Locality:           []string{req.Locality},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(req.ValidFor),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}

	// 使用CA签名
	certBytes, err := x509.CreateCertificate(rand.Reader, template, ca.ca, &privateKey.PublicKey, ca.caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// 解析证书
	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// 记录证书
	ca.mu.Lock()
	ca.certs[serialNumber.String()] = &CertificateInfo{
		SerialNumber: serialNumber.String(),
		CommonName:   req.CommonName,
		Organization: req.Organization,
		IssuedAt:     cert.NotBefore,
		ExpiresAt:    cert.NotAfter,
		Revoked:      false,
	}
	ca.mu.Unlock()

	return cert, privateKey, nil
}

// VerifyCertificate 验证证书
func (ca *CertificateAuth) VerifyCertificate(cert *x509.Certificate) error {
	// 检查吊销状态
	ca.mu.RLock()
	serialNumber := cert.SerialNumber.String()
	if revokedAt, revoked := ca.revoked[serialNumber]; revoked {
		ca.mu.RUnlock()
		return fmt.Errorf("certificate revoked at %s", revokedAt.Format(time.RFC3339))
	}
	ca.mu.RUnlock()

	// 验证证书链
	opts := x509.VerifyOptions{
		Roots:       ca.certPool,
		CurrentTime: time.Now(),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	if _, err := cert.Verify(opts); err != nil {
		return fmt.Errorf("certificate verification failed: %w", err)
	}

	return nil
}

// RevokeCertificate 吊销证书
func (ca *CertificateAuth) RevokeCertificate(serialNumber string) error {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	certInfo, exists := ca.certs[serialNumber]
	if !exists {
		return fmt.Errorf("certificate not found: %s", serialNumber)
	}

	if certInfo.Revoked {
		return fmt.Errorf("certificate already revoked")
	}

	now := time.Now()
	certInfo.Revoked = true
	certInfo.RevokedAt = now
	ca.revoked[serialNumber] = now

	return nil
}

// RenewCertificate 续期证书
func (ca *CertificateAuth) RenewCertificate(oldCert *x509.Certificate, validFor time.Duration) (*x509.Certificate, *rsa.PrivateKey, error) {
	// 创建续期请求
	req := &CertificateRequest{
		CommonName:   oldCert.Subject.CommonName,
		Organization: "",
		ValidFor:     validFor,
	}

	if len(oldCert.Subject.Organization) > 0 {
		req.Organization = oldCert.Subject.Organization[0]
	}

	// 签发新证书
	return ca.IssueCertificate(req)
}

// ListCertificates 列出所有证书
func (ca *CertificateAuth) ListCertificates() []*CertificateInfo {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	certs := make([]*CertificateInfo, 0, len(ca.certs))
	for _, cert := range ca.certs {
		certs = append(certs, cert)
	}

	return certs
}

// GetCertificateInfo 获取证书信息
func (ca *CertificateAuth) GetCertificateInfo(serialNumber string) (*CertificateInfo, error) {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	info, exists := ca.certs[serialNumber]
	if !exists {
		return nil, fmt.Errorf("certificate not found: %s", serialNumber)
	}

	return info, nil
}

// SaveCertificatePEM 保存证书到PEM文件
func SaveCertificatePEM(cert *x509.Certificate, filename string) error {
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})

	return os.WriteFile(filename, certPEM, 0644)
}

// SavePrivateKeyPEM 保存私钥到PEM文件
func SavePrivateKeyPEM(key *rsa.PrivateKey, filename string) error {
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	return os.WriteFile(filename, keyPEM, 0600)
}

// LoadCertificatePEM 从PEM文件加载证书
func LoadCertificatePEM(filename string) (*x509.Certificate, error) {
	certPEM, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate: %w", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return cert, nil
}

// LoadPrivateKeyPEM 从PEM文件加载私钥
func LoadPrivateKeyPEM(filename string) (*rsa.PrivateKey, error) {
	keyPEM, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}

	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return key, nil
}

// CreateTLSConfig 创建TLS配置
func (ca *CertificateAuth) CreateTLSConfig(serverMode bool) *tls.Config {
	config := &tls.Config{
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS13,
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  ca.certPool,
		CipherSuites: []uint16{
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
	}

	if serverMode {
		config.ClientAuth = tls.RequireAndVerifyClientCert
	} else {
		config.InsecureSkipVerify = false
		config.RootCAs = ca.certPool
	}

	return config
}

// VerifyConnection 验证TLS连接
func (ca *CertificateAuth) VerifyConnection(connState tls.ConnectionState) error {
	if len(connState.PeerCertificates) == 0 {
		return fmt.Errorf("no peer certificates")
	}

	peerCert := connState.PeerCertificates[0]
	return ca.VerifyCertificate(peerCert)
}

// ExportCertificateBundle 导出证书包
func (ca *CertificateAuth) ExportCertificateBundle(cert *x509.Certificate, key *rsa.PrivateKey, writer io.Writer) error {
	// 写入CA证书
	if err := pem.Encode(writer, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: ca.ca.Raw,
	}); err != nil {
		return fmt.Errorf("failed to encode CA certificate: %w", err)
	}

	// 写入客户端证书
	if err := pem.Encode(writer, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}); err != nil {
		return fmt.Errorf("failed to encode client certificate: %w", err)
	}

	// 写入私钥
	if err := pem.Encode(writer, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}); err != nil {
		return fmt.Errorf("failed to encode private key: %w", err)
	}

	return nil
}

// CheckExpiringSoon 检查即将过期的证书
func (ca *CertificateAuth) CheckExpiringSoon(threshold time.Duration) []*CertificateInfo {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	now := time.Now()
	expiring := make([]*CertificateInfo, 0)

	for _, cert := range ca.certs {
		if cert.Revoked {
			continue
		}

		timeLeft := cert.ExpiresAt.Sub(now)
		if timeLeft > 0 && timeLeft < threshold {
			expiring = append(expiring, cert)
		}
	}

	return expiring
}

// Stats 证书统计
type CertStats struct {
	Total      int `json:"total"`
	Active     int `json:"active"`
	Revoked    int `json:"revoked"`
	Expired    int `json:"expired"`
	ExpiringSoon int `json:"expiring_soon"` // 30天内过期
}

// GetStats 获取证书统计
func (ca *CertificateAuth) GetStats() *CertStats {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	stats := &CertStats{
		Total: len(ca.certs),
	}

	now := time.Now()
	threshold := now.Add(30 * 24 * time.Hour)

	for _, cert := range ca.certs {
		if cert.Revoked {
			stats.Revoked++
		} else if cert.ExpiresAt.Before(now) {
			stats.Expired++
		} else if cert.ExpiresAt.Before(threshold) {
			stats.ExpiringSoon++
			stats.Active++
		} else {
			stats.Active++
		}
	}

	return stats
}
