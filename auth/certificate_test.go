package auth

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"testing"
	"time"
)

func TestGenerateCA(t *testing.T) {
	req := &CertificateRequest{
		CommonName:   "VeilDeploy CA",
		Organization: "VeilDeploy",
		Country:      "US",
		ValidFor:     365 * 24 * time.Hour,
		KeySize:      2048, // 使用2048以加快测试
	}

	caCert, caKey, err := GenerateCA(req)
	if err != nil {
		t.Fatalf("Failed to generate CA: %v", err)
	}

	if caCert == nil {
		t.Fatal("CA certificate is nil")
	}

	if caKey == nil {
		t.Fatal("CA private key is nil")
	}

	// 验证CA证书属性
	if !caCert.IsCA {
		t.Error("Certificate should be a CA")
	}

	if caCert.Subject.CommonName != req.CommonName {
		t.Errorf("Expected CommonName '%s', got '%s'", req.CommonName, caCert.Subject.CommonName)
	}
}

func TestIssueCertificate(t *testing.T) {
	// 生成CA
	caReq := &CertificateRequest{
		CommonName:   "Test CA",
		Organization: "Test Org",
		ValidFor:     365 * 24 * time.Hour,
		KeySize:      2048,
	}

	caCert, caKey, err := GenerateCA(caReq)
	if err != nil {
		t.Fatalf("Failed to generate CA: %v", err)
	}

	// 创建认证器
	certAuth := NewCertificateAuth(caCert, caKey)

	// 签发客户端证书
	clientReq := &CertificateRequest{
		CommonName:   "test-client",
		Organization: "Test Org",
		ValidFor:     30 * 24 * time.Hour,
		KeySize:      2048,
	}

	clientCert, clientKey, err := certAuth.IssueCertificate(clientReq)
	if err != nil {
		t.Fatalf("Failed to issue certificate: %v", err)
	}

	if clientCert == nil {
		t.Fatal("Client certificate is nil")
	}

	if clientKey == nil {
		t.Fatal("Client private key is nil")
	}

	// 验证证书属性
	if clientCert.IsCA {
		t.Error("Client certificate should not be a CA")
	}

	if clientCert.Subject.CommonName != clientReq.CommonName {
		t.Errorf("Expected CommonName '%s', got '%s'", clientReq.CommonName, clientCert.Subject.CommonName)
	}
}

func TestVerifyCertificate(t *testing.T) {
	// 生成CA
	caReq := &CertificateRequest{
		CommonName:   "Test CA",
		Organization: "Test Org",
		ValidFor:     365 * 24 * time.Hour,
		KeySize:      2048,
	}

	caCert, caKey, err := GenerateCA(caReq)
	if err != nil {
		t.Fatalf("Failed to generate CA: %v", err)
	}

	certAuth := NewCertificateAuth(caCert, caKey)

	// 签发证书
	clientReq := &CertificateRequest{
		CommonName:   "test-client",
		Organization: "Test Org",
		ValidFor:     30 * 24 * time.Hour,
		KeySize:      2048,
	}

	clientCert, _, err := certAuth.IssueCertificate(clientReq)
	if err != nil {
		t.Fatalf("Failed to issue certificate: %v", err)
	}

	// 验证证书
	err = certAuth.VerifyCertificate(clientCert)
	if err != nil {
		t.Errorf("Certificate verification failed: %v", err)
	}
}

