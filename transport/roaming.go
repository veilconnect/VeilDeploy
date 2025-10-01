package transport

import (
	"net"
	"sync"
	"time"
)

// RoamingConfig 漫游配置
type RoamingConfig struct {
	Enabled         bool          // 是否启用漫游
	SwitchThreshold int           // 切换阈值（连续包数）
	VerifyTimeout   time.Duration // 验证超时
	MaxCandidates   int           // 最大候选端点数
}

// DefaultRoamingConfig 默认配置
func DefaultRoamingConfig() RoamingConfig {
	return RoamingConfig{
		Enabled:         true,
		SwitchThreshold: 3,
		VerifyTimeout:   5 * time.Second,
		MaxCandidates:   5,
	}
}

// EndpointCandidate 候选端点
type EndpointCandidate struct {
	Addr          net.Addr
	FirstSeen     time.Time
	LastSeen      time.Time
	PacketCount   int
	Authenticated bool
}

// RoamingManager 漫游管理器
type RoamingManager struct {
	mu sync.RWMutex

	// 配置
	config RoamingConfig

	// 当前端点
	currentEndpoint net.Addr
	establishedAt   time.Time

	// 候选端点
	candidates map[string]*EndpointCandidate

	// 统计
	switchCount     int
	lastSwitchTime  time.Time

	// 回调
	onEndpointChange func(old, new net.Addr)
}

// NewRoamingManager 创建漫游管理器
func NewRoamingManager(config RoamingConfig, initialEndpoint net.Addr) *RoamingManager {
	return &RoamingManager{
		config:          config,
		currentEndpoint: initialEndpoint,
		establishedAt:   time.Now(),
		candidates:      make(map[string]*EndpointCandidate),
	}
}

// SetEndpointChangeCallback 设置端点变化回调
func (rm *RoamingManager) SetEndpointChangeCallback(callback func(old, new net.Addr)) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.onEndpointChange = callback
}

// UpdateEndpoint 更新端点（处理接收到的数据包）
func (rm *RoamingManager) UpdateEndpoint(srcAddr net.Addr, authenticated bool) bool {
	if !rm.config.Enabled {
		return false
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	now := time.Now()

	// 如果是当前端点，直接返回
	if addrEqual(srcAddr, rm.currentEndpoint) {
		return false
	}

	addrKey := srcAddr.String()

	// 查找或创建候选端点
	candidate, exists := rm.candidates[addrKey]
	if !exists {
		// 新候选端点
		candidate = &EndpointCandidate{
			Addr:          srcAddr,
			FirstSeen:     now,
			LastSeen:      now,
			PacketCount:   1,
			Authenticated: authenticated,
		}
		rm.candidates[addrKey] = candidate

		// 限制候选数量
		if len(rm.candidates) > rm.config.MaxCandidates {
			rm.pruneOldestCandidate()
		}

		return false
	}

	// 更新候选端点
	candidate.LastSeen = now
	candidate.PacketCount++
	candidate.Authenticated = candidate.Authenticated || authenticated

	// 检查是否应该切换
	if candidate.Authenticated && candidate.PacketCount >= rm.config.SwitchThreshold {
		// 验证超时检查
		if now.Sub(candidate.FirstSeen) <= rm.config.VerifyTimeout {
			return rm.switchToEndpoint(candidate)
		}
	}

	return false
}

// switchToEndpoint 切换到新端点
func (rm *RoamingManager) switchToEndpoint(candidate *EndpointCandidate) bool {
	oldEndpoint := rm.currentEndpoint
	newEndpoint := candidate.Addr

	rm.currentEndpoint = newEndpoint
	rm.establishedAt = time.Now()
	rm.switchCount++
	rm.lastSwitchTime = time.Now()

	// 清理候选列表
	delete(rm.candidates, candidate.Addr.String())

	// 回调通知
	if rm.onEndpointChange != nil {
		go rm.onEndpointChange(oldEndpoint, newEndpoint)
	}

	return true
}

// pruneOldestCandidate 删除最旧的候选端点
func (rm *RoamingManager) pruneOldestCandidate() {
	var oldest *EndpointCandidate
	var oldestKey string

	for key, candidate := range rm.candidates {
		if oldest == nil || candidate.FirstSeen.Before(oldest.FirstSeen) {
			oldest = candidate
			oldestKey = key
		}
	}

	if oldestKey != "" {
		delete(rm.candidates, oldestKey)
	}
}

// GetCurrentEndpoint 获取当前端点
func (rm *RoamingManager) GetCurrentEndpoint() net.Addr {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.currentEndpoint
}

// ForceSetEndpoint 强制设置端点（用于初始化）
func (rm *RoamingManager) ForceSetEndpoint(addr net.Addr) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	oldEndpoint := rm.currentEndpoint
	rm.currentEndpoint = addr
	rm.establishedAt = time.Now()

	// 清空候选列表
	rm.candidates = make(map[string]*EndpointCandidate)

	if rm.onEndpointChange != nil && oldEndpoint != nil {
		go rm.onEndpointChange(oldEndpoint, addr)
	}
}

