package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"time"

	"stp/audit"
	"stp/auth"
	"stp/crypto"
	"stp/transport"
)

func main() {
	fmt.Println("🚀 VeilDeploy 企业级功能演示")
	fmt.Println("================================\n")

	// 1. 加密套件演示
	demonstrateCipherSuites()

	// 2. 用户认证演示
	demonstrateAuthentication()

	// 3. ACL 演示
	demonstrateACL()

	// 4. 审计日志演示
	demonstrateAuditLog()

	// 5. 拥塞控制演示
	demonstrateCongestionControl()

	fmt.Println("\n✅ 所有功能演示完成！")
}

func demonstrateCipherSuites() {
	fmt.Println("📦 1. 加密套件支持")
	fmt.Println("-------------------")

	// 列出所有支持的加密套件
	suites := crypto.ListSupportedCipherSuites()
	for _, suite := range suites {
		fmt.Printf("  • %s (0x%04x)\n", suite.Name, suite.ID)
		fmt.Printf("    密钥: %d 字节, Nonce: %d 字节\n", suite.KeySize, suite.NonceSize)
		fmt.Printf("    描述: %s\n", suite.Description)
	}

	// 测试加密
	key := make([]byte, 32)
	rand.Read(key)

	plaintext := []byte("VeilDeploy Enterprise Features")

	// 使用 ChaCha20-Poly1305
	cs1, _ := crypto.NewCipherSuiteState(crypto.CipherSuiteChaCha20Poly1305, key)
	fmt.Printf("\n  测试加密: %s\n", plaintext)
	fmt.Printf("  使用套件: %s\n", cs1.Info().Name)

	// 使用 AES-256-GCM
	cs2, _ := crypto.NewCipherSuiteState(crypto.CipherSuiteAES256GCM, key)
	fmt.Printf("  使用套件: %s\n", cs2.Info().Name)

	fmt.Println()
}

func demonstrateAuthentication() {
	fmt.Println("🔐 2. 用户认证系统")
	fmt.Println("-------------------")

	authenticator := auth.NewAuthenticator()

	// 添加用户
	authenticator.AddUser("admin", "SecurePass123!", []string{"admin", "read", "write"})
	authenticator.AddUser("user1", "UserPass456", []string{"read"})
	fmt.Println("  ✓ 创建用户: admin (角色: admin, read, write)")
	fmt.Println("  ✓ 创建用户: user1 (角色: read)")

	// 认证
	token, err := authenticator.Authenticate(auth.Credential{
		Username: "admin",
		Password: "SecurePass123!",
	})
	if err != nil {
		fmt.Printf("  ✗ 认证失败: %v\n", err)
	} else {
		fmt.Printf("  ✓ 认证成功！令牌: %s...\n", token[:16])
	}

	// 验证令牌
	authToken, err := authenticator.ValidateToken(token)
	if err == nil {
		fmt.Printf("  ✓ 令牌有效，用户: %s\n", authToken.Username)
	}

	// 检查权限
	if authenticator.HasRole(token, "admin") {
		fmt.Println("  ✓ 用户具有 admin 角色")
	}

	// 列出用户
	users := authenticator.ListUsers()
	fmt.Printf("  系统用户: %v\n", users)

	fmt.Println()
}

func demonstrateACL() {
	fmt.Println("🛡️  3. 访问控制列表 (ACL)")
	fmt.Println("-------------------------")

	acl := auth.NewACLManager(auth.PermissionNone)

	// 添加规则
	acl.AddRule("internal", "192.168.0.0/16", auth.PermissionAdmin, []string{"admin"})
	acl.AddRule("vpn", "10.0.0.0/8", auth.PermissionWrite, []string{"user"})
	acl.AddRule("public", "0.0.0.0/0", auth.PermissionRead, nil)

	fmt.Println("  规则配置:")
	rules := acl.ListRules()
	for _, rule := range rules {
		fmt.Printf("    • %s: %s -> %s (角色: %v)\n",
			rule.Name, rule.IPNet.String(), rule.Permission.String(), rule.Roles)
	}

	// 测试权限
	testIPs := []string{"192.168.1.100", "10.0.0.5", "8.8.8.8"}
	fmt.Println("\n  权限测试:")
	for _, ipStr := range testIPs {
		ip := net.ParseIP(ipStr)
		canRead := acl.CheckPermission(ip, auth.PermissionRead)
		canWrite := acl.CheckPermission(ip, auth.PermissionWrite)
		canAdmin := acl.CheckPermission(ip, auth.PermissionAdmin)

		fmt.Printf("    %s: 读=%v, 写=%v, 管理=%v\n",
			ipStr, canRead, canWrite, canAdmin)
	}

	fmt.Println()
}

