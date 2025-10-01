package transport

import (
	"bytes"
	"crypto/rand"
	"net"
	"testing"
	"time"
)

// TestPortHopping 测试动态端口跳跃
func TestPortHopping(t *testing.T) {
	// 创建共享密钥
	secret := make([]byte, 32)
	rand.Read(secret)

	config := PortHoppingConfig{
		Enabled:       true,
		PortRangeMin:  10000,
		PortRangeMax:  20000,
		HopInterval:   2 * time.Second,
		SharedSecret:  secret,
		SyncTolerance: 5 * time.Second,
	}

	// 客户端和服务器管理器
	clientMgr := NewPortHoppingManager(config)
	serverMgr := NewPortHoppingManager(config)

	// 初始端口应该相同
	clientPort := clientMgr.GetCurrentPort()
	serverPort := serverMgr.GetCurrentPort()

	if clientPort != serverPort {
		t.Errorf("Initial ports don't match: client=%d, server=%d", clientPort, serverPort)
	}

	t.Logf("Initial port: %d", clientPort)

	// 端口应该在指定范围内
	if clientPort < config.PortRangeMin || clientPort > config.PortRangeMax {
		t.Errorf("Port out of range: %d", clientPort)
	}

	// 测试端口验证
	if !clientMgr.ValidatePort(clientPort) {
		t.Error("Current port validation failed")
	}

	// 测试不同时间槽的端口
	currentSlot := clientMgr.getCurrentTimeSlot()
	nextPort := clientMgr.GetPortForTimeSlot(currentSlot + 1)

	if nextPort == clientPort {
		t.Error("Next time slot should have different port")
	}

	t.Logf("Next port: %d", nextPort)

	// 测试统计
	stats := clientMgr.GetStats()
	t.Logf("Stats: current=%d, hops=%d", stats.CurrentPort, stats.HopCount)
}

// TestRoaming 测试无缝漫游
func TestRoaming(t *testing.T) {
	config := DefaultRoamingConfig()
	config.SwitchThreshold = 3

	// 创建初始端点
	initialAddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:8000")
	manager := NewRoamingManager(config, initialAddr)

	// 测试获取当前端点
	currentAddr := manager.GetCurrentEndpoint()
	if currentAddr.String() != initialAddr.String() {
		t.Errorf("Initial endpoint mismatch: got %s, want %s",
			currentAddr.String(), initialAddr.String())
	}

	// 模拟从新地址接收数据包
	newAddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:9000")

	// 连续接收认证的数据包，直到切换
	// 期望：在第3个认证包时切换（阈值=3）
	var switched bool
	for i := 0; i < 5; i++ {
		switched = manager.UpdateEndpoint(newAddr, true)
		t.Logf("Packet %d: switched=%v", i+1, switched)

		if switched {
			break
		}
	}

	// 验证发生了切换
	if !switched {
		t.Error("Expected switch to occur")
	}

	// 验证端点已切换
	currentAddr = manager.GetCurrentEndpoint()
	if currentAddr.String() != newAddr.String() {
		t.Errorf("Endpoint not switched: got %s, want %s",
			currentAddr.String(), newAddr.String())
	}

	// 测试统计
	stats := manager.GetStats()
	t.Logf("Roaming stats: switches=%d, candidates=%d",
		stats.SwitchCount, stats.CandidateCount)

	if stats.SwitchCount != 1 {
		t.Errorf("Expected 1 switch, got %d", stats.SwitchCount)
	}
}

// TestPathValidator 测试路径验证
func TestPathValidator(t *testing.T) {
	validator := NewPathValidator()

	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:8000")

	// 创建挑战
	challenge := validator.CreateChallenge(addr)

	t.Logf("Challenge data: %v", challenge.Data)

	// 验证响应
	if !validator.ValidateResponse(addr, challenge.Data) {
		t.Error("Valid response rejected")
	}

	// 测试错误的数据
	wrongData := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	if validator.ValidateResponse(addr, wrongData) {
		t.Error("Invalid response accepted")
	}

	// 测试超时
	time.Sleep(6 * time.Second)
	if validator.ValidateResponse(addr, challenge.Data) {
		t.Error("Expired challenge accepted")
	}
}

