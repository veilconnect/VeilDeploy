package bridge

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRegisterBridge(t *testing.T) {
	discovery := NewDiscovery()

	bridge := &Bridge{
		Address:  "bridge1.example.com",
		Port:     51820,
		Type:     "direct",
		Capacity: 100,
		Location: "US",
	}

	err := discovery.RegisterBridge(bridge)
	if err != nil {
		t.Fatalf("Failed to register bridge: %v", err)
	}

	if bridge.ID == "" {
		t.Error("Bridge ID should be generated")
	}

	// 验证桥接已注册
	info, err := discovery.GetBridgeInfo(bridge.ID)
	if err != nil {
		t.Fatalf("Failed to get bridge info: %v", err)
	}

	if info.Address != bridge.Address {
		t.Errorf("Expected address %s, got %s", bridge.Address, info.Address)
	}
}

func TestGetBridges(t *testing.T) {
	discovery := NewDiscovery()

	// 注册多个桥接
	for i := 0; i < 5; i++ {
		bridge := &Bridge{
			Address:  "bridge" + string(rune('0'+i)) + ".example.com",
			Port:     51820 + i,
			Type:     "direct",
			Capacity: 100,
			Location: "US",
		}
		discovery.RegisterBridge(bridge)
	}

	// 获取桥接
	bridges, err := discovery.GetBridges("192.168.1.1", 3)
	if err != nil {
		t.Fatalf("Failed to get bridges: %v", err)
	}

	if len(bridges) != 3 {
		t.Errorf("Expected 3 bridges, got %d", len(bridges))
	}
}

func TestRateLimit(t *testing.T) {
	discovery := NewDiscovery()
	discovery.maxRequestsPerIP = 3

	// 注册桥接
	bridge := &Bridge{
		Address:  "bridge.example.com",
		Port:     51820,
		Type:     "direct",
		Capacity: 100,
		Location: "US",
	}
	discovery.RegisterBridge(bridge)

	clientIP := "192.168.1.1"

	// 前3次请求应该成功
	for i := 0; i < 3; i++ {
		_, err := discovery.GetBridges(clientIP, 1)
		if err != nil {
			t.Errorf("Request %d should succeed: %v", i+1, err)
		}
	}

	// 第4次请求应该失败
	_, err := discovery.GetBridges(clientIP, 1)
	if err == nil {
		t.Error("Request should fail due to rate limit")
	}
}

func TestUpdateBridge(t *testing.T) {
	discovery := NewDiscovery()

	bridge := &Bridge{
		Address:  "bridge.example.com",
		Port:     51820,
		Type:     "direct",
		Capacity: 100,
		Location: "US",
	}

	err := discovery.RegisterBridge(bridge)
	if err != nil {
		t.Fatalf("Failed to register bridge: %v", err)
	}

	// 记录初始时间
	initialTime := bridge.lastSeen

	// 等待一秒
	time.Sleep(1 * time.Second)

	// 更新桥接
	err = discovery.UpdateBridge(bridge.ID)
	if err != nil {
		t.Fatalf("Failed to update bridge: %v", err)
	}

	// 获取更新后的信息
	info, _ := discovery.GetBridgeInfo(bridge.ID)

	if !info.lastSeen.After(initialTime) {
		t.Error("lastSeen should be updated")
	}
}

func TestRemoveBridge(t *testing.T) {
	discovery := NewDiscovery()

	bridge := &Bridge{
		Address:  "bridge.example.com",
		Port:     51820,
		Type:     "direct",
		Capacity: 100,
		Location: "US",
	}

	discovery.RegisterBridge(bridge)

	// 移除桥接
	err := discovery.RemoveBridge(bridge.ID)
	if err != nil {
		t.Fatalf("Failed to remove bridge: %v", err)
	}

	// 验证桥接已移除
	_, err = discovery.GetBridgeInfo(bridge.ID)
	if err == nil {
		t.Error("Bridge should be removed")
	}
}

func TestListBridges(t *testing.T) {
	discovery := NewDiscovery()

	// 注册多个桥接
	for i := 0; i < 3; i++ {
		bridge := &Bridge{
			Address:  "bridge.example.com",
			Port:     51820 + i,
			Type:     "direct",
			Capacity: 100,
			Location: "US",
		}
		discovery.RegisterBridge(bridge)
	}

	// 列出所有桥接
	bridges := discovery.ListBridges()

	if len(bridges) != 3 {
		t.Errorf("Expected 3 bridges, got %d", len(bridges))
	}
}