func TestRevokeCertificate(t *testing.T) {
	// 生成CA
	caReq := &CertificateRequest{
		CommonName:   "Test CA",
		Organization: "Test Org",
		ValidFor:     365 * 24 * time.Hour,
		KeySize:      2048,
	}

	caCert, caKey, err := GenerateCA(caReq)
	if err != nil {
		t.Fatalf("Failed to generate CA: %v", err)
	}

	certAuth := NewCertificateAuth(caCert, caKey)

	// 签发证书
	clientReq := &CertificateRequest{
		CommonName:   "test-client",
		Organization: "Test Org",
		ValidFor:     30 * 24 * time.Hour,
		KeySize:      2048,
	}

	clientCert, _, err := certAuth.IssueCertificate(clientReq)
	if err != nil {
		t.Fatalf("Failed to issue certificate: %v", err)
	}

	serialNumber := clientCert.SerialNumber.String()

	// 吊销证书
	err = certAuth.RevokeCertificate(serialNumber)
	if err != nil {
		t.Fatalf("Failed to revoke certificate: %v", err)
	}

	// 验证吊销状态
	info, err := certAuth.GetCertificateInfo(serialNumber)
	if err != nil {
		t.Fatalf("Failed to get certificate info: %v", err)
	}

	if !info.Revoked {
		t.Error("Certificate should be revoked")
	}

	// 验证已吊销的证书应该失败
	err = certAuth.VerifyCertificate(clientCert)
	if err == nil {
		t.Error("Verification should fail for revoked certificate")
	}
}

func TestSaveAndLoadCertificate(t *testing.T) {
	// 生成CA
	caReq := &CertificateRequest{
		CommonName:   "Test CA",
		Organization: "Test Org",
		ValidFor:     365 * 24 * time.Hour,
		KeySize:      2048,
	}

	caCert, caKey, err := GenerateCA(caReq)
	if err != nil {
		t.Fatalf("Failed to generate CA: %v", err)
	}

	// 创建临时文件
	certFile := t.TempDir() + "/cert.pem"
	keyFile := t.TempDir() + "/key.pem"

	// 保存证书
	err = SaveCertificatePEM(caCert, certFile)
	if err != nil {
		t.Fatalf("Failed to save certificate: %v", err)
	}

	// 保存私钥
	err = SavePrivateKeyPEM(caKey, keyFile)
	if err != nil {
		t.Fatalf("Failed to save private key: %v", err)
	}

	// 加载证书
	loadedCert, err := LoadCertificatePEM(certFile)
	if err != nil {
		t.Fatalf("Failed to load certificate: %v", err)
	}

	// 加载私钥
	loadedKey, err := LoadPrivateKeyPEM(keyFile)
	if err != nil {
		t.Fatalf("Failed to load private key: %v", err)
	}

	// 验证加载的内容
	if !caCert.Equal(loadedCert) {
		t.Error("Loaded certificate does not match original")
	}

	if caKey.N.Cmp(loadedKey.N) != 0 {
		t.Error("Loaded private key does not match original")
	}
}

func TestCertificateStats(t *testing.T) {
	// 生成CA
	caReq := &CertificateRequest{
		CommonName:   "Test CA",
		Organization: "Test Org",
		ValidFor:     365 * 24 * time.Hour,
		KeySize:      2048,
	}

	caCert, caKey, err := GenerateCA(caReq)
	if err != nil {
		t.Fatalf("Failed to generate CA: %v", err)
	}

	certAuth := NewCertificateAuth(caCert, caKey)

	// 签发多个证书
	for i := 0; i < 3; i++ {
		clientReq := &CertificateRequest{
			CommonName:   "test-client",
			Organization: "Test Org",
			ValidFor:     30 * 24 * time.Hour,
			KeySize:      2048,
		}

		_, _, err := certAuth.IssueCertificate(clientReq)
		if err != nil {
			t.Fatalf("Failed to issue certificate %d: %v", i, err)
		}
	}

	// 获取统计
	stats := certAuth.GetStats()

	if stats.Total != 3 {
		t.Errorf("Expected 3 total certificates, got %d", stats.Total)
	}

	if stats.Active != 3 {
		t.Errorf("Expected 3 active certificates, got %d", stats.Active)
	}
}