func demonstrateAuditLog() {
	fmt.Println("📝 4. 审计日志系统")
	fmt.Println("-------------------")

	logger, err := audit.NewAuditLogger(audit.AuditLoggerConfig{
		OutputPath: "stdout",
		BufferSize: 100,
	})
	if err != nil {
		fmt.Printf("  ✗ 创建审计日志失败: %v\n", err)
		return
	}
	defer logger.Close()

	fmt.Println("  记录审计事件:")

	// 记录各种事件
	logger.LogAuthentication("admin", "192.168.1.100", "success", "User logged in successfully")
	fmt.Println("  ✓ 认证事件已记录")

	logger.LogConnection("admin", "192.168.1.100", "connect", "success")
	fmt.Println("  ✓ 连接事件已记录")

	logger.LogConfiguration("admin", "firewall_rules", "update", "success", map[string]interface{}{
		"rules_count": 5,
		"action":      "allow_http",
	})
	fmt.Println("  ✓ 配置变更已记录")

	logger.LogDataTransfer("admin", "192.168.1.100", 1024000, 5*time.Second)
	fmt.Println("  ✓ 数据传输已记录")

	// 获取统计
	stats := logger.GetStatistics()
	fmt.Printf("\n  审计统计:\n")
	fmt.Printf("    总事件数: %v\n", stats["total_events"])
	fmt.Printf("    事件类型: %v\n", stats["event_types"])
	fmt.Printf("    事件级别: %v\n", stats["event_levels"])

	// 搜索事件
	events := logger.SearchEvents(audit.EventTypeAuthentication, "", time.Time{}, time.Time{})
	fmt.Printf("    认证事件: %d 条\n", len(events))

	fmt.Println()
}

func demonstrateCongestionControl() {
	fmt.Println("⚡ 5. 自适应拥塞控制")
	fmt.Println("---------------------")

	cc := transport.NewCongestionControl()
	cc.SetMSS(1400)

	fmt.Printf("  初始状态: %s\n", cc.GetState())
	fmt.Printf("  初始窗口: %.1f 包 (%d 字节)\n", cc.GetCongestionWindow(), cc.GetSendWindow())

	// 模拟数据传输
	fmt.Println("\n  模拟数据传输:")

	for i := 0; i < 20; i++ {
		packetSize := uint64(1400)

		if cc.CanSend(packetSize) {
			cc.OnPacketSent(packetSize)

			// 模拟 RTT
			rtt := 50*time.Millisecond + time.Duration(i)*time.Millisecond

			// 模拟 ACK
			cc.OnPacketAcked(packetSize, rtt)

			if (i+1)%5 == 0 {
				fmt.Printf("    包 %d: cwnd=%.1f, 状态=%s, RTT=%dms\n",
					i+1, cc.GetCongestionWindow(), cc.GetState(), rtt.Milliseconds())
			}
		}
	}

	// 模拟丢包
	fmt.Println("\n  模拟丢包事件:")
	cc.OnPacketLost(1400)
	fmt.Printf("    丢包后: cwnd=%.1f, 状态=%s\n",
		cc.GetCongestionWindow(), cc.GetState())

	// 继续传输
	for i := 0; i < 10; i++ {
		packetSize := uint64(1400)
		if cc.CanSend(packetSize) {
			cc.OnPacketSent(packetSize)
			cc.OnPacketAcked(packetSize, 55*time.Millisecond)
		}
	}

	fmt.Printf("    恢复后: cwnd=%.1f, 状态=%s\n",
		cc.GetCongestionWindow(), cc.GetState())

	// 获取详细统计
	stats := cc.GetStatistics()
	fmt.Println("\n  拥塞控制统计:")
	fmt.Printf("    发送包数: %v\n", stats["packets_sent"])
	fmt.Printf("    确认包数: %v\n", stats["packets_acked"])
	fmt.Printf("    丢失包数: %v\n", stats["packets_lost"])
	fmt.Printf("    丢包率: %.2f%%\n", stats["loss_rate"].(float64)*100)
	fmt.Printf("    平滑RTT: %v ms\n", stats["srtt_ms"])
	fmt.Printf("    最小RTT: %v ms\n", stats["min_rtt_ms"])
	fmt.Printf("    重传超时: %v ms\n", stats["rto_ms"])
	fmt.Printf("    发送窗口: %v 字节\n", stats["send_window"])

	fmt.Println()
}

func demonstrateMultiplexing() {
	fmt.Println("🔀 6. 连接复用")
	fmt.Println("---------------")

	// 注意：这需要实际的网络连接，这里只演示概念
	fmt.Println("  功能说明:")
	fmt.Println("    • 单连接支持多个并发流")
	fmt.Println("    • 减少连接建立开销")
	fmt.Println("    • 自动流管理和心跳")
	fmt.Println("    • 支持优先级和流控")

	fmt.Println("\n  使用示例:")
	fmt.Println("    mux, _ := transport.DialMux(ctx, \"tcp\", \"server:8080\")")
	fmt.Println("    stream1, _ := mux.OpenStream()")
	fmt.Println("    stream2, _ := mux.OpenStream()")
	fmt.Println("    // 同时使用多个流")

	// 在实际网络环境中的测试
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	fmt.Println("\n  尝试连接到本地测试服务器...")
	_, err := transport.DialMux(ctx, "tcp", "localhost:9999")
	if err != nil {
		fmt.Printf("  ⚠  无可用测试服务器 (这是正常的)\n")
	} else {
		fmt.Println("  ✓ 连接成功！")
	}

	fmt.Println()
}
