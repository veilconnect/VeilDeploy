package bridge

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Bridge 桥接节点
type Bridge struct {
	ID        string    `json:"id"`
	Address   string    `json:"address"`
	Port      int       `json:"port"`
	Type      string    `json:"type"` // direct/cdn/domain-fronting
	PublicKey string    `json:"public_key"`
	Capacity  int       `json:"capacity"`
	Location  string    `json:"location"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// 内部使用
	connections int
	lastSeen    time.Time
}

// Discovery 桥接发现服务
type Discovery struct {
	mu       sync.RWMutex
	bridges  map[string]*Bridge
	secrets  map[string]string // email -> secret
	requests map[string]int    // IP -> request count (防滥用)

	// 配置
	maxRequestsPerIP   int
	requestResetPeriod time.Duration
	bridgeTimeout      time.Duration
}

// NewDiscovery 创建桥接发现服务
func NewDiscovery() *Discovery {
	d := &Discovery{
		bridges:            make(map[string]*Bridge),
		secrets:            make(map[string]string),
		requests:           make(map[string]int),
		maxRequestsPerIP:   10,
		requestResetPeriod: 24 * time.Hour,
		bridgeTimeout:      7 * 24 * time.Hour,
	}

	// 启动清理任务
	go d.cleanupTask()

	return d
}

// RegisterBridge 注册桥接节点
func (d *Discovery) RegisterBridge(bridge *Bridge) error {
	if bridge.Address == "" || bridge.Port == 0 {
		return fmt.Errorf("invalid bridge address")
	}

	if bridge.ID == "" {
		bridge.ID = generateBridgeID()
	}

	bridge.CreatedAt = time.Now()
	bridge.UpdatedAt = time.Now()
	bridge.lastSeen = time.Now()

	d.mu.Lock()
	d.bridges[bridge.ID] = bridge
	d.mu.Unlock()

	return nil
}

// UpdateBridge 更新桥接节点状态
func (d *Discovery) UpdateBridge(bridgeID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	bridge, exists := d.bridges[bridgeID]
	if !exists {
		return fmt.Errorf("bridge not found: %s", bridgeID)
	}

	bridge.lastSeen = time.Now()
	bridge.UpdatedAt = time.Now()

	return nil
}

// GetBridges 获取桥接节点（限流）
func (d *Discovery) GetBridges(clientIP string, count int) ([]*Bridge, error) {
	// 检查请求限制
	if !d.checkRateLimit(clientIP) {
		return nil, fmt.Errorf("too many requests from IP: %s", clientIP)
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	// 选择健康的桥接节点
	available := make([]*Bridge, 0)
	now := time.Now()

	for _, bridge := range d.bridges {
		// 检查桥接是否在线
		if now.Sub(bridge.lastSeen) > d.bridgeTimeout {
			continue
		}

		// 检查容量
		if bridge.Capacity > 0 && bridge.connections >= bridge.Capacity {
			continue
		}

		available = append(available, bridge)
	}

	// 如果请求的数量超过可用数量，返回所有可用的
	if count > len(available) {
		count = len(available)
	}

	// 随机选择桥接节点
	selected := selectRandomBridges(available, count)

	return selected, nil
}

// GetBridgesByEmail 通过邮件请求桥接节点
func (d *Discovery) GetBridgesByEmail(email string) ([]*Bridge, string, error) {
	// 生成或获取密钥
	secret := d.getOrCreateSecret(email)

	// 生成挑战码（防止自动化）
	challenge := generateChallenge(email, secret)

	// 返回桥接节点和挑战码
	bridges, err := d.GetBridges(email, 3)
	if err != nil {
		return nil, "", err
	}

	return bridges, challenge, nil
}

// VerifyChallenge 验证挑战码
func (d *Discovery) VerifyChallenge(email, challenge string) bool {
	secret := d.getSecret(email)
	if secret == "" {
		return false
	}

	expected := generateChallenge(email, secret)
	return challenge == expected
}

// RequestBridgeHTTPS 通过HTTPS请求桥接节点（类似Tor的BridgeDB）
func (d *Discovery) RequestBridgeHTTPS(w http.ResponseWriter, r *http.Request) {
	clientIP := getClientIP(r)

	// 获取请求的桥接数量
	count := 3
	if countStr := r.URL.Query().Get("count"); countStr != "" {
		fmt.Sscanf(countStr, "%d", &count)
	}

	// 限制最大数量
	if count > 10 {
		count = 10
	}

	// 获取桥接节点
	bridges, err := d.GetBridges(clientIP, count)
	if err != nil {
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	}

	// 返回JSON响应
	response := map[string]interface{}{
		"bridges": bridges,
		"count":   len(bridges),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// checkRateLimit 检查速率限制
func (d *Discovery) checkRateLimit(clientIP string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	count := d.requests[clientIP]
	if count >= d.maxRequestsPerIP {
		return false
	}

	d.requests[clientIP] = count + 1
	return true
}

// getOrCreateSecret 获取或创建密钥
func (d *Discovery) getOrCreateSecret(email string) string {
	d.mu.Lock()
	defer d.mu.Unlock()

	if secret, exists := d.secrets[email]; exists {
		return secret
	}

	secret := generateSecret()
	d.secrets[email] = secret
	return secret
}

// getSecret 获取密钥
func (d *Discovery) getSecret(email string) string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.secrets[email]
}

// cleanupTask 清理任务
func (d *Discovery) cleanupTask() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		d.cleanup()
	}
}

// cleanup 清理过期的桥接和请求记录
func (d *Discovery) cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()

	// 清理过期的桥接节点
	for id, bridge := range d.bridges {
		if now.Sub(bridge.lastSeen) > d.bridgeTimeout {
			delete(d.bridges, id)
		}
	}

	// 重置请求计数器
	d.requests = make(map[string]int)
}

// RemoveBridge 移除桥接节点
func (d *Discovery) RemoveBridge(bridgeID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.bridges[bridgeID]; !exists {
		return fmt.Errorf("bridge not found: %s", bridgeID)
	}

	delete(d.bridges, bridgeID)
	return nil
}

// ListBridges 列出所有桥接节点
func (d *Discovery) ListBridges() []*Bridge {
	d.mu.RLock()
	defer d.mu.RUnlock()

	bridges := make([]*Bridge, 0, len(d.bridges))
	for _, bridge := range d.bridges {
		bridges = append(bridges, bridge)
	}

	return bridges
}

// GetBridgeInfo 获取桥接节点信息
func (d *Discovery) GetBridgeInfo(bridgeID string) (*Bridge, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	bridge, exists := d.bridges[bridgeID]
	if !exists {
		return nil, fmt.Errorf("bridge not found: %s", bridgeID)
	}

	return bridge, nil
}

// Stats 统计信息
type BridgeStats struct {
	TotalBridges   int            `json:"total_bridges"`
	ActiveBridges  int            `json:"active_bridges"`
	TypeCount      map[string]int `json:"type_count"`
	LocationCount  map[string]int `json:"location_count"`
	TotalConnections int          `json:"total_connections"`
}

// GetStats 获取统计信息
func (d *Discovery) GetStats() *BridgeStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	stats := &BridgeStats{
		TotalBridges:  len(d.bridges),
		TypeCount:     make(map[string]int),
		LocationCount: make(map[string]int),
	}

	now := time.Now()

	for _, bridge := range d.bridges {
		// 统计类型
		stats.TypeCount[bridge.Type]++

		// 统计位置
		stats.LocationCount[bridge.Location]++

		// 统计连接数
		stats.TotalConnections += bridge.connections

		// 统计活跃桥接
		if now.Sub(bridge.lastSeen) < d.bridgeTimeout {
			stats.ActiveBridges++
		}
	}

	return stats
}

// 辅助函数

func generateBridgeID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func generateSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func generateChallenge(email, secret string) string {
	h := sha256.New()
	h.Write([]byte(email))
	h.Write([]byte(secret))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func selectRandomBridges(bridges []*Bridge, count int) []*Bridge {
	if count >= len(bridges) {
		return bridges
	}

	// 随机选择
	selected := make([]*Bridge, 0, count)
	indices := make(map[int]bool)

	for len(selected) < count {
		b := make([]byte, 4)
		rand.Read(b)
		index := int(b[0]) % len(bridges)

		if !indices[index] {
			selected = append(selected, bridges[index])
			indices[index] = true
		}
	}

	return selected
}

func getClientIP(r *http.Request) string {
	// 尝试从X-Forwarded-For获取
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}

	// 尝试从X-Real-IP获取
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// 使用RemoteAddr
	return r.RemoteAddr
}

// ExportBridges 导出桥接列表（用于分发）
func (d *Discovery) ExportBridges(writer io.Writer) error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	bridges := make([]*Bridge, 0, len(d.bridges))
	for _, bridge := range d.bridges {
		bridges = append(bridges, bridge)
	}

	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(bridges)
}

// ImportBridges 导入桥接列表
func (d *Discovery) ImportBridges(reader io.Reader) error {
	var bridges []*Bridge

	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&bridges); err != nil {
		return fmt.Errorf("failed to decode bridges: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	for _, bridge := range bridges {
		if bridge.ID == "" {
			bridge.ID = generateBridgeID()
		}
		bridge.lastSeen = time.Now()
		d.bridges[bridge.ID] = bridge
	}

	return nil
}

// BridgeDistributor 桥接分发器（支持多种分发方式）
type BridgeDistributor struct {
	discovery *Discovery
}

// NewBridgeDistributor 创建桥接分发器
func NewBridgeDistributor(discovery *Discovery) *BridgeDistributor {
	return &BridgeDistributor{
		discovery: discovery,
	}
}

// DistributeByEmail 通过邮件分发
func (bd *BridgeDistributor) DistributeByEmail(email string) (string, error) {
	bridges, challenge, err := bd.discovery.GetBridgesByEmail(email)
	if err != nil {
		return "", err
	}

	// 构建邮件内容
	content := fmt.Sprintf(`VeilDeploy Bridge Configuration

Here are your bridge addresses:

`)

	for i, bridge := range bridges {
		content += fmt.Sprintf("%d. veil://%s:%d\n", i+1, bridge.Address, bridge.Port)
	}

	content += fmt.Sprintf(`
Challenge Code: %s

Please keep these bridges private and do not share them publicly.

`, challenge)

	return content, nil
}

// DistributeByHTTPS 通过HTTPS分发
func (bd *BridgeDistributor) DistributeByHTTPS(clientIP string, count int) ([]*Bridge, error) {
	return bd.discovery.GetBridges(clientIP, count)
}
