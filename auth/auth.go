package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// User represents an authenticated user
type User struct {
	Username     string
	PasswordHash []byte
	Salt         []byte
	Roles        []string
	CreatedAt    time.Time
	LastLogin    time.Time
	Enabled      bool
}

// Credential stores user credentials
type Credential struct {
	Username string
	Password string
}

// AuthToken represents an authentication token
type AuthToken struct {
	Token     string
	Username  string
	IssuedAt  time.Time
	ExpiresAt time.Time
	Roles     []string
}

// Authenticator manages user authentication
type Authenticator struct {
	users  map[string]*User
	tokens map[string]*AuthToken
	mu     sync.RWMutex

	// Argon2 parameters
	argonTime    uint32
	argonMemory  uint32
	argonThreads uint8
	argonKeyLen  uint32
	saltLen      int

	// Token settings
	tokenDuration time.Duration
}

// NewAuthenticator creates a new authenticator
func NewAuthenticator() *Authenticator {
	return &Authenticator{
		users:         make(map[string]*User),
		tokens:        make(map[string]*AuthToken),
		argonTime:     1,
		argonMemory:   64 * 1024, // 64 MB
		argonThreads:  4,
		argonKeyLen:   32,
		saltLen:       16,
		tokenDuration: 24 * time.Hour,
	}
}

// hashPassword creates a secure password hash using Argon2id
func (a *Authenticator) hashPassword(password string, salt []byte) []byte {
	return argon2.IDKey(
		[]byte(password),
		salt,
		a.argonTime,
		a.argonMemory,
		a.argonThreads,
		a.argonKeyLen,
	)
}

// generateSalt creates a random salt
func (a *Authenticator) generateSalt() ([]byte, error) {
	salt := make([]byte, a.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return salt, nil
}

// generateToken creates a random authentication token
func (a *Authenticator) generateToken() (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(tokenBytes), nil
}

// AddUser adds a new user to the system
func (a *Authenticator) AddUser(username, password string, roles []string) error {
	if username == "" {
		return errors.New("username cannot be empty")
	}
	if password == "" {
		return errors.New("password cannot be empty")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if _, exists := a.users[username]; exists {
		return fmt.Errorf("user %s already exists", username)
	}

	salt, err := a.generateSalt()
	if err != nil {
		return err
	}

	passwordHash := a.hashPassword(password, salt)

	user := &User{
		Username:     username,
		PasswordHash: passwordHash,
		Salt:         salt,
		Roles:        roles,
		CreatedAt:    time.Now(),
		Enabled:      true,
	}

	a.users[username] = user
	return nil
}

// Authenticate verifies user credentials and returns a token
func (a *Authenticator) Authenticate(cred Credential) (string, error) {
	a.mu.RLock()
	user, exists := a.users[cred.Username]
	a.mu.RUnlock()

	if !exists {
		return "", errors.New("invalid username or password")
	}

	if !user.Enabled {
		return "", errors.New("user account is disabled")
	}

	// Verify password
	passwordHash := a.hashPassword(cred.Password, user.Salt)
	if subtle.ConstantTimeCompare(passwordHash, user.PasswordHash) != 1 {
		return "", errors.New("invalid username or password")
	}

	// Generate token
	token, err := a.generateToken()
	if err != nil {
		return "", err
	}

	now := time.Now()
	authToken := &AuthToken{
		Token:     token,
		Username:  cred.Username,
		IssuedAt:  now,
		ExpiresAt: now.Add(a.tokenDuration),
		Roles:     user.Roles,
	}

	a.mu.Lock()
	a.tokens[token] = authToken
	user.LastLogin = now
	a.mu.Unlock()

	return token, nil
}

// ValidateToken checks if a token is valid
func (a *Authenticator) ValidateToken(token string) (*AuthToken, error) {
	a.mu.RLock()
	authToken, exists := a.tokens[token]
	a.mu.RUnlock()

	if !exists {
		return nil, errors.New("invalid token")
	}

	if time.Now().After(authToken.ExpiresAt) {
		a.mu.Lock()
		delete(a.tokens, token)
		a.mu.Unlock()
		return nil, errors.New("token expired")
	}

	return authToken, nil
}

// RevokeToken invalidates a token
func (a *Authenticator) RevokeToken(token string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, exists := a.tokens[token]; !exists {
		return errors.New("token not found")
	}

	delete(a.tokens, token)
	return nil
}

// HasRole checks if a user has a specific role
func (a *Authenticator) HasRole(token, role string) bool {
	authToken, err := a.ValidateToken(token)
	if err != nil {
		return false
	}

	for _, r := range authToken.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// DisableUser disables a user account
func (a *Authenticator) DisableUser(username string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	user, exists := a.users[username]
	if !exists {
		return fmt.Errorf("user %s not found", username)
	}

	user.Enabled = false
	return nil
}

// EnableUser enables a user account
func (a *Authenticator) EnableUser(username string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	user, exists := a.users[username]
	if !exists {
		return fmt.Errorf("user %s not found", username)
	}

	user.Enabled = true
	return nil
}

// ChangePassword changes a user's password
func (a *Authenticator) ChangePassword(username, oldPassword, newPassword string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	user, exists := a.users[username]
	if !exists {
		return errors.New("user not found")
	}

	// Verify old password
	oldHash := a.hashPassword(oldPassword, user.Salt)
	if subtle.ConstantTimeCompare(oldHash, user.PasswordHash) != 1 {
		return errors.New("incorrect old password")
	}

	// Generate new salt and hash
	newSalt, err := a.generateSalt()
	if err != nil {
		return err
	}

	newHash := a.hashPassword(newPassword, newSalt)
	user.Salt = newSalt
	user.PasswordHash = newHash

	return nil
}

// CleanupExpiredTokens removes expired tokens
func (a *Authenticator) CleanupExpiredTokens() int {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	removed := 0

	for token, authToken := range a.tokens {
		if now.After(authToken.ExpiresAt) {
			delete(a.tokens, token)
			removed++
		}
	}

	return removed
}

// GetUserInfo returns information about a user
func (a *Authenticator) GetUserInfo(username string) (*User, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	user, exists := a.users[username]
	if !exists {
		return nil, fmt.Errorf("user %s not found", username)
	}

	// Return a copy to prevent modification
	userCopy := *user
	return &userCopy, nil
}

// ListUsers returns all usernames
func (a *Authenticator) ListUsers() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	usernames := make([]string, 0, len(a.users))
	for username := range a.users {
		usernames = append(usernames, username)
	}
	return usernames
}

// GenerateHMACToken generates an HMAC-based token for API authentication
func GenerateHMACToken(secret, data []byte) string {
	h := hmac.New(sha256.New, secret)
	h.Write(data)
	return base64.URLEncoding.EncodeToString(h.Sum(nil))
}

// ValidateHMACToken validates an HMAC token
func ValidateHMACToken(secret, data []byte, token string) bool {
	expected := GenerateHMACToken(secret, data)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(token)) == 1
}