// GetStats 获取统计信息
func (rm *RoamingManager) GetStats() RoamingStats {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	candidateAddrs := make([]string, 0, len(rm.candidates))
	for _, candidate := range rm.candidates {
		candidateAddrs = append(candidateAddrs, candidate.Addr.String())
	}

	return RoamingStats{
		CurrentEndpoint:  rm.currentEndpoint.String(),
		EstablishedAt:    rm.establishedAt,
		SwitchCount:      rm.switchCount,
		LastSwitchTime:   rm.lastSwitchTime,
		CandidateCount:   len(rm.candidates),
		CandidateAddrs:   candidateAddrs,
	}
}

// RoamingStats 漫游统计
type RoamingStats struct {
	CurrentEndpoint string
	EstablishedAt   time.Time
	SwitchCount     int
	LastSwitchTime  time.Time
	CandidateCount  int
	CandidateAddrs  []string
}

// CleanupExpiredCandidates 清理过期的候选端点
func (rm *RoamingManager) CleanupExpiredCandidates(maxAge time.Duration) int {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	now := time.Now()
	removed := 0

	for key, candidate := range rm.candidates {
		if now.Sub(candidate.LastSeen) > maxAge {
			delete(rm.candidates, key)
			removed++
		}
	}

	return removed
}

// addrEqual 比较两个网络地址是否相等
func addrEqual(a, b net.Addr) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Network() == b.Network() && a.String() == b.String()
}

// PathValidator 路径验证器
type PathValidator struct {
	challenges map[string]*PathChallenge
	mu         sync.RWMutex
}

// PathChallenge 路径挑战
type PathChallenge struct {
	Data      [8]byte
	SentAt    time.Time
	Validated bool
}

// NewPathValidator 创建路径验证器
func NewPathValidator() *PathValidator {
	return &PathValidator{
		challenges: make(map[string]*PathChallenge),
	}
}

// CreateChallenge 创建挑战
func (pv *PathValidator) CreateChallenge(addr net.Addr) *PathChallenge {
	pv.mu.Lock()
	defer pv.mu.Unlock()

	challenge := &PathChallenge{
		SentAt: time.Now(),
	}

	// 生成随机数据
	for i := range challenge.Data {
		challenge.Data[i] = byte(time.Now().UnixNano() & 0xFF)
	}

	pv.challenges[addr.String()] = challenge
	return challenge
}

// ValidateResponse 验证响应
func (pv *PathValidator) ValidateResponse(addr net.Addr, data [8]byte) bool {
	pv.mu.Lock()
	defer pv.mu.Unlock()

	challenge, exists := pv.challenges[addr.String()]
	if !exists {
		return false
	}

	// 检查数据是否匹配
	if challenge.Data != data {
		return false
	}

	// 检查超时（5秒）
	if time.Since(challenge.SentAt) > 5*time.Second {
		delete(pv.challenges, addr.String())
		return false
	}

	challenge.Validated = true
	return true
}

// CleanupOld 清理旧的挑战
func (pv *PathValidator) CleanupOld(maxAge time.Duration) {
	pv.mu.Lock()
	defer pv.mu.Unlock()

	now := time.Now()
	for addr, challenge := range pv.challenges {
		if now.Sub(challenge.SentAt) > maxAge {
			delete(pv.challenges, addr)
		}
	}
}
