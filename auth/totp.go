package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"math"
	"net/url"
	"time"
)

// TOTP参数
const (
	TOTPDigits     = 6
	TOTPPeriod     = 30 // 30秒
	TOTPSkew       = 1  // 允许前后1个时间窗口
	TOTPSecretSize = 20 // 160 bits
)

// GenerateTOTPSecret 生成TOTP密钥
func GenerateTOTPSecret() (string, error) {
	secret := make([]byte, TOTPSecretSize)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("failed to generate secret: %w", err)
	}

	// Base32编码（去除填充）
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	return encoded, nil
}

// VerifyTOTP 验证TOTP令牌
func VerifyTOTP(secret, token string) bool {
	// 解码密钥
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return false
	}

	// 当前时间窗口
	counter := uint64(time.Now().Unix() / TOTPPeriod)

	// 检查当前和前后时间窗口（防时钟偏移）
	for i := -TOTPSkew; i <= TOTPSkew; i++ {
		testCounter := counter + uint64(i)
		if generateTOTP(key, testCounter) == token {
			return true
		}
	}

	return false
}

// generateTOTP 生成TOTP令牌
func generateTOTP(key []byte, counter uint64) string {
	// HMAC-SHA1
	h := hmac.New(sha1.New, key)

	// 写入counter（大端序）
	counterBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(counterBytes, counter)
	h.Write(counterBytes)

	hash := h.Sum(nil)

	// 动态截断
	offset := hash[len(hash)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(hash[offset:offset+4]) & 0x7fffffff

	// 取模得到6位数字
	code := truncated % uint32(math.Pow10(TOTPDigits))

	// 格式化为6位字符串
	return fmt.Sprintf("%0*d", TOTPDigits, code)
}

// GenerateTOTPURI 生成TOTP URI（用于二维码）
// 格式: otpauth://totp/VeilDeploy:username?secret=SECRET&issuer=VeilDeploy
func GenerateTOTPURI(username, secret string) string {
	u := url.URL{
		Scheme: "otpauth",
		Host:   "totp",
		Path:   fmt.Sprintf("/VeilDeploy:%s", username),
	}

	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", "VeilDeploy")
	query.Set("algorithm", "SHA1")
	query.Set("digits", fmt.Sprintf("%d", TOTPDigits))
	query.Set("period", fmt.Sprintf("%d", TOTPPeriod))

	u.RawQuery = query.Encode()

	return u.String()
}

// ValidateTOTPSecret 验证TOTP密钥格式
func ValidateTOTPSecret(secret string) error {
	if secret == "" {
		return fmt.Errorf("secret cannot be empty")
	}

	// 尝试解码
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return fmt.Errorf("invalid secret format: %w", err)
	}

	// 检查长度
	if len(decoded) < 16 {
		return fmt.Errorf("secret too short (minimum 128 bits)")
	}

	return nil
}

// GenerateTOTPBackupCodes 生成备用恢复码
func GenerateTOTPBackupCodes(count int) ([]string, error) {
	if count <= 0 {
		count = 10
	}

	codes := make([]string, count)
	for i := 0; i < count; i++ {
		// 生成8字节随机数
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}

		// 格式化为8位十六进制
		codes[i] = fmt.Sprintf("%016x", binary.BigEndian.Uint64(b))
	}

	return codes, nil
}

// TOTPManager 2FA管理器
type TOTPManager struct {
	backupCodes map[string][]string // username -> backup codes
}

// NewTOTPManager 创建2FA管理器
func NewTOTPManager() *TOTPManager {
	return &TOTPManager{
		backupCodes: make(map[string][]string),
	}
}

// GenerateBackupCodes 为用户生成备用码
func (tm *TOTPManager) GenerateBackupCodes(username string) ([]string, error) {
	codes, err := GenerateTOTPBackupCodes(10)
	if err != nil {
		return nil, err
	}

	tm.backupCodes[username] = codes
	return codes, nil
}

// VerifyBackupCode 验证备用码（一次性使用）
func (tm *TOTPManager) VerifyBackupCode(username, code string) bool {
	codes, exists := tm.backupCodes[username]
	if !exists {
		return false
	}

	// 查找并移除使用过的备用码
	for i, c := range codes {
		if c == code {
			// 删除已使用的备用码
			tm.backupCodes[username] = append(codes[:i], codes[i+1:]...)
			return true
		}
	}

	return false
}

// GetRemainingBackupCodes 获取剩余备用码数量
func (tm *TOTPManager) GetRemainingBackupCodes(username string) int {
	codes, exists := tm.backupCodes[username]
	if !exists {
		return 0
	}
	return len(codes)
}

// TOTPConfig TOTP配置
type TOTPConfig struct {
	Enabled      bool   `json:"enabled"`
	Secret       string `json:"secret,omitempty"`
	BackupCodes  int    `json:"backup_codes"`  // 剩余备用码数量
	LastUsed     int64  `json:"last_used"`     // 最后使用时间
	FailedCount  int    `json:"failed_count"`  // 连续失败次数
	LockedUntil  int64  `json:"locked_until"`  // 锁定到什么时候
}

// VerifyWithRateLimit 带速率限制的验证
func (tc *TOTPConfig) VerifyWithRateLimit(secret, token string, maxFailures int) (bool, error) {
	// 检查是否被锁定
	if tc.LockedUntil > time.Now().Unix() {
		return false, fmt.Errorf("2FA temporarily locked due to too many failures")
	}

	// 验证令牌
	if !VerifyTOTP(secret, token) {
		tc.FailedCount++

		// 达到最大失败次数，锁定5分钟
		if tc.FailedCount >= maxFailures {
			tc.LockedUntil = time.Now().Add(5 * time.Minute).Unix()
			return false, fmt.Errorf("2FA locked for 5 minutes due to too many failures")
		}

		return false, fmt.Errorf("invalid 2FA token")
	}

	// 验证成功，重置失败计数
	tc.FailedCount = 0
	tc.LastUsed = time.Now().Unix()

	return true, nil
}
