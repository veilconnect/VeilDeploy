package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// SimpleConfig 极简配置（3行足够）
type SimpleConfig struct {
	// 必填项（仅3个）
	Server   string `yaml:"server"`   // 服务器地址 vpn.example.com:51820
	Password string `yaml:"password"` // 密码
	Mode     string `yaml:"mode"`     // auto/client/server

	// 可选高级配置
	Advanced *AdvancedConfig `yaml:"advanced,omitempty"`
}

// AdvancedConfig 高级配置（可选）
type AdvancedConfig struct {
	// 抗审查
	Obfuscation  string `yaml:"obfuscation,omitempty"`   // auto/none/obfs4/tls
	PortHopping  bool   `yaml:"port_hopping,omitempty"`  // 动态端口跳跃
	CDN          string `yaml:"cdn,omitempty"`           // cloudflare/none
	Fallback     bool   `yaml:"fallback,omitempty"`      // 流量回落
	FallbackAddr string `yaml:"fallback_addr,omitempty"` // 回落地址

	// 性能
	Cipher      string `yaml:"cipher,omitempty"`      // chacha20/aes256/xchacha20
	Compression bool   `yaml:"compression,omitempty"` // 压缩

	// 安全
	TwoFactor bool   `yaml:"2fa,omitempty"`      // 双因素认证
	PFS       bool   `yaml:"pfs,omitempty"`      // 完美前向保密
	ZeroRTT   bool   `yaml:"zero_rtt,omitempty"` // 0-RTT恢复

	// 网络
	MTU        int    `yaml:"mtu,omitempty"`         // MTU大小
	KeepAlive  string `yaml:"keep_alive,omitempty"`  // 保活间隔
	DNSServers string `yaml:"dns_servers,omitempty"` // DNS服务器
}

// DefaultSimpleConfig 默认简单配置
func DefaultSimpleConfig() *SimpleConfig {
	return &SimpleConfig{
		Mode: "auto",
		Advanced: &AdvancedConfig{
			Obfuscation: "auto",
			PortHopping: true,
			PFS:         true,
			ZeroRTT:     true,
			Cipher:      "chacha20",
			MTU:         1420,
			KeepAlive:   "15s",
		},
	}
}

// LoadSimpleConfig 加载简单配置
func LoadSimpleConfig(path string) (*SimpleConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var config SimpleConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// 应用默认值
	if config.Mode == "" {
		config.Mode = "auto"
	}

	if config.Advanced == nil {
		config.Advanced = &AdvancedConfig{}
	}

	// Auto模式的默认值
	if config.Mode == "auto" || config.Advanced.Obfuscation == "auto" {
		applyAutoDefaults(&config)
	}

	return &config, nil
}

// applyAutoDefaults 应用自动默认值
func applyAutoDefaults(config *SimpleConfig) {
	adv := config.Advanced

	// 自动检测是否在中国
	inChina := detectChina()

	if inChina {
		// 中国优化配置
		if adv.Obfuscation == "" || adv.Obfuscation == "auto" {
			adv.Obfuscation = "obfs4"
		}
		adv.PortHopping = true
		adv.Fallback = true
		adv.FallbackAddr = "www.bing.com:443"
		adv.CDN = "cloudflare"
	} else {
		// 海外配置（性能优先）
		if adv.Obfuscation == "" || adv.Obfuscation == "auto" {
			adv.Obfuscation = "none"
		}
		adv.PortHopping = false
		adv.Fallback = false
	}

	// 通用优化
	if adv.Cipher == "" {
		adv.Cipher = "chacha20"
	}
	if adv.MTU == 0 {
		adv.MTU = 1420
	}
	if adv.KeepAlive == "" {
		adv.KeepAlive = "15s"
	}

	adv.PFS = true
	adv.ZeroRTT = true
}

// detectChina 检测是否在中国
func detectChina() bool {
	// 简化实现：检查环境变量
	if os.Getenv("VEILDEPLOY_CHINA") == "true" {
		return true
	}

	// 实际实现可以：
	// 1. GeoIP查询
	// 2. DNS污染检测
	// 3. 特定网站可达性测试
	// 这里返回false作为默认值
	return false
}

