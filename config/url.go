package config

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ParseVeilURL 解析 veil:// URL
// 格式: veil://METHOD:PASSWORD@HOST:PORT/?PARAMS
// 示例: veil://chacha20:mypassword@vpn.example.com:51820/?obfs=tls&cdn=true
func ParseVeilURL(rawURL string) (*SimpleConfig, error) {
	// 检查协议
	if !strings.HasPrefix(rawURL, "veil://") {
		return nil, fmt.Errorf("invalid protocol: must start with veil://")
	}

	// 解析URL
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	// 提取用户信息
	if u.User == nil {
		return nil, fmt.Errorf("missing credentials in URL")
	}

	method := u.User.Username()
	password, _ := u.User.Password()

	if password == "" {
		return nil, fmt.Errorf("missing password in URL")
	}

	// 提取服务器地址
	if u.Host == "" {
		return nil, fmt.Errorf("missing server address in URL")
	}

	// 创建基础配置
	config := &SimpleConfig{
		Server:   u.Host,
		Password: password,
		Mode:     "client",
		Advanced: &AdvancedConfig{
			Cipher: method,
		},
	}

	// 解析查询参数
	query := u.Query()

	if obfs := query.Get("obfs"); obfs != "" {
		config.Advanced.Obfuscation = obfs
	}

	if cdn := query.Get("cdn"); cdn != "" {
		config.Advanced.CDN = cdn
	}

	if portHop := query.Get("port_hop"); portHop == "true" {
		config.Advanced.PortHopping = true
	}

	if fallback := query.Get("fallback"); fallback == "true" {
		config.Advanced.Fallback = true
	}

	if pfs := query.Get("pfs"); pfs == "true" || pfs == "" {
		config.Advanced.PFS = true
	}

	if zeroRTT := query.Get("zero_rtt"); zeroRTT == "true" || zeroRTT == "" {
		config.Advanced.ZeroRTT = true
	}

	if mtu := query.Get("mtu"); mtu != "" {
		if mtuInt, err := strconv.Atoi(mtu); err == nil {
			config.Advanced.MTU = mtuInt
		}
	}

	return config, nil
}

// EncodeVeilURL 编码为 veil:// URL
func EncodeVeilURL(config *SimpleConfig) (string, error) {
	if config.Server == "" {
		return "", fmt.Errorf("server is required")
	}

	if config.Password == "" {
		return "", fmt.Errorf("password is required")
	}

	// 确定加密方法
	method := "chacha20"
	if config.Advanced != nil && config.Advanced.Cipher != "" {
		method = config.Advanced.Cipher
	}

	// 构建URL
	u := &url.URL{
		Scheme: "veil",
		User:   url.UserPassword(method, config.Password),
		Host:   config.Server,
	}

	// 添加查询参数
	query := url.Values{}

	if config.Advanced != nil {
		adv := config.Advanced

		if adv.Obfuscation != "" && adv.Obfuscation != "none" {
			query.Set("obfs", adv.Obfuscation)
		}

		if adv.CDN != "" {
			query.Set("cdn", adv.CDN)
		}

		if adv.PortHopping {
			query.Set("port_hop", "true")
		}

		if adv.Fallback {
			query.Set("fallback", "true")
		}

		if adv.PFS {
			query.Set("pfs", "true")
		}

		if adv.ZeroRTT {
			query.Set("zero_rtt", "true")
		}

		if adv.MTU != 0 && adv.MTU != 1420 {
			query.Set("mtu", strconv.Itoa(adv.MTU))
		}
	}

	u.RawQuery = query.Encode()

	return u.String(), nil
}

// ParseBase64URL 解析 Base64 编码的URL
// 用于二维码分享
func ParseBase64URL(encoded string) (*SimpleConfig, error) {
	// 去除可能的前缀
	encoded = strings.TrimPrefix(encoded, "veil://")

	// Base64解码
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		// 尝试URL安全的Base64
		decoded, err = base64.URLEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64: %w", err)
		}
	}

	// 解析URL
	return ParseVeilURL("veil://" + string(decoded))
}

// EncodeBase64URL 编码为 Base64 URL
func EncodeBase64URL(config *SimpleConfig) (string, error) {
	// 生成URL
	rawURL, err := EncodeVeilURL(config)
	if err != nil {
		return "", err
	}

	// 去除协议前缀
	rawURL = strings.TrimPrefix(rawURL, "veil://")

	// Base64编码
	encoded := base64.StdEncoding.EncodeToString([]byte(rawURL))

	return "veil://" + encoded, nil
}

// GenerateQRCode 生成二维码内容
func GenerateQRCode(config *SimpleConfig) (string, error) {
	return EncodeBase64URL(config)
}

// ParseQRCode 解析二维码
func ParseQRCode(qrData string) (*SimpleConfig, error) {
	// 尝试直接解析
	if strings.HasPrefix(qrData, "veil://") {
		if config, err := ParseVeilURL(qrData); err == nil {
			return config, nil
		}

		// 尝试Base64解码
		return ParseBase64URL(qrData)
	}

	return nil, fmt.Errorf("invalid QR code data")
}