func TestGetStats(t *testing.T) {
	discovery := NewDiscovery()

	// 注册不同类型的桥接
	bridges := []*Bridge{
		{Address: "bridge1.example.com", Port: 51820, Type: "direct", Location: "US"},
		{Address: "bridge2.example.com", Port: 51821, Type: "cdn", Location: "US"},
		{Address: "bridge3.example.com", Port: 51822, Type: "direct", Location: "JP"},
	}

	for _, bridge := range bridges {
		discovery.RegisterBridge(bridge)
	}

	// 获取统计
	stats := discovery.GetStats()

	if stats.TotalBridges != 3 {
		t.Errorf("Expected 3 total bridges, got %d", stats.TotalBridges)
	}

	if stats.ActiveBridges != 3 {
		t.Errorf("Expected 3 active bridges, got %d", stats.ActiveBridges)
	}

	if stats.TypeCount["direct"] != 2 {
		t.Errorf("Expected 2 direct bridges, got %d", stats.TypeCount["direct"])
	}

	if stats.LocationCount["US"] != 2 {
		t.Errorf("Expected 2 US bridges, got %d", stats.LocationCount["US"])
	}
}

func TestGetBridgesByEmail(t *testing.T) {
	discovery := NewDiscovery()

	// 注册桥接
	for i := 0; i < 5; i++ {
		bridge := &Bridge{
			Address:  "bridge.example.com",
			Port:     51820 + i,
			Type:     "direct",
			Capacity: 100,
			Location: "US",
		}
		discovery.RegisterBridge(bridge)
	}

	// 通过邮件获取桥接
	email := "user@example.com"
	bridges, challenge, err := discovery.GetBridgesByEmail(email)
	if err != nil {
		t.Fatalf("Failed to get bridges by email: %v", err)
	}

	if len(bridges) != 3 {
		t.Errorf("Expected 3 bridges, got %d", len(bridges))
	}

	if challenge == "" {
		t.Error("Challenge should not be empty")
	}

	// 验证挑战码
	if !discovery.VerifyChallenge(email, challenge) {
		t.Error("Challenge verification failed")
	}

	// 无效的挑战码应该失败
	if discovery.VerifyChallenge(email, "invalid-challenge") {
		t.Error("Invalid challenge should fail verification")
	}
}

func TestRequestBridgeHTTPS(t *testing.T) {
	discovery := NewDiscovery()

	// 注册桥接
	for i := 0; i < 5; i++ {
		bridge := &Bridge{
			Address:  "bridge.example.com",
			Port:     51820 + i,
			Type:     "direct",
			Capacity: 100,
			Location: "US",
		}
		discovery.RegisterBridge(bridge)
	}

	// 创建HTTP请求
	req := httptest.NewRequest("GET", "/bridges?count=3", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	// 创建响应记录器
	w := httptest.NewRecorder()

	// 处理请求
	discovery.RequestBridgeHTTPS(w, req)

	// 验证响应
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}
}

func TestExportImportBridges(t *testing.T) {
	discovery1 := NewDiscovery()

	// 注册桥接
	bridges := []*Bridge{
		{Address: "bridge1.example.com", Port: 51820, Type: "direct", Location: "US"},
		{Address: "bridge2.example.com", Port: 51821, Type: "cdn", Location: "JP"},
	}

	for _, bridge := range bridges {
		discovery1.RegisterBridge(bridge)
	}

	// 导出
	var buf bytes.Buffer
	err := discovery1.ExportBridges(&buf)
	if err != nil {
		t.Fatalf("Failed to export bridges: %v", err)
	}

	// 导入到新的discovery
	discovery2 := NewDiscovery()
	err = discovery2.ImportBridges(&buf)
	if err != nil {
		t.Fatalf("Failed to import bridges: %v", err)
	}

	// 验证导入的桥接
	importedBridges := discovery2.ListBridges()
	if len(importedBridges) != 2 {
		t.Errorf("Expected 2 imported bridges, got %d", len(importedBridges))
	}
}

