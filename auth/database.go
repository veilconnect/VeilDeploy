package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// InMemoryDatabase 内存用户数据库（用于测试）
type InMemoryDatabase struct {
	mu    sync.RWMutex
	users map[string]*PasswordUser
}

// NewInMemoryDatabase 创建内存数据库
func NewInMemoryDatabase() *InMemoryDatabase {
	return &InMemoryDatabase{
		users: make(map[string]*PasswordUser),
	}
}

// GetUser 获取用户
func (db *InMemoryDatabase) GetUser(username string) (*PasswordUser, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	user, exists := db.users[username]
	if !exists {
		return nil, fmt.Errorf("user not found: %s", username)
	}

	return user, nil
}

// CreateUser 创建用户
func (db *InMemoryDatabase) CreateUser(user *PasswordUser) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.users[user.Username]; exists {
		return fmt.Errorf("user already exists: %s", user.Username)
	}

	db.users[user.Username] = user
	return nil
}

// UpdateUser 更新用户
func (db *InMemoryDatabase) UpdateUser(user *PasswordUser) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.users[user.Username]; !exists {
		return fmt.Errorf("user not found: %s", user.Username)
	}

	db.users[user.Username] = user
	return nil
}

// DeleteUser 删除用户
func (db *InMemoryDatabase) DeleteUser(username string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.users[username]; !exists {
		return fmt.Errorf("user not found: %s", username)
	}

	delete(db.users, username)
	return nil
}

// ListUsers 列出所有用户
func (db *InMemoryDatabase) ListUsers() ([]*PasswordUser, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	users := make([]*PasswordUser, 0, len(db.users))
	for _, user := range db.users {
		users = append(users, user)
	}

	return users, nil
}

// FileDatabase 文件用户数据库
type FileDatabase struct {
	mu       sync.RWMutex
	filePath string
	users    map[string]*PasswordUser
}

// NewFileDatabase 创建文件数据库
func NewFileDatabase(filePath string) (*FileDatabase, error) {
	db := &FileDatabase{
		filePath: filePath,
		users:    make(map[string]*PasswordUser),
	}

	// 加载现有数据
	if err := db.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load database: %w", err)
	}

	return db, nil
}

// load 从文件加载用户数据
func (db *FileDatabase) load() error {
	data, err := os.ReadFile(db.filePath)
	if err != nil {
		return err
	}

	var users []*PasswordUser
	if err := json.Unmarshal(data, &users); err != nil {
		return fmt.Errorf("failed to unmarshal users: %w", err)
	}

	// 构建索引
	for _, user := range users {
		db.users[user.Username] = user
	}

	return nil
}

// save 保存用户数据到文件
func (db *FileDatabase) save() error {
	// 转换为数组
	users := make([]*PasswordUser, 0, len(db.users))
	for _, user := range db.users {
		users = append(users, user)
	}

	// 序列化
	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal users: %w", err)
	}

	// 确保目录存在
	dir := filepath.Dir(db.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 写入文件（安全权限）
	if err := os.WriteFile(db.filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// GetUser 获取用户
func (db *FileDatabase) GetUser(username string) (*PasswordUser, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	user, exists := db.users[username]
	if !exists {
		return nil, fmt.Errorf("user not found: %s", username)
	}

	return user, nil
}

// CreateUser 创建用户
func (db *FileDatabase) CreateUser(user *PasswordUser) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.users[user.Username]; exists {
		return fmt.Errorf("user already exists: %s", user.Username)
	}

	db.users[user.Username] = user

	return db.save()
}

// UpdateUser 更新用户
func (db *FileDatabase) UpdateUser(user *PasswordUser) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.users[user.Username]; !exists {
		return fmt.Errorf("user not found: %s", user.Username)
	}

	db.users[user.Username] = user

	return db.save()
}

// DeleteUser 删除用户
func (db *FileDatabase) DeleteUser(username string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.users[username]; !exists {
		return fmt.Errorf("user not found: %s", username)
	}

	delete(db.users, username)

	return db.save()
}

// ListUsers 列出所有用户
func (db *FileDatabase) ListUsers() ([]*PasswordUser, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	users := make([]*PasswordUser, 0, len(db.users))
	for _, user := range db.users {
		users = append(users, user)
	}

	return users, nil
}

// Backup 备份数据库
func (db *FileDatabase) Backup(backupPath string) error {
	db.mu.RLock()
	defer db.mu.RUnlock()

	// 读取当前数据
	data, err := os.ReadFile(db.filePath)
	if err != nil {
		return fmt.Errorf("failed to read database: %w", err)
	}

	// 写入备份文件
	if err := os.WriteFile(backupPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write backup: %w", err)
	}

	return nil
}

// Restore 从备份恢复
func (db *FileDatabase) Restore(backupPath string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// 加载备份数据
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup: %w", err)
	}

	var users []*PasswordUser
	if err := json.Unmarshal(data, &users); err != nil {
		return fmt.Errorf("failed to unmarshal backup: %w", err)
	}

	// 重建索引
	db.users = make(map[string]*PasswordUser)
	for _, user := range users {
		db.users[user.Username] = user
	}

	// 保存到主数据库
	return db.save()
}

// DatabaseStats 数据库统计
type DatabaseStats struct {
	TotalUsers    int `json:"total_users"`
	EnabledUsers  int `json:"enabled_users"`
	DisabledUsers int `json:"disabled_users"`
	Users2FA      int `json:"users_2fa"`
}

// GetStats 获取数据库统计
func (db *FileDatabase) GetStats() (*DatabaseStats, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	stats := &DatabaseStats{
		TotalUsers: len(db.users),
	}

	for _, user := range db.users {
		if user.Enabled {
			stats.EnabledUsers++
		} else {
			stats.DisabledUsers++
		}

		if user.TwoFactorEnabled {
			stats.Users2FA++
		}
	}

	return stats, nil
}
