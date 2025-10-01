package routing

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
)

// Action 路由动作
type Action string

const (
	ActionProxy  Action = "proxy"  // 代理
	ActionDirect Action = "direct" // 直连
	ActionBlock  Action = "block"  // 阻止
)

// RuleType 规则类型
type RuleType string

const (
	RuleTypeDomain     RuleType = "domain"      // 域名
	RuleTypeDomainSuffix RuleType = "domain-suffix" // 域名后缀
	RuleTypeDomainKeyword RuleType = "domain-keyword" // 域名关键字
	RuleTypeIP         RuleType = "ip"          // IP地址
	RuleTypeIPCIDR     RuleType = "ip-cidr"     // IP CIDR
	RuleTypeGeoIP      RuleType = "geoip"       // GeoIP
	RuleTypePort       RuleType = "port"        // 端口
	RuleTypeProtocol   RuleType = "protocol"    // 协议
)

// Rule 路由规则
type Rule struct {
	Type    RuleType `json:"type"`
	Pattern string   `json:"pattern"`
	Action  Action   `json:"action"`

	// 内部使用
	regex  *regexp.Regexp
	cidr   *net.IPNet
}

// Router 路由器
type Router struct {
	rules      []*Rule
	geoip      *GeoIP
	mu         sync.RWMutex
	defaultAction Action
}

// NewRouter 创建路由器
func NewRouter(defaultAction Action) *Router {
	return &Router{
		rules:         make([]*Rule, 0),
		defaultAction: defaultAction,
	}
}

// AddRule 添加规则
func (r *Router) AddRule(rule *Rule) error {
	// 预编译正则表达式
	if rule.Type == RuleTypeDomainKeyword {
		regex, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
		rule.regex = regex
	}

	// 解析CIDR
	if rule.Type == RuleTypeIPCIDR {
		_, cidr, err := net.ParseCIDR(rule.Pattern)
		if err != nil {
			return fmt.Errorf("invalid CIDR: %w", err)
		}
		rule.cidr = cidr
	}

	r.mu.Lock()
	r.rules = append(r.rules, rule)
	r.mu.Unlock()

	return nil
}

// Route 路由决策
func (r *Router) Route(domain string, ip net.IP, port int, protocol string) Action {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, rule := range r.rules {
		if r.matchRule(rule, domain, ip, port, protocol) {
			return rule.Action
		}
	}

	return r.defaultAction
}

// matchRule 匹配规则
func (r *Router) matchRule(rule *Rule, domain string, ip net.IP, port int, protocol string) bool {
	switch rule.Type {
	case RuleTypeDomain:
		return strings.EqualFold(domain, rule.Pattern)

	case RuleTypeDomainSuffix:
		return strings.HasSuffix(strings.ToLower(domain), strings.ToLower(rule.Pattern))

	case RuleTypeDomainKeyword:
		if rule.regex != nil {
			return rule.regex.MatchString(domain)
		}
		return strings.Contains(strings.ToLower(domain), strings.ToLower(rule.Pattern))

	case RuleTypeIP:
		if ip == nil {
			return false
		}
		targetIP := net.ParseIP(rule.Pattern)
		return ip.Equal(targetIP)

	case RuleTypeIPCIDR:
		if ip == nil || rule.cidr == nil {
			return false
		}
		return rule.cidr.Contains(ip)

	case RuleTypeGeoIP:
		if ip == nil || r.geoip == nil {
			return false
		}
		country := r.geoip.Lookup(ip)
		return strings.EqualFold(country, rule.Pattern)

	case RuleTypePort:
		// 解析端口范围
		if strings.Contains(rule.Pattern, "-") {
			// 端口范围 "80-443"
			parts := strings.Split(rule.Pattern, "-")
			if len(parts) == 2 {
				var start, end int
				fmt.Sscanf(parts[0], "%d", &start)
				fmt.Sscanf(parts[1], "%d", &end)
				return port >= start && port <= end
			}
		}
		// 单个端口
		var rulePort int
		fmt.Sscanf(rule.Pattern, "%d", &rulePort)
		return port == rulePort

	case RuleTypeProtocol:
		return strings.EqualFold(protocol, rule.Pattern)

	default:
		return false
	}
}

// LoadRules 从配置加载规则
func (r *Router) LoadRules(config *RoutingConfig) error {
	for _, ruleConfig := range config.Rules {
		rule := &Rule{
			Type:    ruleConfig.Type,
			Pattern: ruleConfig.Pattern,
			Action:  ruleConfig.Action,
		}

		if err := r.AddRule(rule); err != nil {
			return fmt.Errorf("failed to add rule: %w", err)
		}
	}

	return nil
}

// RemoveRule 移除规则
func (r *Router) RemoveRule(index int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if index < 0 || index >= len(r.rules) {
		return fmt.Errorf("invalid rule index: %d", index)
	}

	r.rules = append(r.rules[:index], r.rules[index+1:]...)
	return nil
}

// ListRules 列出所有规则
func (r *Router) ListRules() []*Rule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rules := make([]*Rule, len(r.rules))
	copy(rules, r.rules)
	return rules
}

// ClearRules 清空所有规则
func (r *Router) ClearRules() {
	r.mu.Lock()
	r.rules = make([]*Rule, 0)
	r.mu.Unlock()
}