// TestZeroRTT 测试0-RTT连接恢复
func TestZeroRTT(t *testing.T) {
	config := DefaultZeroRTTConfig()
	config.MaxTicketUsage = 3

	// 服务器端管理器
	serverMgr := NewZeroRTTManager(config)
	defer serverMgr.Stop()

	// 客户端管理器
	clientMgr := NewZeroRTTManager(config)
	defer clientMgr.Stop()

	// 模拟会话建立后签发票据
	peerAddr := "127.0.0.1:8000"
	sessionKey := make([]byte, 32)
	rand.Read(sessionKey)

	remotePubKey := [32]byte{}
	rand.Read(remotePubKey[:])

	// 服务器签发票据
	ticket, err := serverMgr.IssueTicket(peerAddr, sessionKey, remotePubKey)
	if err != nil {
		t.Fatalf("Failed to issue ticket: %v", err)
	}

	t.Logf("Ticket issued: ID=%x", ticket.ID)

	// 客户端存储票据
	clientMgr.StoreTicket(peerAddr, ticket)

	// 客户端使用票据
	usedTicket, err := clientMgr.UseTicket(peerAddr)
	if err != nil {
		t.Fatalf("Failed to use ticket: %v", err)
	}

	if !bytes.Equal(usedTicket.ID[:], ticket.ID[:]) {
		t.Error("Ticket ID mismatch")
	}

	// 服务器验证票据
	if err := serverMgr.ValidateTicket(usedTicket); err != nil {
		t.Errorf("Ticket validation failed: %v", err)
	}

	// 测试重放检测
	if err := serverMgr.ValidateTicket(usedTicket); err != nil {
		t.Logf("Replay correctly detected: %v", err)
	}

	// 测试统计
	serverStats := serverMgr.GetStats()
	t.Logf("Server stats: issued=%d, success=%d",
		serverStats.TicketsIssued, serverStats.ZeroRTTSuccess)

	clientStats := clientMgr.GetStats()
	t.Logf("Client stats: used=%d, stored=%d",
		clientStats.TicketsUsed, clientStats.TicketsStored)
}

// TestTicketSerialization 测试票据序列化
func TestTicketSerialization(t *testing.T) {
	// 创建票据
	ticket := &SessionTicket{
		SessionKey:      make([]byte, 32),
		IssuedAt:        time.Now(),
		ExpiresAt:       time.Now().Add(24 * time.Hour),
		UsageCount:      0,
		MaxUsage:        3,
	}
	rand.Read(ticket.ID[:])
	rand.Read(ticket.SessionKey)
	rand.Read(ticket.RemotePublicKey[:])

	// 序列化
	data := SerializeTicket(ticket)
	t.Logf("Serialized ticket size: %d bytes", len(data))

	// 反序列化
	recovered, err := DeserializeTicket(data)
	if err != nil {
		t.Fatalf("Failed to deserialize ticket: %v", err)
	}

	// 验证字段
	if ticket.ID != recovered.ID {
		t.Error("ID mismatch")
	}

	if !bytes.Equal(ticket.SessionKey, recovered.SessionKey) {
		t.Error("Session key mismatch")
	}

	if ticket.RemotePublicKey != recovered.RemotePublicKey {
		t.Error("Remote public key mismatch")
	}

	if ticket.MaxUsage != recovered.MaxUsage {
		t.Error("Max usage mismatch")
	}
}

// TestZeroRTTData 测试0-RTT数据编解码
func TestZeroRTTData(t *testing.T) {
	// 创建票据
	ticket := &SessionTicket{
		SessionKey: make([]byte, 32),
		IssuedAt:   time.Now(),
		ExpiresAt:  time.Now().Add(24 * time.Hour),
		MaxUsage:   3,
	}
	rand.Read(ticket.ID[:])
	rand.Read(ticket.SessionKey)

	// 创建0-RTT数据
	payload := []byte("Hello, 0-RTT!")
	zeroRTTData := &ZeroRTTData{
		Ticket:  ticket,
		Payload: payload,
	}

	// 编码
	encoded := EncodeZeroRTTData(zeroRTTData)
	t.Logf("Encoded 0-RTT data size: %d bytes", len(encoded))

	// 解码
	decoded, err := DecodeZeroRTTData(encoded)
	if err != nil {
		t.Fatalf("Failed to decode 0-RTT data: %v", err)
	}

	// 验证
	if !bytes.Equal(decoded.Payload, payload) {
		t.Errorf("Payload mismatch: got %q, want %q", decoded.Payload, payload)
	}

	if decoded.Ticket.ID != ticket.ID {
		t.Error("Ticket ID mismatch")
	}
}

