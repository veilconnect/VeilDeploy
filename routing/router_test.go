package routing

import (
	"net"
	"testing"
)

func TestRouter(t *testing.T) {
	router := NewRouter(ActionProxy)

	// 添加规则
	rules := []*Rule{
		{Type: RuleTypeDomain, Pattern: "example.com", Action: ActionDirect},
		{Type: RuleTypeDomainSuffix, Pattern: ".google.com", Action: ActionProxy},
		{Type: RuleTypeDomainKeyword, Pattern: "ad", Action: ActionBlock},
		{Type: RuleTypeIPCIDR, Pattern: "192.168.0.0/16", Action: ActionDirect},
	}

	for _, rule := range rules {
		err := router.AddRule(rule)
		if err != nil {
			t.Fatalf("Failed to add rule: %v", err)
		}
	}

	// 测试域名匹配
	t.Run("DomainMatch", func(t *testing.T) {
		action := router.Route("example.com", nil, 0, "")
		if action != ActionDirect {
			t.Errorf("Expected ActionDirect, got %s", action)
		}
	})

	// 测试域名后缀匹配
	t.Run("DomainSuffixMatch", func(t *testing.T) {
		action := router.Route("www.google.com", nil, 0, "")
		if action != ActionProxy {
			t.Errorf("Expected ActionProxy, got %s", action)
		}
	})

	// 测试域名关键字匹配
	t.Run("DomainKeywordMatch", func(t *testing.T) {
		action := router.Route("ad.doubleclick.net", nil, 0, "")
		if action != ActionBlock {
			t.Errorf("Expected ActionBlock, got %s", action)
		}
	})

	// 测试IP CIDR匹配
	t.Run("IPCIDRMatch", func(t *testing.T) {
		ip := net.ParseIP("192.168.1.1")
		action := router.Route("", ip, 0, "")
		if action != ActionDirect {
			t.Errorf("Expected ActionDirect, got %s", action)
		}
	})

	// 测试默认动作
	t.Run("DefaultAction", func(t *testing.T) {
		action := router.Route("unknown.example.com", nil, 0, "")
		if action != ActionProxy {
			t.Errorf("Expected default ActionProxy, got %s", action)
		}
	})
}

func TestPortRule(t *testing.T) {
	router := NewRouter(ActionProxy)

	// 单个端口
	router.AddRule(&Rule{
		Type:    RuleTypePort,
		Pattern: "80",
		Action:  ActionDirect,
	})

	// 端口范围
	router.AddRule(&Rule{
		Type:    RuleTypePort,
		Pattern: "8000-9000",
		Action:  ActionBlock,
	})

	// 测试单个端口
	action := router.Route("", nil, 80, "")
	if action != ActionDirect {
		t.Errorf("Expected ActionDirect for port 80, got %s", action)
	}

	// 测试端口范围
	action = router.Route("", nil, 8080, "")
	if action != ActionBlock {
		t.Errorf("Expected ActionBlock for port 8080, got %s", action)
	}

	// 测试范围外的端口
	action = router.Route("", nil, 443, "")
	if action != ActionProxy {
		t.Errorf("Expected ActionProxy for port 443, got %s", action)
	}
}

func TestProtocolRule(t *testing.T) {
	router := NewRouter(ActionProxy)

	router.AddRule(&Rule{
		Type:    RuleTypeProtocol,
		Pattern: "http",
		Action:  ActionDirect,
	})

	// 测试协议匹配
	action := router.Route("", nil, 0, "http")
	if action != ActionDirect {
		t.Errorf("Expected ActionDirect for http, got %s", action)
	}

	// 测试不匹配的协议
	action = router.Route("", nil, 0, "https")
	if action != ActionProxy {
		t.Errorf("Expected ActionProxy for https, got %s", action)
	}
}