// ShareableLink 生成可分享的链接
type ShareableLink struct {
	URL         string `json:"url"`          // 完整URL
	ShortURL    string `json:"short_url"`    // 短URL
	QRCode      string `json:"qr_code"`      // 二维码内容
	DisplayText string `json:"display_text"` // 显示文本
}

// GenerateShareableLink 生成可分享链接
func GenerateShareableLink(config *SimpleConfig) (*ShareableLink, error) {
	// 生成URL
	fullURL, err := EncodeVeilURL(config)
	if err != nil {
		return nil, err
	}

	// 生成QR码
	qrCode, err := GenerateQRCode(config)
	if err != nil {
		return nil, err
	}

	// 生成显示文本
	displayText := fmt.Sprintf(`VeilDeploy 连接配置

服务器: %s
加密: %s
混淆: %s

快速连接:
%s

或扫描二维码连接
`, config.Server,
		getDisplayCipher(config),
		getDisplayObfuscation(config),
		fullURL,
	)

	return &ShareableLink{
		URL:         fullURL,
		QRCode:      qrCode,
		DisplayText: displayText,
	}, nil
}

func getDisplayCipher(config *SimpleConfig) string {
	if config.Advanced != nil && config.Advanced.Cipher != "" {
		return config.Advanced.Cipher
	}
	return "chacha20"
}

func getDisplayObfuscation(config *SimpleConfig) string {
	if config.Advanced != nil && config.Advanced.Obfuscation != "" {
		return config.Advanced.Obfuscation
	}
	return "none"
}

// ImportFromURL 从URL字符串导入配置
func ImportFromURL(urlString string) (*SimpleConfig, error) {
	// 支持多种格式
	urlString = strings.TrimSpace(urlString)

	// 1. 标准 veil:// URL
	if strings.HasPrefix(urlString, "veil://") {
		// 检查是否是Base64编码
		if !strings.Contains(urlString, "@") {
			return ParseBase64URL(urlString)
		}
		return ParseVeilURL(urlString)
	}

	// 2. Shadowsocks格式兼容 (ss://)
	if strings.HasPrefix(urlString, "ss://") {
		return importFromShadowsocks(urlString)
	}

	// 3. V2Ray格式兼容 (vmess://)
	if strings.HasPrefix(urlString, "vmess://") {
		return importFromVmess(urlString)
	}

	// 4. 普通URL（自动添加veil://前缀）
	if !strings.Contains(urlString, "://") {
		return ParseVeilURL("veil://" + urlString)
	}

	return nil, fmt.Errorf("unsupported URL format")
}

// importFromShadowsocks 从Shadowsocks URL导入
func importFromShadowsocks(ssURL string) (*SimpleConfig, error) {
	// ss://base64(method:password)@server:port
	ssURL = strings.TrimPrefix(ssURL, "ss://")

	// 分离服务器部分
	parts := strings.SplitN(ssURL, "@", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid shadowsocks URL")
	}

	// 解码认证信息
	authData, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		authData, err = base64.URLEncoding.DecodeString(parts[0])
		if err != nil {
			return nil, fmt.Errorf("failed to decode auth: %w", err)
		}
	}

	// 分离方法和密码
	authParts := strings.SplitN(string(authData), ":", 2)
	if len(authParts) != 2 {
		return nil, fmt.Errorf("invalid auth format")
	}

	method := authParts[0]
	password := authParts[1]

	// 映射加密方法
	cipherMap := map[string]string{
		"aes-256-gcm":        "aes256",
		"chacha20-poly1305":  "chacha20",
		"chacha20-ietf-poly1305": "chacha20",
		"xchacha20-poly1305": "xchacha20",
	}

	cipher := cipherMap[method]
	if cipher == "" {
		cipher = "chacha20" // 默认
	}

	return &SimpleConfig{
		Server:   parts[1],
		Password: password,
		Mode:     "client",
		Advanced: &AdvancedConfig{
			Cipher: cipher,
		},
	}, nil
}

// importFromVmess 从V2Ray VMess URL导入
func importFromVmess(vmessURL string) (*SimpleConfig, error) {
	// vmess://base64(json)
	vmessURL = strings.TrimPrefix(vmessURL, "vmess://")

	// Base64解码
	jsonData, err := base64.StdEncoding.DecodeString(vmessURL)
	if err != nil {
		jsonData, err = base64.URLEncoding.DecodeString(vmessURL)
		if err != nil {
			return nil, fmt.Errorf("failed to decode vmess: %w", err)
		}
	}

	// 简化：只提取基本信息
	// 实际应该解析JSON
	// 这里返回错误，提示用户使用veil://格式
	return nil, fmt.Errorf("vmess format not fully supported, please use veil:// format")
}

// ExportToClipboard 导出到剪贴板格式
func ExportToClipboard(config *SimpleConfig) (string, error) {
	url, err := EncodeVeilURL(config)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`VeilDeploy配置

%s

复制此链接或使用命令:
veildeploy connect "%s"
`, url, url), nil
}