// TestFallbackDetection 测试流量检测和回落
func TestFallbackDetection(t *testing.T) {
	config := DefaultFallbackConfig("")
	config.FallbackAddr = ""
	config.UseHTTPServer = true

	// 创建回落监听器
	listener, err := NewFallbackListener(config, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()
	t.Logf("Listening on %s", addr)

	// 测试协议检测
	testCases := []struct {
		name     string
		data     []byte
		expected bool
	}{
		{
			name:     "HTTP GET",
			data:     []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"),
			expected: false, // 应该回落
		},
		{
			name:     "HTTP POST",
			data:     []byte("POST /api HTTP/1.1\r\nHost: example.com\r\n\r\n"),
			expected: false,
		},
		{
			name:     "Valid Protocol",
			data:     []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
			expected: true, // 应该接受
		},
		{
			name:     "TLS ClientHello",
			data:     []byte{0x16, 0x03, 0x01, 0x00, 0x00},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := listener.isValidProtocol(tc.data)
			if result != tc.expected {
				t.Errorf("Detection result mismatch: got %v, want %v", result, tc.expected)
			}
		})
	}

	// 测试统计
	stats := listener.GetStats()
	t.Logf("Fallback stats: valid=%d, fallback=%d",
		stats.ValidConnections, stats.FallbackConnections)
}

// TestBufferedConn 测试缓冲连接
func TestBufferedConn(t *testing.T) {
	// 创建管道
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	// 包装为缓冲连接
	buffered := newBufferedConn(client, 4096)

	// 服务器发送数据
	testData := []byte("Hello, World!")
	go func() {
		server.Write(testData)
	}()

	// 等待数据到达
	time.Sleep(100 * time.Millisecond)

	// Peek数据
	peeked, err := buffered.Peek(5)
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}

	if string(peeked) != "Hello" {
		t.Errorf("Peeked data mismatch: got %q, want %q", peeked, "Hello")
	}

	// 读取完整数据
	buf := make([]byte, len(testData))
	n, err := buffered.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if n != len(testData) || string(buf) != string(testData) {
		t.Errorf("Read data mismatch: got %q, want %q", buf[:n], testData)
	}
}

// BenchmarkPortHoppingCalculation 基准测试端口计算
func BenchmarkPortHoppingCalculation(b *testing.B) {
	secret := make([]byte, 32)
	rand.Read(secret)

	config := DefaultPortHoppingConfig(secret)
	mgr := NewPortHoppingManager(config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.calculatePort(int64(i))
	}
}

// BenchmarkTicketSerialization 基准测试票据序列化
func BenchmarkTicketSerialization(b *testing.B) {
	ticket := &SessionTicket{
		SessionKey: make([]byte, 32),
		IssuedAt:   time.Now(),
		ExpiresAt:  time.Now().Add(24 * time.Hour),
		MaxUsage:   3,
	}
	rand.Read(ticket.ID[:])
	rand.Read(ticket.SessionKey)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data := SerializeTicket(ticket)
		DeserializeTicket(data)
	}
}

// BenchmarkZeroRTTDataEncoding 基准测试0-RTT数据编码
func BenchmarkZeroRTTDataEncoding(b *testing.B) {
	ticket := &SessionTicket{
		SessionKey: make([]byte, 32),
		IssuedAt:   time.Now(),
		ExpiresAt:  time.Now().Add(24 * time.Hour),
	}
	rand.Read(ticket.ID[:])

	payload := make([]byte, 1400) // MTU大小
	rand.Read(payload)

	zeroRTTData := &ZeroRTTData{
		Ticket:  ticket,
		Payload: payload,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded := EncodeZeroRTTData(zeroRTTData)
		DecodeZeroRTTData(encoded)
	}
}
