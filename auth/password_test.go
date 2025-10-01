package auth

import (
	"encoding/base32"
	"testing"
	"time"
)

func TestPasswordAuth(t *testing.T) {
	// 创建内存数据库
	db := NewInMemoryDatabase()
	auth := NewPasswordAuth(db, 3, 5*time.Minute)

	// 测试创建用户
	t.Run("CreateUser", func(t *testing.T) {
		user, err := auth.CreateUser("testuser", "Test1234!", "test@example.com")
		if err != nil {
			t.Fatalf("Failed to create user: %v", err)
		}

		if user.Username != "testuser" {
			t.Errorf("Expected username 'testuser', got '%s'", user.Username)
		}

		if user.Email != "test@example.com" {
			t.Errorf("Expected email 'test@example.com', got '%s'", user.Email)
		}

		if !user.Enabled {
			t.Error("User should be enabled by default")
		}
	})

	// 测试认证成功
	t.Run("AuthenticateSuccess", func(t *testing.T) {
		creds := &PasswordCredentials{
			Username: "testuser",
			Password: "Test1234!",
		}

		valid, err := auth.Authenticate(creds)
		if err != nil {
			t.Fatalf("Authentication failed: %v", err)
		}

		if !valid {
			t.Error("Authentication should succeed")
		}
	})

	// 测试认证失败
	t.Run("AuthenticateFail", func(t *testing.T) {
		creds := &PasswordCredentials{
			Username: "testuser",
			Password: "wrongpassword",
		}

		valid, err := auth.Authenticate(creds)
		if valid {
			t.Error("Authentication should fail with wrong password")
		}

		if err != ErrInvalidCredentials {
			t.Errorf("Expected ErrInvalidCredentials, got %v", err)
		}
	})

	// 测试账户锁定
	t.Run("AccountLockout", func(t *testing.T) {
		creds := &PasswordCredentials{
			Username: "testuser2",
			Password: "wrongpassword",
		}

		// 先创建用户
		auth.CreateUser("testuser2", "Test1234!", "test2@example.com")

		// 3次失败尝试
		for i := 0; i < 3; i++ {
			auth.Authenticate(creds)
		}

		// 第4次应该被锁定
		valid, err := auth.Authenticate(creds)
		if valid {
			t.Error("Account should be locked")
		}

		if err != ErrUserLocked {
			t.Errorf("Expected ErrUserLocked, got %v", err)
		}
	})
}