func TestBridgeDistributor(t *testing.T) {
	discovery := NewDiscovery()

	// 注册桥接
	for i := 0; i < 3; i++ {
		bridge := &Bridge{
			Address:  "bridge.example.com",
			Port:     51820 + i,
			Type:     "direct",
			Capacity: 100,
			Location: "US",
		}
		discovery.RegisterBridge(bridge)
	}

	distributor := NewBridgeDistributor(discovery)

	// 通过邮件分发
	email := "user@example.com"
	content, err := distributor.DistributeByEmail(email)
	if err != nil {
		t.Fatalf("Failed to distribute by email: %v", err)
	}

	if content == "" {
		t.Error("Email content should not be empty")
	}

	// 通过HTTPS分发
	clientIP := "192.168.1.1"
	bridges, err := distributor.DistributeByHTTPS(clientIP, 2)
	if err != nil {
		t.Fatalf("Failed to distribute by HTTPS: %v", err)
	}

	if len(bridges) != 2 {
		t.Errorf("Expected 2 bridges, got %d", len(bridges))
	}
}

func TestBridgeTimeout(t *testing.T) {
	discovery := NewDiscovery()
	discovery.bridgeTimeout = 1 * time.Second

	// 注册桥接
	bridge := &Bridge{
		Address:  "bridge.example.com",
		Port:     51820,
		Type:     "direct",
		Capacity: 100,
		Location: "US",
	}

	discovery.RegisterBridge(bridge)

	// 初始应该能获取到桥接
	bridges, _ := discovery.GetBridges("192.168.1.1", 1)
	if len(bridges) != 1 {
		t.Error("Should get 1 bridge initially")
	}

	// 等待超时
	time.Sleep(2 * time.Second)

	// 超时后应该获取不到桥接
	bridges, _ = discovery.GetBridges("192.168.1.2", 1)
	if len(bridges) != 0 {
		t.Error("Should get 0 bridges after timeout")
	}
}

func TestBridgeCapacity(t *testing.T) {
	discovery := NewDiscovery()

	// 注册容量为0的桥接
	bridge := &Bridge{
		Address:     "bridge.example.com",
		Port:        51820,
		Type:        "direct",
		Capacity:    2,
		Location:    "US",
		connections: 2, // 已满
	}

	discovery.RegisterBridge(bridge)

	// 应该无法获取已满的桥接
	bridges, _ := discovery.GetBridges("192.168.1.1", 1)
	if len(bridges) != 0 {
		t.Error("Should not get bridge at capacity")
	}
}

func TestGenerateBridgeID(t *testing.T) {
	id1 := generateBridgeID()
	id2 := generateBridgeID()

	if id1 == "" {
		t.Error("Bridge ID should not be empty")
	}

	if id1 == id2 {
		t.Error("Bridge IDs should be unique")
	}
}

func TestGenerateChallenge(t *testing.T) {
	email := "test@example.com"
	secret := "secret123"

	challenge1 := generateChallenge(email, secret)
	challenge2 := generateChallenge(email, secret)

	// 相同输入应该产生相同的挑战
	if challenge1 != challenge2 {
		t.Error("Same input should generate same challenge")
	}

	// 不同的secret应该产生不同的挑战
	challenge3 := generateChallenge(email, "different-secret")
	if challenge1 == challenge3 {
		t.Error("Different secret should generate different challenge")
	}
}

func BenchmarkRegisterBridge(b *testing.B) {
	discovery := NewDiscovery()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bridge := &Bridge{
			Address:  "bridge.example.com",
			Port:     51820,
			Type:     "direct",
			Capacity: 100,
			Location: "US",
		}
		discovery.RegisterBridge(bridge)
	}
}

func BenchmarkGetBridges(b *testing.B) {
	discovery := NewDiscovery()
	discovery.maxRequestsPerIP = 1000000 // 禁用速率限制

	// 注册桥接
	for i := 0; i < 100; i++ {
		bridge := &Bridge{
			Address:  "bridge.example.com",
			Port:     51820 + i,
			Type:     "direct",
			Capacity: 100,
			Location: "US",
		}
		discovery.RegisterBridge(bridge)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		discovery.GetBridges("192.168.1.1", 10)
	}
}
