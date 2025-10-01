# VeilDeploy 认证系统

VeilDeploy 提供多种认证机制，支持从简单的密码认证到企业级的证书和多因素认证。

## 认证模块

### 1. 密码认证 (Password Authentication)

**文件**: `password.go`, `totp.go`, `database.go`

基于 bcrypt 的密码认证系统，支持：

#### 核心功能
- ✅ **安全密码存储**: 使用 bcrypt 哈希算法（成本因子 10）
- ✅ **密码强度验证**:
  - 最少8个字符，最多128个字符
  - 必须包含大写字母、小写字母、数字和特殊字符
- ✅ **账户锁定**: 失败3次后锁定5分钟，防止暴力破解
- ✅ **双因素认证（2FA）**: 基于 TOTP（Time-based One-Time Password）
- ✅ **用户管理**: 创建、更新、删除用户
- ✅ **角色管理**: 支持多角色授权

#### 使用示例

```go
// 创建密码认证器
db := auth.NewInMemoryDatabase()
passwordAuth := auth.NewPasswordAuth(db, 3, 5*time.Minute)

// 创建用户
user, err := passwordAuth.CreateUser("admin", "MyP@ssw0rd", "admin@example.com")

// 认证用户
creds := &auth.PasswordCredentials{
    Username: "admin",
    Password: "MyP@ssw0rd",
}
valid, err := passwordAuth.Authenticate(creds)

// 启用2FA
uri, err := passwordAuth.Enable2FA("admin")
// 用户扫描 uri 生成的二维码

// 使用2FA认证
creds.TOTPToken = "123456" // 从验证器获取
valid, err = passwordAuth.Authenticate(creds)
```

### 2. TOTP 双因素认证

**文件**: `totp.go`

符合 RFC 6238 标准的 TOTP 实现：

#### 特性
- ✅ **标准兼容**: 与 Google Authenticator、Authy 等应用兼容
- ✅ **时间窗口**: 30秒周期，±1个窗口容错（防时钟偏移）
- ✅ **备用码**: 10个一次性备用恢复码
- ✅ **速率限制**: 防止暴力破解2FA令牌
- ✅ **QR码生成**: 生成 otpauth:// URI 用于二维码

#### TOTP 配置

```go
// 生成TOTP密钥
secret, err := auth.GenerateTOTPSecret()

// 生成URI（用于生成二维码）
uri := auth.GenerateTOTPURI("user@example.com", secret)

// 验证TOTP令牌
valid := auth.VerifyTOTP(secret, "123456")

// 生成备用码
manager := auth.NewTOTPManager()
backupCodes, err := manager.GenerateBackupCodes("username")

// 使用备用码
valid := manager.VerifyBackupCode("username", backupCode)
```

### 3. 用户数据库

**文件**: `database.go`

提供两种用户存储实现：

#### InMemoryDatabase
用于测试和开发：

```go
db := auth.NewInMemoryDatabase()
```

#### FileDatabase
基于 JSON 文件的持久化存储：

```go
db, err := auth.NewFileDatabase("/path/to/users.json")

// 备份数据库
err = db.Backup("/path/to/backup.json")

// 从备份恢复
err = db.Restore("/path/to/backup.json")

// 获取统计信息
stats, err := db.GetStats()
// stats.TotalUsers, stats.EnabledUsers, stats.Users2FA
```

## 安全特性

### 密码安全
- **Bcrypt 哈希**: 计算成本可配置，默认 cost=10
- **防时序攻击**: 使用 `subtle.ConstantTimeCompare`
- **密码强度**: 强制执行复杂度要求
- **安全随机**: 使用 `crypto/rand` 生成密钥和 salt

### 账户保护
- **账户锁定**: 失败尝试后自动锁定
- **用户禁用**: 管理员可禁用账户
- **会话管理**: 最后登录时间跟踪

### 2FA 安全
- **HMAC-SHA1**: 标准 TOTP 算法
- **时间窗口**: 防止时钟偏移问题
- **备用码**: 一次性使用，用于紧急访问
- **速率限制**: 5次失败后锁定5分钟

## 数据模型

### PasswordUser

```go
type PasswordUser struct {
    Username     string
    PasswordHash string    // bcrypt 哈希
    Email        string
    CreatedAt    time.Time
    UpdatedAt    time.Time
    LastLogin    time.Time
    Enabled      bool
    Roles        []string
    Metadata     map[string]string

    // 2FA
    TwoFactorEnabled bool
    TOTPSecret       string
}
```

### PasswordCredentials

```go
type PasswordCredentials struct {
    Username  string
    Password  string
    TOTPToken string // 可选，2FA启用时必需
}
```

## 测试覆盖

完整的测试套件 (`password_test.go`):

- ✅ 密码认证流程
- ✅ 账户锁定机制
- ✅ 密码强度验证
- ✅ 密码修改
- ✅ 2FA 启用/禁用
- ✅ 2FA 认证
- ✅ 用户数据库操作
- ✅ 随机密码生成

运行测试：
```bash
go test -v ./auth -run "TestPassword|Test2FA"
```

## 性能

- **Bcrypt 成本**: Cost=10，约 100ms/hash（适合认证场景）
- **TOTP 验证**: <1ms
- **内存数据库**: O(1) 查询
- **文件数据库**: 启动时加载到内存，写入时持久化

## 未来改进

1. **证书认证**: 支持 X.509 客户端证书
2. **LDAP/RADIUS**: 企业目录集成
3. **OAuth2/OIDC**: 第三方身份提供商
4. **WebAuthn**: FIDO2 硬件密钥支持
5. **审计日志**: 详细的认证事件记录
6. **会话管理**: Token 刷新和撤销

## 最佳实践

### 生产环境配置

```go
// 使用文件数据库
db, err := auth.NewFileDatabase("/var/lib/veildeploy/users.json")

// 配置账户锁定
maxRetries := 5
lockoutTime := 15 * time.Minute
auth := auth.NewPasswordAuth(db, maxRetries, lockoutTime)

// 定期备份
go func() {
    ticker := time.NewTicker(1 * time.Hour)
    for range ticker.C {
        db.Backup("/var/backups/veildeploy/users.json")
    }
}()
```

### 安全建议

1. **强密码策略**: 使用 `ValidatePasswordStrength` 验证
2. **启用2FA**: 为管理员账户强制启用
3. **定期备份**: 自动备份用户数据库
4. **监控失败登录**: 检测异常认证活动
5. **安全存储**: 确保数据库文件权限 0600
6. **HTTPS传输**: 密码必须通过加密通道传输

## 兼容性

- **Go 版本**: 1.19+
- **依赖**:
  - `golang.org/x/crypto/bcrypt` - 密码哈希
  - `encoding/base32` - TOTP 密钥编码
  - `crypto/hmac` - TOTP HMAC

## 参考文档

- RFC 6238: TOTP: Time-Based One-Time Password Algorithm
- RFC 4648: The Base16, Base32, and Base64 Data Encodings
- OWASP Password Storage Cheat Sheet
- bcrypt: A Future-Adaptable Password Scheme
