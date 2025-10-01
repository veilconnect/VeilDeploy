package timers

import (
	"sync"
	"time"
)

// ConnectionState 定义连接状态
type ConnectionState int

const (
	StateStart ConnectionState = iota
	StateInitiationSent
	StateResponseSent
	StateEstablished
	StateRehandshaking
	StateDead
)

func (s ConnectionState) String() string {
	switch s {
	case StateStart:
		return "Start"
	case StateInitiationSent:
		return "InitiationSent"
	case StateResponseSent:
		return "ResponseSent"
	case StateEstablished:
		return "Established"
	case StateRehandshaking:
		return "Rehandshaking"
	case StateDead:
		return "Dead"
	default:
		return "Unknown"
	}
}

// TimerConfig 定时器配置
type TimerConfig struct {
	HandshakeTimeout    time.Duration // 握手超时时间
	RekeyInterval       time.Duration // 密钥轮换间隔
	KeepaliveInterval   time.Duration // 保活间隔
	DeadPeerTimeout     time.Duration // 死亡检测超时
	HandshakeRetries    int           // 握手重试次数
}

// DefaultTimerConfig 返回默认配置
func DefaultTimerConfig() TimerConfig {
	return TimerConfig{
		HandshakeTimeout:  5 * time.Second,
		RekeyInterval:     5 * time.Minute,
		KeepaliveInterval: 15 * time.Second,
		DeadPeerTimeout:   60 * time.Second,
		HandshakeRetries:  3,
	}
}

// ConnectionTimers 管理连接的所有定时器
type ConnectionTimers struct {
	mu sync.Mutex

	// 定时器
	handshakeTimer *time.Timer
	rekeyTimer     *time.Timer
	keepaliveTimer *time.Timer
	deadPeerTimer  *time.Timer

	// 状态
	state         ConnectionState
	retryCount    int
	lastHandshake time.Time
	lastDataSent  time.Time
	lastDataRecv  time.Time

	// 配置
	config TimerConfig

	// 回调函数
	onHandshakeTimeout func()
	onRekey            func()
	onKeepalive        func()
	onDeadPeer         func()
	onStateChange      func(old, new ConnectionState)

	// 控制
	stopChan chan struct{}
	stopped  bool
}

// NewConnectionTimers 创建定时器管理器
func NewConnectionTimers(config TimerConfig) *ConnectionTimers {
	ct := &ConnectionTimers{
		state:    StateStart,
		config:   config,
		stopChan: make(chan struct{}),
	}

	return ct
}

// SetCallbacks 设置回调函数
func (ct *ConnectionTimers) SetCallbacks(
	onHandshakeTimeout func(),
	onRekey func(),
	onKeepalive func(),
	onDeadPeer func(),
	onStateChange func(old, new ConnectionState),
) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.onHandshakeTimeout = onHandshakeTimeout
	ct.onRekey = onRekey
	ct.onKeepalive = onKeepalive
	ct.onDeadPeer = onDeadPeer
	ct.onStateChange = onStateChange
}

// Start 启动定时器
func (ct *ConnectionTimers) Start() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.stopped {
		return
	}

	// 启动死亡检测定时器
	ct.deadPeerTimer = time.AfterFunc(ct.config.DeadPeerTimeout, func() {
		ct.handleDeadPeer()
	})
}

// Stop 停止所有定时器
func (ct *ConnectionTimers) Stop() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.stopped {
		return
	}

	ct.stopped = true
	close(ct.stopChan)

	if ct.handshakeTimer != nil {
		ct.handshakeTimer.Stop()
	}
	if ct.rekeyTimer != nil {
		ct.rekeyTimer.Stop()
	}
	if ct.keepaliveTimer != nil {
		ct.keepaliveTimer.Stop()
	}
	if ct.deadPeerTimer != nil {
		ct.deadPeerTimer.Stop()
	}
}

// OnDataSent 记录数据发送
func (ct *ConnectionTimers) OnDataSent() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.lastDataSent = time.Now()

	// 重置保活定时器
	if ct.keepaliveTimer != nil {
		ct.keepaliveTimer.Stop()
	}
	ct.keepaliveTimer = time.AfterFunc(ct.config.KeepaliveInterval, func() {
		ct.handleKeepalive()
	})
}

// OnDataReceived 记录数据接收
func (ct *ConnectionTimers) OnDataReceived() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.lastDataRecv = time.Now()

	// 重置死亡检测定时器
	if ct.deadPeerTimer != nil {
		ct.deadPeerTimer.Stop()
	}
	ct.deadPeerTimer = time.AfterFunc(ct.config.DeadPeerTimeout, func() {
		ct.handleDeadPeer()
	})
}