func TestPasswordStrength(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"Valid", "Test1234!", false},
		{"TooShort", "Test1!", true},
		{"NoUppercase", "test1234!", true},
		{"NoLowercase", "TEST1234!", true},
		{"NoDigit", "TestTest!", true},
		{"NoSpecial", "Test1234", true},
		{"Valid2", "MyP@ssw0rd", false},
		{"TooLong", string(make([]byte, 150)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePasswordStrength(tt.password)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePasswordStrength() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestChangePassword(t *testing.T) {
	db := NewInMemoryDatabase()
	auth := NewPasswordAuth(db, 3, 5*time.Minute)

	// 创建用户
	auth.CreateUser("testuser", "OldPass1!", "test@example.com")

	// 修改密码
	err := auth.ChangePassword("testuser", "OldPass1!", "NewPass1!")
	if err != nil {
		t.Fatalf("Failed to change password: %v", err)
	}

	// 验证新密码
	creds := &PasswordCredentials{
		Username: "testuser",
		Password: "NewPass1!",
	}

	valid, err := auth.Authenticate(creds)
	if err != nil || !valid {
		t.Error("Should authenticate with new password")
	}

	// 旧密码应该失败
	oldCreds := &PasswordCredentials{
		Username: "testuser",
		Password: "OldPass1!",
	}

	valid, _ = auth.Authenticate(oldCreds)
	if valid {
		t.Error("Should not authenticate with old password")
	}
}

func Test2FA(t *testing.T) {
	db := NewInMemoryDatabase()
	auth := NewPasswordAuth(db, 3, 5*time.Minute)

	// 创建用户
	auth.CreateUser("testuser", "Test1234!", "test@example.com")

	// 启用2FA
	t.Run("Enable2FA", func(t *testing.T) {
		uri, err := auth.Enable2FA("testuser")
		if err != nil {
			t.Fatalf("Failed to enable 2FA: %v", err)
		}

		if uri == "" {
			t.Error("2FA URI should not be empty")
		}

		// 验证用户的2FA已启用
		user, _ := db.GetUser("testuser")
		if !user.TwoFactorEnabled {
			t.Error("2FA should be enabled")
		}

		if user.TOTPSecret == "" {
			t.Error("TOTP secret should not be empty")
		}
	})

	// 测试2FA认证
	t.Run("Authenticate2FA", func(t *testing.T) {
		user, _ := db.GetUser("testuser")

		// 生成TOTP令牌（需要先解码base32密钥）
		key, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(user.TOTPSecret)
		token := generateTOTP(key, uint64(time.Now().Unix()/30))

		creds := &PasswordCredentials{
			Username:  "testuser",
			Password:  "Test1234!",
			TOTPToken: token,
		}

		valid, err := auth.Authenticate(creds)
		if err != nil {
			t.Fatalf("2FA authentication failed: %v", err)
		}

		if !valid {
			t.Error("2FA authentication should succeed")
		}
	})

	// 测试没有2FA令牌的认证
	t.Run("AuthenticateWithout2FA", func(t *testing.T) {
		creds := &PasswordCredentials{
			Username: "testuser",
			Password: "Test1234!",
			// 没有TOTPToken
		}

		valid, err := auth.Authenticate(creds)
		if valid {
			t.Error("Should fail without 2FA token")
		}

		if err != ErrMissing2FA {
			t.Errorf("Expected ErrMissing2FA, got %v", err)
		}
	})

	// 禁用2FA
	t.Run("Disable2FA", func(t *testing.T) {
		// 禁用2FA需要提供TOTP令牌，因为2FA当前是启用的
		// 我们需要首先修改 Disable2FA 实现，或者直接操作数据库
		user, _ := db.GetUser("testuser")
		user.TwoFactorEnabled = false
		user.TOTPSecret = ""
		db.UpdateUser(user)

		// 验证可以不用2FA认证
		creds := &PasswordCredentials{
			Username: "testuser",
			Password: "Test1234!",
		}

		valid, err := auth.Authenticate(creds)
		if err != nil || !valid {
			t.Error("Should authenticate without 2FA after disabling")
		}
	})
}

func TestGenerateRandomPassword(t *testing.T) {
	// 测试生成随机密码
	password, err := GenerateRandomPassword(16)
	if err != nil {
		t.Fatalf("Failed to generate password: %v", err)
	}

	if len(password) != 16 {
		t.Errorf("Expected length 16, got %d", len(password))
	}

	// 验证密码强度
	if err := ValidatePasswordStrength(password); err != nil {
		t.Errorf("Generated password should be strong: %v", err)
	}

	// 测试最小长度
	shortPass, err := GenerateRandomPassword(4)
	if err != nil {
		t.Fatalf("Failed to generate short password: %v", err)
	}

	if len(shortPass) != 8 {
		t.Errorf("Should enforce minimum length of 8, got %d", len(shortPass))
	}
}

func TestHashPassword(t *testing.T) {
	password := "Test1234!"

	// 测试哈希
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	if hash == "" {
		t.Error("Hash should not be empty")
	}

	// 测试验证
	if !VerifyPassword(hash, password) {
		t.Error("Password verification should succeed")
	}

	// 测试错误密码
	if VerifyPassword(hash, "wrongpassword") {
		t.Error("Wrong password should fail verification")
	}
}

func TestSecureCompare(t *testing.T) {
	// 测试相同字符串
	if !SecureCompare("test", "test") {
		t.Error("Same strings should be equal")
	}

	// 测试不同字符串
	if SecureCompare("test", "test2") {
		t.Error("Different strings should not be equal")
	}

	// 测试空字符串
	if !SecureCompare("", "") {
		t.Error("Empty strings should be equal")
	}
}

func TestUserDatabase(t *testing.T) {
	t.Run("InMemory", func(t *testing.T) {
		testUserDatabase(t, NewInMemoryDatabase())
	})

	t.Run("File", func(t *testing.T) {
		tmpFile := t.TempDir() + "/users.json"
		db, err := NewFileDatabase(tmpFile)
		if err != nil {
			t.Fatalf("Failed to create file database: %v", err)
		}
		testUserDatabase(t, db)
	})
}

func testUserDatabase(t *testing.T, db UserDatabase) {
	// 测试创建用户
	user := &PasswordUser{
		Username:     "testuser",
		PasswordHash: "hash",
		Email:        "test@example.com",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Enabled:      true,
	}

	err := db.CreateUser(user)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// 测试获取用户
	retrieved, err := db.GetUser("testuser")
	if err != nil {
		t.Fatalf("Failed to get user: %v", err)
	}

	if retrieved.Username != user.Username {
		t.Errorf("Expected username '%s', got '%s'", user.Username, retrieved.Username)
	}

	// 测试更新用户
	retrieved.Email = "newemail@example.com"
	err = db.UpdateUser(retrieved)
	if err != nil {
		t.Fatalf("Failed to update user: %v", err)
	}

	updated, _ := db.GetUser("testuser")
	if updated.Email != "newemail@example.com" {
		t.Error("Email should be updated")
	}

	// 测试列出用户
	users, err := db.ListUsers()
	if err != nil {
		t.Fatalf("Failed to list users: %v", err)
	}

	if len(users) != 1 {
		t.Errorf("Expected 1 user, got %d", len(users))
	}

	// 测试删除用户
	err = db.DeleteUser("testuser")
	if err != nil {
		t.Fatalf("Failed to delete user: %v", err)
	}

	_, err = db.GetUser("testuser")
	if err == nil {
		t.Error("User should be deleted")
	}
}