func TestGeoIPRule(t *testing.T) {
	// 创建示例GeoIP数据
	geoip := GenerateSampleGeoIP()

	router := NewRouter(ActionProxy)
	router.SetGeoIP(geoip)

	// 添加GeoIP规则
	router.AddRule(&Rule{
		Type:    RuleTypeGeoIP,
		Pattern: "CN",
		Action:  ActionDirect,
	})

	router.AddRule(&Rule{
		Type:    RuleTypeGeoIP,
		Pattern: "US",
		Action:  ActionProxy,
	})

	// 测试中国IP
	cnIP := net.ParseIP("1.0.1.1")
	action := router.Route("", cnIP, 0, "")
	if action != ActionDirect {
		t.Errorf("Expected ActionDirect for CN IP, got %s", action)
	}

	// 测试美国IP
	usIP := net.ParseIP("8.8.8.8")
	action = router.Route("", usIP, 0, "")
	if action != ActionProxy {
		t.Errorf("Expected ActionProxy for US IP, got %s", action)
	}
}

func TestPresetRules(t *testing.T) {
	router := NewRouter(ActionProxy)

	// 应用中国直连规则
	err := router.ApplyPreset("china-direct")
	if err != nil {
		t.Fatalf("Failed to apply preset: %v", err)
	}

	// 测试.cn域名
	action := router.Route("baidu.cn", nil, 0, "")
	if action != ActionDirect {
		t.Errorf("Expected ActionDirect for .cn domain, got %s", action)
	}

	// 测试百度
	action = router.Route("www.baidu.com", nil, 0, "")
	if action != ActionDirect {
		t.Errorf("Expected ActionDirect for baidu.com, got %s", action)
	}
}

func TestPresetBlockAds(t *testing.T) {
	router := NewRouter(ActionProxy)

	err := router.ApplyPreset("block-ads")
	if err != nil {
		t.Fatalf("Failed to apply preset: %v", err)
	}

	// 测试广告域名
	action := router.Route("ad.doubleclick.net", nil, 0, "")
	if action != ActionBlock {
		t.Errorf("Expected ActionBlock for ad domain, got %s", action)
	}

	action = router.Route("analytics.google.com", nil, 0, "")
	if action != ActionBlock {
		t.Errorf("Expected ActionBlock for analytics domain, got %s", action)
	}
}

func TestLocalDirect(t *testing.T) {
	router := NewRouter(ActionProxy)

	err := router.ApplyPreset("local-direct")
	if err != nil {
		t.Fatalf("Failed to apply preset: %v", err)
	}

	// 测试私有IP
	privateIPs := []string{
		"10.0.0.1",
		"172.16.0.1",
		"192.168.1.1",
		"127.0.0.1",
	}

	for _, ipStr := range privateIPs {
		ip := net.ParseIP(ipStr)
		action := router.Route("", ip, 0, "")
		if action != ActionDirect {
			t.Errorf("Expected ActionDirect for private IP %s, got %s", ipStr, action)
		}
	}

	// 测试公网IP
	publicIP := net.ParseIP("8.8.8.8")
	action := router.Route("", publicIP, 0, "")
	if action != ActionProxy {
		t.Errorf("Expected ActionProxy for public IP, got %s", action)
	}
}

func TestRemoveRule(t *testing.T) {
	router := NewRouter(ActionProxy)

	// 添加规则
	router.AddRule(&Rule{
		Type:    RuleTypeDomain,
		Pattern: "example.com",
		Action:  ActionDirect,
	})

	router.AddRule(&Rule{
		Type:    RuleTypeDomain,
		Pattern: "test.com",
		Action:  ActionBlock,
	})

	// 删除第一个规则
	err := router.RemoveRule(0)
	if err != nil {
		t.Fatalf("Failed to remove rule: %v", err)
	}

	// 验证规则已删除
	rules := router.ListRules()
	if len(rules) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(rules))
	}

	if rules[0].Pattern != "test.com" {
		t.Errorf("Expected remaining rule to be test.com, got %s", rules[0].Pattern)
	}
}

func TestClearRules(t *testing.T) {
	router := NewRouter(ActionProxy)

	// 添加规则
	router.AddRule(&Rule{
		Type:    RuleTypeDomain,
		Pattern: "example.com",
		Action:  ActionDirect,
	})

	router.AddRule(&Rule{
		Type:    RuleTypeDomain,
		Pattern: "test.com",
		Action:  ActionBlock,
	})

	// 清空规则
	router.ClearRules()

	// 验证规则已清空
	rules := router.ListRules()
	if len(rules) != 0 {
		t.Errorf("Expected 0 rules, got %d", len(rules))
	}
}