// SetGeoIP 设置GeoIP数据库
func (r *Router) SetGeoIP(geoip *GeoIP) {
	r.mu.Lock()
	r.geoip = geoip
	r.mu.Unlock()
}

// Stats 路由统计
type RouterStats struct {
	TotalRules   int            `json:"total_rules"`
	ActionCounts map[Action]int `json:"action_counts"`
}

// GetStats 获取路由统计
func (r *Router) GetStats() *RouterStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := &RouterStats{
		TotalRules:   len(r.rules),
		ActionCounts: make(map[Action]int),
	}

	for _, rule := range r.rules {
		stats.ActionCounts[rule.Action]++
	}

	return stats
}

// RoutingConfig 路由配置
type RoutingConfig struct {
	DefaultAction Action        `yaml:"default_action" json:"default_action"`
	Rules         []RuleConfig  `yaml:"rules" json:"rules"`
	GeoIPDatabase string        `yaml:"geoip_database" json:"geoip_database"`
}

// RuleConfig 规则配置
type RuleConfig struct {
	Type    RuleType `yaml:"type" json:"type"`
	Pattern string   `yaml:"pattern" json:"pattern"`
	Action  Action   `yaml:"action" json:"action"`
}

// PresetRules 预设规则
type PresetRules struct {
	ChinaDirect      []*Rule // 中国网站直连
	ChinaProxy       []*Rule // 国外网站代理
	BlockAds         []*Rule // 广告拦截
	LocalDirect      []*Rule // 本地网络直连
}

// LoadPresetRules 加载预设规则
func LoadPresetRules() *PresetRules {
	return &PresetRules{
		ChinaDirect: []*Rule{
			{Type: RuleTypeDomainSuffix, Pattern: ".cn", Action: ActionDirect},
			{Type: RuleTypeDomainSuffix, Pattern: ".baidu.com", Action: ActionDirect},
			{Type: RuleTypeDomainSuffix, Pattern: ".taobao.com", Action: ActionDirect},
			{Type: RuleTypeDomainSuffix, Pattern: ".qq.com", Action: ActionDirect},
			{Type: RuleTypeDomainSuffix, Pattern: ".weixin.com", Action: ActionDirect},
			{Type: RuleTypeDomainSuffix, Pattern: ".aliyun.com", Action: ActionDirect},
			{Type: RuleTypeGeoIP, Pattern: "CN", Action: ActionDirect},
		},
		ChinaProxy: []*Rule{
			{Type: RuleTypeDomainSuffix, Pattern: ".google.com", Action: ActionProxy},
			{Type: RuleTypeDomainSuffix, Pattern: ".youtube.com", Action: ActionProxy},
			{Type: RuleTypeDomainSuffix, Pattern: ".facebook.com", Action: ActionProxy},
			{Type: RuleTypeDomainSuffix, Pattern: ".twitter.com", Action: ActionProxy},
			{Type: RuleTypeDomainSuffix, Pattern: ".instagram.com", Action: ActionProxy},
			{Type: RuleTypeDomainSuffix, Pattern: ".github.com", Action: ActionProxy},
		},
		BlockAds: []*Rule{
			{Type: RuleTypeDomainKeyword, Pattern: "ad", Action: ActionBlock},
			{Type: RuleTypeDomainKeyword, Pattern: "analytics", Action: ActionBlock},
			{Type: RuleTypeDomainKeyword, Pattern: "tracker", Action: ActionBlock},
			{Type: RuleTypeDomainSuffix, Pattern: ".doubleclick.net", Action: ActionBlock},
		},
		LocalDirect: []*Rule{
			{Type: RuleTypeIPCIDR, Pattern: "10.0.0.0/8", Action: ActionDirect},
			{Type: RuleTypeIPCIDR, Pattern: "172.16.0.0/12", Action: ActionDirect},
			{Type: RuleTypeIPCIDR, Pattern: "192.168.0.0/16", Action: ActionDirect},
			{Type: RuleTypeIPCIDR, Pattern: "127.0.0.0/8", Action: ActionDirect},
		},
	}
}

// ApplyPreset 应用预设规则
func (r *Router) ApplyPreset(preset string) error {
	presets := LoadPresetRules()

	var rules []*Rule

	switch preset {
	case "china-direct":
		rules = presets.ChinaDirect
	case "china-proxy":
		rules = presets.ChinaProxy
	case "block-ads":
		rules = presets.BlockAds
	case "local-direct":
		rules = presets.LocalDirect
	case "all":
		// 应用所有预设规则（按优先级）
		rules = append(rules, presets.LocalDirect...)
		rules = append(rules, presets.BlockAds...)
		rules = append(rules, presets.ChinaProxy...)
		rules = append(rules, presets.ChinaDirect...)
	default:
		return fmt.Errorf("unknown preset: %s", preset)
	}

	for _, rule := range rules {
		if err := r.AddRule(rule); err != nil {
			return err
		}
	}

	return nil
}

// ExportRules 导出规则为配置
func (r *Router) ExportRules() *RoutingConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	config := &RoutingConfig{
		DefaultAction: r.defaultAction,
		Rules:         make([]RuleConfig, 0, len(r.rules)),
	}

	for _, rule := range r.rules {
		config.Rules = append(config.Rules, RuleConfig{
			Type:    rule.Type,
			Pattern: rule.Pattern,
			Action:  rule.Action,
		})
	}

	return config
}