// TransitionState 状态转换
func (ct *ConnectionTimers) TransitionState(newState ConnectionState) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	oldState := ct.state
	if oldState == newState {
		return
	}

	ct.state = newState

	// 回调通知
	if ct.onStateChange != nil {
		go ct.onStateChange(oldState, newState)
	}

	// 根据新状态设置定时器
	switch newState {
	case StateInitiationSent, StateRehandshaking:
		// 启动握手超时定时器
		if ct.handshakeTimer != nil {
			ct.handshakeTimer.Stop()
		}
		ct.handshakeTimer = time.AfterFunc(ct.config.HandshakeTimeout, func() {
			ct.handleHandshakeTimeout()
		})

	case StateEstablished:
		// 握手成功
		ct.lastHandshake = time.Now()
		ct.retryCount = 0

		// 停止握手定时器
		if ct.handshakeTimer != nil {
			ct.handshakeTimer.Stop()
			ct.handshakeTimer = nil
		}

		// 启动密钥轮换定时器
		if ct.rekeyTimer != nil {
			ct.rekeyTimer.Stop()
		}
		ct.rekeyTimer = time.AfterFunc(ct.config.RekeyInterval, func() {
			ct.handleRekey()
		})

		// 启动保活定时器
		if ct.keepaliveTimer != nil {
			ct.keepaliveTimer.Stop()
		}
		ct.keepaliveTimer = time.AfterFunc(ct.config.KeepaliveInterval, func() {
			ct.handleKeepalive()
		})

	case StateDead:
		// 连接死亡，停止所有定时器
		if ct.handshakeTimer != nil {
			ct.handshakeTimer.Stop()
		}
		if ct.rekeyTimer != nil {
			ct.rekeyTimer.Stop()
		}
		if ct.keepaliveTimer != nil {
			ct.keepaliveTimer.Stop()
		}
		if ct.deadPeerTimer != nil {
			ct.deadPeerTimer.Stop()
		}
	}
}

// GetState 获取当前状态
func (ct *ConnectionTimers) GetState() ConnectionState {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.state
}

// GetStats 获取统计信息
func (ct *ConnectionTimers) GetStats() TimerStats {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	return TimerStats{
		State:         ct.state,
		LastHandshake: ct.lastHandshake,
		LastDataSent:  ct.lastDataSent,
		LastDataRecv:  ct.lastDataRecv,
		RetryCount:    ct.retryCount,
	}
}

// TimerStats 定时器统计信息
type TimerStats struct {
	State         ConnectionState
	LastHandshake time.Time
	LastDataSent  time.Time
	LastDataRecv  time.Time
	RetryCount    int
}

// handleHandshakeTimeout 处理握手超时
func (ct *ConnectionTimers) handleHandshakeTimeout() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.stopped {
		return
	}

	ct.retryCount++

	if ct.retryCount >= ct.config.HandshakeRetries {
		// 达到最大重试次数，标记为死亡
		oldState := ct.state
		ct.state = StateDead

		if ct.onStateChange != nil {
			go ct.onStateChange(oldState, StateDead)
		}

		if ct.onDeadPeer != nil {
			go ct.onDeadPeer()
		}
		return
	}

	// 重试握手
	if ct.onHandshakeTimeout != nil {
		go ct.onHandshakeTimeout()
	}

	// 重新设置握手超时定时器
	ct.handshakeTimer = time.AfterFunc(ct.config.HandshakeTimeout, func() {
		ct.handleHandshakeTimeout()
	})
}

// handleRekey 处理密钥轮换
func (ct *ConnectionTimers) handleRekey() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.stopped || ct.state != StateEstablished {
		return
	}

	if ct.onRekey != nil {
		go ct.onRekey()
	}

	// 重新设置密钥轮换定时器
	ct.rekeyTimer = time.AfterFunc(ct.config.RekeyInterval, func() {
		ct.handleRekey()
	})
}

// handleKeepalive 处理保活
func (ct *ConnectionTimers) handleKeepalive() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.stopped || ct.state != StateEstablished {
		return
	}

	// 检查是否需要发送保活包
	timeSinceLastSent := time.Since(ct.lastDataSent)
	if timeSinceLastSent >= ct.config.KeepaliveInterval {
		if ct.onKeepalive != nil {
			go ct.onKeepalive()
		}
	}

	// 重新设置保活定时器
	ct.keepaliveTimer = time.AfterFunc(ct.config.KeepaliveInterval, func() {
		ct.handleKeepalive()
	})
}

// handleDeadPeer 处理死亡连接
func (ct *ConnectionTimers) handleDeadPeer() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.stopped {
		return
	}

	// 检查是否真的超时
	timeSinceLastRecv := time.Since(ct.lastDataRecv)
	if timeSinceLastRecv < ct.config.DeadPeerTimeout {
		// 还没超时，重新设置定时器
		remaining := ct.config.DeadPeerTimeout - timeSinceLastRecv
		ct.deadPeerTimer = time.AfterFunc(remaining, func() {
			ct.handleDeadPeer()
		})
		return
	}

	// 确认超时，标记为死亡
	oldState := ct.state
	ct.state = StateDead

	if ct.onStateChange != nil {
		go ct.onStateChange(oldState, StateDead)
	}

	if ct.onDeadPeer != nil {
		go ct.onDeadPeer()
	}
}