func TestListCertificates(t *testing.T) {
	// 生成CA
	caReq := &CertificateRequest{
		CommonName:   "Test CA",
		Organization: "Test Org",
		ValidFor:     365 * 24 * time.Hour,
		KeySize:      2048,
	}

	caCert, caKey, err := GenerateCA(caReq)
	if err != nil {
		t.Fatalf("Failed to generate CA: %v", err)
	}

	certAuth := NewCertificateAuth(caCert, caKey)

	// 签发证书
	clientReq := &CertificateRequest{
		CommonName:   "test-client",
		Organization: "Test Org",
		ValidFor:     30 * 24 * time.Hour,
		KeySize:      2048,
	}

	_, _, err = certAuth.IssueCertificate(clientReq)
	if err != nil {
		t.Fatalf("Failed to issue certificate: %v", err)
	}

	// 列出证书
	certs := certAuth.ListCertificates()

	if len(certs) != 1 {
		t.Errorf("Expected 1 certificate, got %d", len(certs))
	}

	if certs[0].CommonName != clientReq.CommonName {
		t.Errorf("Expected CommonName '%s', got '%s'", clientReq.CommonName, certs[0].CommonName)
	}
}

func TestCreateTLSConfig(t *testing.T) {
	// 生成CA
	caReq := &CertificateRequest{
		CommonName:   "Test CA",
		Organization: "Test Org",
		ValidFor:     365 * 24 * time.Hour,
		KeySize:      2048,
	}

	caCert, caKey, err := GenerateCA(caReq)
	if err != nil {
		t.Fatalf("Failed to generate CA: %v", err)
	}

	certAuth := NewCertificateAuth(caCert, caKey)

	// 创建服务器TLS配置
	serverConfig := certAuth.CreateTLSConfig(true)

	if serverConfig.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Error("Server should require and verify client certificates")
	}

	if serverConfig.MinVersion != tls.VersionTLS12 {
		t.Error("MinVersion should be TLS 1.2")
	}

	// 创建客户端TLS配置
	clientConfig := certAuth.CreateTLSConfig(false)

	if clientConfig.InsecureSkipVerify {
		t.Error("Client should not skip certificate verification")
	}
}

func TestRenewCertificate(t *testing.T) {
	// 生成CA
	caReq := &CertificateRequest{
		CommonName:   "Test CA",
		Organization: "Test Org",
		ValidFor:     365 * 24 * time.Hour,
		KeySize:      2048,
	}

	caCert, caKey, err := GenerateCA(caReq)
	if err != nil {
		t.Fatalf("Failed to generate CA: %v", err)
	}

	certAuth := NewCertificateAuth(caCert, caKey)

	// 签发原始证书
	clientReq := &CertificateRequest{
		CommonName:   "test-client",
		Organization: "Test Org",
		ValidFor:     30 * 24 * time.Hour,
		KeySize:      2048,
	}

	oldCert, _, err := certAuth.IssueCertificate(clientReq)
	if err != nil {
		t.Fatalf("Failed to issue certificate: %v", err)
	}

	// 续期证书
	newCert, _, err := certAuth.RenewCertificate(oldCert, 60*24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to renew certificate: %v", err)
	}

	// 验证新证书
	if newCert.Subject.CommonName != oldCert.Subject.CommonName {
		t.Error("Renewed certificate should have same CommonName")
	}

	if newCert.SerialNumber.Cmp(oldCert.SerialNumber) == 0 {
		t.Error("Renewed certificate should have different serial number")
	}
}

