package transport

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"
)

// PortHoppingConfig 端口跳跃配置
type PortHoppingConfig struct {
	Enabled       bool          // 是否启用端口跳跃
	PortRangeMin  int           // 端口范围最小值
	PortRangeMax  int           // 端口范围最大值
	HopInterval   time.Duration // 跳跃间隔
	SharedSecret  []byte        // 共享密钥（用于HMAC）
	SyncTolerance time.Duration // 时间同步容忍度
}

// DefaultPortHoppingConfig 默认配置
func DefaultPortHoppingConfig(secret []byte) PortHoppingConfig {
	return PortHoppingConfig{
		Enabled:       true,
		PortRangeMin:  10000,
		PortRangeMax:  60000,
		HopInterval:   60 * time.Second, // 每分钟跳跃一次
		SharedSecret:  secret,
		SyncTolerance: 5 * time.Second, // 允许5秒误差
	}
}

// PortHoppingManager 端口跳跃管理器
type PortHoppingManager struct {
	mu sync.RWMutex

	config PortHoppingConfig

	// 当前状态
	currentPort    int
	currentTimeSlot int64
	lastHopTime    time.Time

	// 端口列表（预计算）
	portSequence []int
	sequenceIndex int

	// 统计
	hopCount      int
	failedHops    int
	lastFailure   time.Time

	// 回调
	onPortChange func(oldPort, newPort int)

	// 控制
	stopChan chan struct{}
	stopped  bool
}

// NewPortHoppingManager 创建端口跳跃管理器
func NewPortHoppingManager(config PortHoppingConfig) *PortHoppingManager {
	phm := &PortHoppingManager{
		config:   config,
		stopChan: make(chan struct{}),
	}

	// 计算初始端口
	phm.currentTimeSlot = phm.getCurrentTimeSlot()
	phm.currentPort = phm.calculatePort(phm.currentTimeSlot)
	phm.lastHopTime = time.Now()

	return phm
}

// Start 启动端口跳跃
func (phm *PortHoppingManager) Start() {
	phm.mu.Lock()
	if phm.stopped {
		phm.mu.Unlock()
		return
	}
	phm.mu.Unlock()

	if !phm.config.Enabled {
		return
	}

	go phm.hopRoutine()
}

// Stop 停止端口跳跃
func (phm *PortHoppingManager) Stop() {
	phm.mu.Lock()
	defer phm.mu.Unlock()

	if phm.stopped {
		return
	}

	phm.stopped = true
	close(phm.stopChan)
}

// GetCurrentPort 获取当前端口
func (phm *PortHoppingManager) GetCurrentPort() int {
	phm.mu.RLock()
	defer phm.mu.RUnlock()
	return phm.currentPort
}

// GetPortForTimeSlot 获取指定时间槽的端口（用于同步）
func (phm *PortHoppingManager) GetPortForTimeSlot(timeSlot int64) int {
	return phm.calculatePort(timeSlot)
}

// GetCurrentTimeSlot 获取当前时间槽
func (phm *PortHoppingManager) getCurrentTimeSlot() int64 {
	return time.Now().Unix() / int64(phm.config.HopInterval.Seconds())
}

// ValidatePort 验证端口是否在有效时间窗口内
func (phm *PortHoppingManager) ValidatePort(port int) bool {
	currentSlot := phm.getCurrentTimeSlot()

	// 检查当前时间槽
	if port == phm.calculatePort(currentSlot) {
		return true
	}

	// 检查前一个时间槽（时间同步容忍）
	if port == phm.calculatePort(currentSlot-1) {
		return true
	}

	// 检查下一个时间槽（时钟漂移）
	if port == phm.calculatePort(currentSlot+1) {
		return true
	}

	return false
}

// calculatePort 计算指定时间槽的端口
func (phm *PortHoppingManager) calculatePort(timeSlot int64) int {
	// 使用 HMAC-SHA256 生成伪随机端口
	h := hmac.New(sha256.New, phm.config.SharedSecret)

	// 将时间槽写入HMAC
	timeBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(timeBytes, uint64(timeSlot))
	h.Write(timeBytes)

	// 获取HMAC结果
	hash := h.Sum(nil)

	// 取前8字节作为随机数
	randomValue := binary.BigEndian.Uint64(hash[:8])

	// 计算端口范围
	portRange := phm.config.PortRangeMax - phm.config.PortRangeMin + 1

	// 计算端口
	port := phm.config.PortRangeMin + int(randomValue%uint64(portRange))

	return port
}

// hopRoutine 端口跳跃协程
func (phm *PortHoppingManager) hopRoutine() {
	// 计算下次跳跃时间
	nextHopTime := phm.calculateNextHopTime()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-phm.stopChan:
			return

		case <-ticker.C:
			now := time.Now()

			// 检查是否到了跳跃时间
			if now.After(nextHopTime) || now.Equal(nextHopTime) {
				phm.performHop()
				nextHopTime = phm.calculateNextHopTime()
			}
		}
	}
}

// performHop 执行端口跳跃
func (phm *PortHoppingManager) performHop() {
	phm.mu.Lock()
	defer phm.mu.Unlock()

	if phm.stopped {
		return
	}

	oldPort := phm.currentPort
	newTimeSlot := phm.getCurrentTimeSlot()

	// 如果时间槽没变，说明时间还没到
	if newTimeSlot == phm.currentTimeSlot {
		return
	}

	// 计算新端口
	newPort := phm.calculatePort(newTimeSlot)

	// 更新状态
	phm.currentTimeSlot = newTimeSlot
	phm.currentPort = newPort
	phm.lastHopTime = time.Now()
	phm.hopCount++

	// 回调通知
	if phm.onPortChange != nil && oldPort != newPort {
		go phm.onPortChange(oldPort, newPort)
	}
}

