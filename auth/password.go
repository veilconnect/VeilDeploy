package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// PasswordAuth 密码认证器
type PasswordAuth struct {
	db          UserDatabase
	maxRetries  int
	lockoutTime time.Duration
	mu          sync.RWMutex
	failedLogins map[string]*LoginAttempts
}

// PasswordUser 密码认证用户信息
type PasswordUser struct {
	Username     string            `json:"username"`
	PasswordHash string            `json:"password_hash"` // bcrypt hash
	Email        string            `json:"email,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	LastLogin    time.Time         `json:"last_login,omitempty"`
	Enabled      bool              `json:"enabled"`
	Roles        []string          `json:"roles,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`

	// 2FA相关
	TwoFactorEnabled bool   `json:"2fa_enabled"`
	TOTPSecret       string `json:"totp_secret,omitempty"`
}

// UserDatabase 用户数据库接口
type UserDatabase interface {
	GetUser(username string) (*PasswordUser, error)
	CreateUser(user *PasswordUser) error
	UpdateUser(user *PasswordUser) error
	DeleteUser(username string) error
	ListUsers() ([]*PasswordUser, error)
}

// PasswordCredentials 密码凭据
type PasswordCredentials struct {
	Username  string
	Password  string
	TOTPToken string // 2FA令牌（可选）
}

// LoginAttempts 登录尝试记录
type LoginAttempts struct {
	Count      int
	LastAttempt time.Time
	LockedUntil time.Time
}

// NewPasswordAuth 创建密码认证器
func NewPasswordAuth(db UserDatabase, maxRetries int, lockoutTime time.Duration) *PasswordAuth {
	return &PasswordAuth{
		db:           db,
		maxRetries:   maxRetries,
		lockoutTime:  lockoutTime,
		failedLogins: make(map[string]*LoginAttempts),
	}
}

// Authenticate 认证用户
func (pa *PasswordAuth) Authenticate(credentials interface{}) (bool, error) {
	creds, ok := credentials.(*PasswordCredentials)
	if !ok {
		return false, fmt.Errorf("invalid credentials type")
	}

	// 检查用户是否被锁定
	if pa.isLocked(creds.Username) {
		return false, ErrUserLocked
	}

	// 从数据库获取用户
	user, err := pa.db.GetUser(creds.Username)
	if err != nil {
		pa.recordFailedAttempt(creds.Username)
		return false, ErrInvalidCredentials
	}

	// 检查用户是否启用
	if !user.Enabled {
		return false, ErrUserDisabled
	}

	// 验证密码
	if err := bcrypt.CompareHashAndPassword(
		[]byte(user.PasswordHash),
		[]byte(creds.Password),
	); err != nil {
		pa.recordFailedAttempt(creds.Username)
		return false, ErrInvalidCredentials
	}

	// 检查2FA
	if user.TwoFactorEnabled {
		if creds.TOTPToken == "" {
			return false, ErrMissing2FA
		}

		if !VerifyTOTP(user.TOTPSecret, creds.TOTPToken) {
			pa.recordFailedAttempt(creds.Username)
			return false, ErrInvalid2FA
		}
	}

	// 认证成功，清除失败记录
	pa.clearFailedAttempts(creds.Username)

	// 更新最后登录时间
	user.LastLogin = time.Now()
	pa.db.UpdateUser(user)

	return true, nil
}

// CreateUser 创建用户
func (pa *PasswordAuth) CreateUser(username, password, email string) (*PasswordUser, error) {
	// 验证密码强度
	if err := ValidatePasswordStrength(password); err != nil {
		return nil, err
	}

	// 生成密码哈希
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	user := &PasswordUser{
		Username:     username,
		PasswordHash: string(hash),
		Email:        email,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Enabled:      true,
		Roles:        []string{"user"},
		Metadata:     make(map[string]string),
	}

	if err := pa.db.CreateUser(user); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return user, nil
}

// ChangePassword 修改密码
func (pa *PasswordAuth) ChangePassword(username, oldPassword, newPassword string) error {
	// 验证旧密码
	creds := &PasswordCredentials{
		Username: username,
		Password: oldPassword,
	}

	if valid, err := pa.Authenticate(creds); !valid || err != nil {
		return fmt.Errorf("old password is incorrect")
	}

	// 验证新密码强度
	if err := ValidatePasswordStrength(newPassword); err != nil {
		return err
	}

	// 生成新密码哈希
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// 更新用户
	user, err := pa.db.GetUser(username)
	if err != nil {
		return err
	}

	user.PasswordHash = string(hash)
	user.UpdatedAt = time.Now()

	return pa.db.UpdateUser(user)
}

// ResetPassword 重置密码（管理员操作）
func (pa *PasswordAuth) ResetPassword(username, newPassword string) error {
	// 验证密码强度
	if err := ValidatePasswordStrength(newPassword); err != nil {
		return err
	}

	// 生成密码哈希
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// 更新用户
	user, err := pa.db.GetUser(username)
	if err != nil {
		return err
	}

	user.PasswordHash = string(hash)
	user.UpdatedAt = time.Now()

	return pa.db.UpdateUser(user)
}