func TestCheckExpiringSoon(t *testing.T) {
	// 生成CA
	caReq := &CertificateRequest{
		CommonName:   "Test CA",
		Organization: "Test Org",
		ValidFor:     365 * 24 * time.Hour,
		KeySize:      2048,
	}

	caCert, caKey, err := GenerateCA(caReq)
	if err != nil {
		t.Fatalf("Failed to generate CA: %v", err)
	}

	certAuth := NewCertificateAuth(caCert, caKey)

	// 签发一个即将过期的证书（1天）
	clientReq := &CertificateRequest{
		CommonName:   "expiring-client",
		Organization: "Test Org",
		ValidFor:     24 * time.Hour,
		KeySize:      2048,
	}

	_, _, err = certAuth.IssueCertificate(clientReq)
	if err != nil {
		t.Fatalf("Failed to issue certificate: %v", err)
	}

	// 签发一个长期证书
	longReq := &CertificateRequest{
		CommonName:   "long-term-client",
		Organization: "Test Org",
		ValidFor:     365 * 24 * time.Hour,
		KeySize:      2048,
	}

	_, _, err = certAuth.IssueCertificate(longReq)
	if err != nil {
		t.Fatalf("Failed to issue certificate: %v", err)
	}

	// 检查30天内过期的证书
	expiring := certAuth.CheckExpiringSoon(30 * 24 * time.Hour)

	if len(expiring) != 1 {
		t.Errorf("Expected 1 expiring certificate, got %d", len(expiring))
	}

	if len(expiring) > 0 && expiring[0].CommonName != "expiring-client" {
		t.Errorf("Expected expiring certificate to be 'expiring-client', got '%s'", expiring[0].CommonName)
	}
}

func TestExportCertificateBundle(t *testing.T) {
	// 生成CA
	caReq := &CertificateRequest{
		CommonName:   "Test CA",
		Organization: "Test Org",
		ValidFor:     365 * 24 * time.Hour,
		KeySize:      2048,
	}

	caCert, caKey, err := GenerateCA(caReq)
	if err != nil {
		t.Fatalf("Failed to generate CA: %v", err)
	}

	certAuth := NewCertificateAuth(caCert, caKey)

	// 签发客户端证书
	clientReq := &CertificateRequest{
		CommonName:   "test-client",
		Organization: "Test Org",
		ValidFor:     30 * 24 * time.Hour,
		KeySize:      2048,
	}

	clientCert, clientKey, err := certAuth.IssueCertificate(clientReq)
	if err != nil {
		t.Fatalf("Failed to issue certificate: %v", err)
	}

	// 导出证书包
	bundleFile := t.TempDir() + "/bundle.pem"
	file, err := os.Create(bundleFile)
	if err != nil {
		t.Fatalf("Failed to create bundle file: %v", err)
	}
	defer file.Close()

	err = certAuth.ExportCertificateBundle(clientCert, clientKey, file)
	if err != nil {
		t.Fatalf("Failed to export certificate bundle: %v", err)
	}

	// 验证文件存在且不为空
	info, err := os.Stat(bundleFile)
	if err != nil {
		t.Fatalf("Bundle file not found: %v", err)
	}

	if info.Size() == 0 {
		t.Error("Bundle file is empty")
	}
}

func TestCertificateExpired(t *testing.T) {
	// 生成CA
	caReq := &CertificateRequest{
		CommonName:   "Test CA",
		Organization: "Test Org",
		ValidFor:     365 * 24 * time.Hour,
		KeySize:      2048,
	}

	caCert, caKey, err := GenerateCA(caReq)
	if err != nil {
		t.Fatalf("Failed to generate CA: %v", err)
	}

	certAuth := NewCertificateAuth(caCert, caKey)

	// 签发一个已过期的证书（负的有效期）
	// 注意：实际上我们不能创建真正过期的证书用于测试
	// 这里只是测试证书信息的时间检查逻辑

	clientReq := &CertificateRequest{
		CommonName:   "test-client",
		Organization: "Test Org",
		ValidFor:     1 * time.Second, // 很短的有效期
		KeySize:      2048,
	}

	clientCert, _, err := certAuth.IssueCertificate(clientReq)
	if err != nil {
		t.Fatalf("Failed to issue certificate: %v", err)
	}

	// 等待证书过期
	time.Sleep(2 * time.Second)

	// 验证过期证书
	opts := x509.VerifyOptions{
		Roots:       certAuth.certPool,
		CurrentTime: time.Now(),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	_, err = clientCert.Verify(opts)
	if err == nil {
		t.Error("Verification should fail for expired certificate")
	}
}