// SaveSimpleConfig 保存简单配置
func SaveSimpleConfig(config *SimpleConfig, path string) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// ToFullConfig 转换为完整配置
func (sc *SimpleConfig) ToFullConfig() (*Config, error) {
	// 使用现有的 Config 结构体
	config := &Config{
		Mode: sc.Mode,
		PSK:  sc.Password,
	}

	// 解析 Server 地址
	if sc.Server != "" {
		if sc.Mode == "server" {
			config.Listen = sc.Server
		} else {
			config.Endpoint = sc.Server
		}
	}

	// 设置默认值
	config.Keepalive = Duration{25 * time.Second}
	config.MaxPadding = 255

	// 网络参数
	if sc.Advanced != nil && sc.Advanced.MTU > 0 {
		config.Tunnel.MTU = sc.Advanced.MTU
	} else {
		config.Tunnel.MTU = 1420
	}

	return config, nil
}

// GenerateMinimalConfig 生成最小配置示例
func GenerateMinimalConfig(mode string) string {
	if mode == "server" {
		return `# VeilDeploy 最简服务器配置
server: 0.0.0.0:51820
password: your-secure-password
mode: server
`
	}

	return `# VeilDeploy 最简客户端配置
server: vpn.example.com:51820
password: your-secure-password
mode: auto
`
}

// GenerateFullConfig 生成完整配置示例
func GenerateFullConfig(mode string) string {
	if mode == "server" {
		return `# VeilDeploy 完整服务器配置
server: 0.0.0.0:51820
password: your-secure-password
mode: server

# 高级配置（可选）
advanced:
  # 抗审查
  obfuscation: obfs4        # auto/none/obfs4/tls
  port_hopping: true        # 动态端口跳跃
  fallback: true           # 流量回落
  fallback_addr: www.bing.com:443

  # 性能
  cipher: chacha20         # chacha20/aes256/xchacha20
  compression: false       # 压缩

  # 安全
  pfs: true               # 完美前向保密
  zero_rtt: true          # 0-RTT快速重连
  2fa: false             # 双因素认证

  # 网络
  mtu: 1420
  keep_alive: 15s
  dns_servers: 8.8.8.8,1.1.1.1
`
	}

	return `# VeilDeploy 完整客户端配置
server: vpn.example.com:51820
password: your-secure-password
mode: auto

# 高级配置（可选）
advanced:
  # 抗审查（auto会自动检测）
  obfuscation: auto        # auto/none/obfs4/tls
  port_hopping: true       # 动态端口跳跃
  cdn: cloudflare         # CDN加速

  # 性能
  cipher: chacha20        # chacha20/aes256
  compression: false

  # 安全
  pfs: true
  zero_rtt: true

  # 网络
  mtu: 1420
  keep_alive: 15s
`
}

// ValidateSimpleConfig 验证配置
func ValidateSimpleConfig(config *SimpleConfig) error {
	// 必填项检查
	if config.Server == "" {
		return fmt.Errorf("server is required")
	}

	if config.Password == "" {
		return fmt.Errorf("password is required")
	}

	if config.Mode == "" {
		return fmt.Errorf("mode is required")
	}

	// 模式检查
	validModes := map[string]bool{
		"auto":   true,
		"client": true,
		"server": true,
	}

	if !validModes[config.Mode] {
		return fmt.Errorf("invalid mode: %s (must be auto/client/server)", config.Mode)
	}

	// 高级配置检查
	if config.Advanced != nil {
		adv := config.Advanced

		// 混淆模式
		if adv.Obfuscation != "" {
			validObfs := map[string]bool{
				"auto":   true,
				"none":   true,
				"obfs4":  true,
				"tls":    true,
				"random": true,
			}

			if !validObfs[adv.Obfuscation] {
				return fmt.Errorf("invalid obfuscation mode: %s", adv.Obfuscation)
			}
		}

		// 密码套件
		if adv.Cipher != "" {
			validCiphers := map[string]bool{
				"chacha20":  true,
				"aes256":    true,
				"xchacha20": true,
			}

			if !validCiphers[adv.Cipher] {
				return fmt.Errorf("invalid cipher: %s", adv.Cipher)
			}
		}

		// MTU范围
		if adv.MTU != 0 && (adv.MTU < 1280 || adv.MTU > 1500) {
			return fmt.Errorf("invalid MTU: %d (must be 1280-1500)", adv.MTU)
		}
	}

	return nil
}