func TestRouterStats(t *testing.T) {
	router := NewRouter(ActionProxy)

	// 添加不同动作的规则
	router.AddRule(&Rule{Type: RuleTypeDomain, Pattern: "direct.com", Action: ActionDirect})
	router.AddRule(&Rule{Type: RuleTypeDomain, Pattern: "proxy.com", Action: ActionProxy})
	router.AddRule(&Rule{Type: RuleTypeDomain, Pattern: "block.com", Action: ActionBlock})
	router.AddRule(&Rule{Type: RuleTypeDomain, Pattern: "direct2.com", Action: ActionDirect})

	// 获取统计
	stats := router.GetStats()

	if stats.TotalRules != 4 {
		t.Errorf("Expected 4 total rules, got %d", stats.TotalRules)
	}

	if stats.ActionCounts[ActionDirect] != 2 {
		t.Errorf("Expected 2 direct rules, got %d", stats.ActionCounts[ActionDirect])
	}

	if stats.ActionCounts[ActionProxy] != 1 {
		t.Errorf("Expected 1 proxy rule, got %d", stats.ActionCounts[ActionProxy])
	}

	if stats.ActionCounts[ActionBlock] != 1 {
		t.Errorf("Expected 1 block rule, got %d", stats.ActionCounts[ActionBlock])
	}
}

func TestExportRules(t *testing.T) {
	router := NewRouter(ActionProxy)

	// 添加规则
	router.AddRule(&Rule{
		Type:    RuleTypeDomain,
		Pattern: "example.com",
		Action:  ActionDirect,
	})

	router.AddRule(&Rule{
		Type:    RuleTypeDomainSuffix,
		Pattern: ".google.com",
		Action:  ActionProxy,
	})

	// 导出配置
	config := router.ExportRules()

	if config.DefaultAction != ActionProxy {
		t.Errorf("Expected default action Proxy, got %s", config.DefaultAction)
	}

	if len(config.Rules) != 2 {
		t.Errorf("Expected 2 rules, got %d", len(config.Rules))
	}

	if config.Rules[0].Pattern != "example.com" {
		t.Errorf("Expected first rule pattern example.com, got %s", config.Rules[0].Pattern)
	}
}

func TestLoadRules(t *testing.T) {
	router := NewRouter(ActionDirect)

	// 创建配置
	config := &RoutingConfig{
		DefaultAction: ActionProxy,
		Rules: []RuleConfig{
			{Type: RuleTypeDomain, Pattern: "example.com", Action: ActionDirect},
			{Type: RuleTypeDomainSuffix, Pattern: ".cn", Action: ActionDirect},
		},
	}

	// 加载规则
	err := router.LoadRules(config)
	if err != nil {
		t.Fatalf("Failed to load rules: %v", err)
	}

	// 验证规则
	rules := router.ListRules()
	if len(rules) != 2 {
		t.Errorf("Expected 2 rules, got %d", len(rules))
	}

	// 测试规则
	action := router.Route("example.com", nil, 0, "")
	if action != ActionDirect {
		t.Errorf("Expected ActionDirect, got %s", action)
	}

	action = router.Route("baidu.cn", nil, 0, "")
	if action != ActionDirect {
		t.Errorf("Expected ActionDirect, got %s", action)
	}
}

func BenchmarkRouterMatch(b *testing.B) {
	router := NewRouter(ActionProxy)
	router.ApplyPreset("all")

	domain := "www.google.com"
	ip := net.ParseIP("8.8.8.8")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		router.Route(domain, ip, 80, "http")
	}
}

func BenchmarkRouterGeoIP(b *testing.B) {
	geoip := GenerateSampleGeoIP()
	router := NewRouter(ActionProxy)
	router.SetGeoIP(geoip)
	router.AddRule(&Rule{
		Type:    RuleTypeGeoIP,
		Pattern: "CN",
		Action:  ActionDirect,
	})

	ip := net.ParseIP("1.0.1.1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		router.Route("", ip, 0, "")
	}
}