// calculateNextHopTime 计算下次跳跃时间
func (phm *PortHoppingManager) calculateNextHopTime() time.Time {
	currentSlot := phm.getCurrentTimeSlot()
	nextSlot := currentSlot + 1
	nextSlotTime := time.Unix(nextSlot*int64(phm.config.HopInterval.Seconds()), 0)
	return nextSlotTime
}

// SetPortChangeCallback 设置端口变化回调
func (phm *PortHoppingManager) SetPortChangeCallback(callback func(oldPort, newPort int)) {
	phm.mu.Lock()
	defer phm.mu.Unlock()
	phm.onPortChange = callback
}

// GetStats 获取统计信息
func (phm *PortHoppingManager) GetStats() PortHoppingStats {
	phm.mu.RLock()
	defer phm.mu.RUnlock()

	return PortHoppingStats{
		CurrentPort:     phm.currentPort,
		CurrentTimeSlot: phm.currentTimeSlot,
		LastHopTime:     phm.lastHopTime,
		HopCount:        phm.hopCount,
		FailedHops:      phm.failedHops,
		LastFailure:     phm.lastFailure,
		NextHopTime:     phm.calculateNextHopTime(),
	}
}

// PortHoppingStats 端口跳跃统计
type PortHoppingStats struct {
	CurrentPort     int
	CurrentTimeSlot int64
	LastHopTime     time.Time
	HopCount        int
	FailedHops      int
	LastFailure     time.Time
	NextHopTime     time.Time
}

// PortHoppingListener 端口跳跃监听器
type PortHoppingListener struct {
	manager  *PortHoppingManager
	listener net.Listener
	mu       sync.RWMutex
	closed   bool
}

// NewPortHoppingListener 创建端口跳跃监听器
func NewPortHoppingListener(manager *PortHoppingManager) (*PortHoppingListener, error) {
	phl := &PortHoppingListener{
		manager: manager,
	}

	// 在当前端口上监听
	currentPort := manager.GetCurrentPort()
	addr := fmt.Sprintf(":%d", currentPort)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on port %d: %w", currentPort, err)
	}

	phl.listener = listener

	// 设置端口变化回调
	manager.SetPortChangeCallback(func(oldPort, newPort int) {
		phl.handlePortChange(oldPort, newPort)
	})

	return phl, nil
}

// Accept 接受连接
func (phl *PortHoppingListener) Accept() (net.Conn, error) {
	phl.mu.RLock()
	listener := phl.listener
	phl.mu.RUnlock()

	if listener == nil {
		return nil, fmt.Errorf("listener is nil")
	}

	return listener.Accept()
}

// Close 关闭监听器
func (phl *PortHoppingListener) Close() error {
	phl.mu.Lock()
	defer phl.mu.Unlock()

	if phl.closed {
		return nil
	}

	phl.closed = true

	if phl.listener != nil {
		return phl.listener.Close()
	}

	return nil
}

// Addr 获取监听地址
func (phl *PortHoppingListener) Addr() net.Addr {
	phl.mu.RLock()
	defer phl.mu.RUnlock()

	if phl.listener != nil {
		return phl.listener.Addr()
	}

	return nil
}

// handlePortChange 处理端口变化
func (phl *PortHoppingListener) handlePortChange(oldPort, newPort int) {
	phl.mu.Lock()
	defer phl.mu.Unlock()

	if phl.closed {
		return
	}

	// 关闭旧监听器
	if phl.listener != nil {
		phl.listener.Close()
	}

	// 在新端口上创建监听器
	addr := fmt.Sprintf(":%d", newPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		// 端口跳跃失败，记录错误
		// 这里可以触发回退逻辑
		return
	}

	phl.listener = listener
}

// PortHoppingDialer 端口跳跃拨号器
type PortHoppingDialer struct {
	manager *PortHoppingManager
	host    string
}

// NewPortHoppingDialer 创建端口跳跃拨号器
func NewPortHoppingDialer(manager *PortHoppingManager, host string) *PortHoppingDialer {
	return &PortHoppingDialer{
		manager: manager,
		host:    host,
	}
}

// Dial 拨号连接
func (phd *PortHoppingDialer) Dial() (net.Conn, error) {
	// 获取当前端口
	currentPort := phd.manager.GetCurrentPort()
	addr := fmt.Sprintf("%s:%d", phd.host, currentPort)

	// 尝试连接
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		// 连接失败，尝试相邻时间槽
		return phd.dialWithTolerance()
	}

	return conn, nil
}

// dialWithTolerance 使用时间容忍度拨号
func (phd *PortHoppingDialer) dialWithTolerance() (net.Conn, error) {
	currentSlot := phd.manager.getCurrentTimeSlot()

	// 尝试前一个时间槽
	prevPort := phd.manager.GetPortForTimeSlot(currentSlot - 1)
	addr := fmt.Sprintf("%s:%d", phd.host, prevPort)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err == nil {
		return conn, nil
	}

	// 尝试下一个时间槽
	nextPort := phd.manager.GetPortForTimeSlot(currentSlot + 1)
	addr = fmt.Sprintf("%s:%d", phd.host, nextPort)
	conn, err = net.DialTimeout("tcp", addr, 2*time.Second)
	if err == nil {
		return conn, nil
	}

	return nil, fmt.Errorf("failed to connect to any valid port")
}

// GetCurrentAddress 获取当前地址
func (phd *PortHoppingDialer) GetCurrentAddress() string {
	port := phd.manager.GetCurrentPort()
	return fmt.Sprintf("%s:%d", phd.host, port)
}