// Enable2FA 启用双因素认证
func (pa *PasswordAuth) Enable2FA(username string) (string, error) {
	user, err := pa.db.GetUser(username)
	if err != nil {
		return "", err
	}

	// 生成TOTP密钥
	secret, err := GenerateTOTPSecret()
	if err != nil {
		return "", fmt.Errorf("failed to generate TOTP secret: %w", err)
	}

	user.TwoFactorEnabled = true
	user.TOTPSecret = secret
	user.UpdatedAt = time.Now()

	if err := pa.db.UpdateUser(user); err != nil {
		return "", err
	}

	// 返回可供用户扫描的URI
	return GenerateTOTPURI(username, secret), nil
}

// Disable2FA 禁用双因素认证
func (pa *PasswordAuth) Disable2FA(username, password string) error {
	// 验证密码
	creds := &PasswordCredentials{
		Username: username,
		Password: password,
	}

	if valid, err := pa.Authenticate(creds); !valid || err != nil {
		return fmt.Errorf("password verification failed")
	}

	user, err := pa.db.GetUser(username)
	if err != nil {
		return err
	}

	user.TwoFactorEnabled = false
	user.TOTPSecret = ""
	user.UpdatedAt = time.Now()

	return pa.db.UpdateUser(user)
}

// isLocked 检查用户是否被锁定
func (pa *PasswordAuth) isLocked(username string) bool {
	pa.mu.RLock()
	defer pa.mu.RUnlock()

	attempts, exists := pa.failedLogins[username]
	if !exists {
		return false
	}

	return time.Now().Before(attempts.LockedUntil)
}

// recordFailedAttempt 记录失败尝试
func (pa *PasswordAuth) recordFailedAttempt(username string) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	attempts, exists := pa.failedLogins[username]
	if !exists {
		attempts = &LoginAttempts{}
		pa.failedLogins[username] = attempts
	}

	attempts.Count++
	attempts.LastAttempt = time.Now()

	// 达到最大重试次数，锁定账户
	if attempts.Count >= pa.maxRetries {
		attempts.LockedUntil = time.Now().Add(pa.lockoutTime)
	}
}

// clearFailedAttempts 清除失败记录
func (pa *PasswordAuth) clearFailedAttempts(username string) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	delete(pa.failedLogins, username)
}

// ValidatePasswordStrength 验证密码强度
func ValidatePasswordStrength(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}

	if len(password) > 128 {
		return fmt.Errorf("password too long (max 128 characters)")
	}

	// 检查是否包含大写字母
	hasUpper := false
	hasLower := false
	hasDigit := false
	hasSpecial := false

	for _, ch := range password {
		switch {
		case ch >= 'A' && ch <= 'Z':
			hasUpper = true
		case ch >= 'a' && ch <= 'z':
			hasLower = true
		case ch >= '0' && ch <= '9':
			hasDigit = true
		case (ch >= '!' && ch <= '/') || (ch >= ':' && ch <= '@') || (ch >= '[' && ch <= '`') || (ch >= '{' && ch <= '~'):
			hasSpecial = true
		}
	}

	if !hasUpper {
		return fmt.Errorf("password must contain at least one uppercase letter")
	}

	if !hasLower {
		return fmt.Errorf("password must contain at least one lowercase letter")
	}

	if !hasDigit {
		return fmt.Errorf("password must contain at least one digit")
	}

	if !hasSpecial {
		return fmt.Errorf("password must contain at least one special character")
	}

	return nil
}

// GenerateRandomPassword 生成随机密码
func GenerateRandomPassword(length int) (string, error) {
	if length < 8 {
		length = 8
	}

	const upper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	const lower = "abcdefghijklmnopqrstuvwxyz"
	const digits = "0123456789"
	const special = "!@#$%^&*"
	const all = upper + lower + digits + special

	password := make([]byte, length)

	// 确保至少包含一个大写字母、小写字母、数字和特殊字符
	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	password[0] = upper[int(randomBytes[0])%len(upper)]
	password[1] = lower[int(randomBytes[1])%len(lower)]
	password[2] = digits[int(randomBytes[2])%len(digits)]
	password[3] = special[int(randomBytes[3])%len(special)]

	// 填充剩余字符
	if length > 4 {
		remaining := make([]byte, length-4)
		if _, err := rand.Read(remaining); err != nil {
			return "", err
		}

		for i, b := range remaining {
			password[i+4] = all[int(b)%len(all)]
		}
	}

	// 随机打乱顺序
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	for i := range password {
		randomBytes := make([]byte, 1)
		rand.Read(randomBytes)
		j := int(randomBytes[0]) % len(password)
		password[i], password[j] = password[j], password[i]
	}

	return string(password), nil
}

// HashPassword 哈希密码（公开函数）
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword 验证密码
func VerifyPassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// SecureCompare 安全比较字符串（防止时序攻击）
func SecureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// EncodePassword 编码密码（用于存储）
func EncodePassword(password string) string {
	return base64.StdEncoding.EncodeToString([]byte(password))
}

// DecodePassword 解码密码
func DecodePassword(encoded string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

// 错误定义
var (
	ErrInvalidCredentials = fmt.Errorf("invalid username or password")
	ErrUserLocked         = fmt.Errorf("user account is locked")
	ErrUserDisabled       = fmt.Errorf("user account is disabled")
	ErrMissing2FA         = fmt.Errorf("2FA token required")
	ErrInvalid2FA         = fmt.Errorf("invalid 2FA token")
)
