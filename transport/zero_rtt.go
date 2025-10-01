package transport

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
	"time"
)

// SessionTicket 会话票据（类似QUIC的session ticket）
type SessionTicket struct {
	ID              [16]byte  // 票据ID
	SessionKey      []byte    // 会话密钥
	RemotePublicKey [32]byte  // 远程公钥
	IssuedAt        time.Time // 签发时间
	ExpiresAt       time.Time // 过期时间
	UsageCount      int       // 使用次数
	MaxUsage        int       // 最大使用次数
}

// ZeroRTTConfig 0-RTT配置
type ZeroRTTConfig struct {
	Enabled           bool          // 是否启用0-RTT
	TicketLifetime    time.Duration // 票据有效期
	MaxTicketUsage    int           // 单个票据最大使用次数
	MaxTicketsPerPeer int           // 每个对等点最大票据数
	CleanupInterval   time.Duration // 清理间隔
	AntiReplay        bool          // 是否启用防重放（0-RTT有重放风险）
}

// DefaultZeroRTTConfig 默认0-RTT配置
func DefaultZeroRTTConfig() ZeroRTTConfig {
	return ZeroRTTConfig{
		Enabled:           true,
		TicketLifetime:    24 * time.Hour,
		MaxTicketUsage:    3,
		MaxTicketsPerPeer: 5,
		CleanupInterval:   1 * time.Hour,
		AntiReplay:        true,
	}
}

// ZeroRTTManager 0-RTT管理器
type ZeroRTTManager struct {
	mu sync.RWMutex

	config ZeroRTTConfig

	// 票据存储（key: peer address, value: tickets）
	tickets map[string][]*SessionTicket

	// 服务器端：已使用的票据ID（防重放）
	usedTickets map[[16]byte]time.Time

	// 统计
	ticketsIssued  uint64
	ticketsUsed    uint64
	zeroRTTSuccess uint64
	zeroRTTFailed  uint64

	// 清理协程控制
	stopChan chan struct{}
	stopped  bool
}

// NewZeroRTTManager 创建0-RTT管理器
func NewZeroRTTManager(config ZeroRTTConfig) *ZeroRTTManager {
	zrm := &ZeroRTTManager{
		config:      config,
		tickets:     make(map[string][]*SessionTicket),
		usedTickets: make(map[[16]byte]time.Time),
		stopChan:    make(chan struct{}),
	}

	// 启动清理协程
	if config.Enabled {
		go zrm.cleanupRoutine()
	}

	return zrm
}

// Stop 停止管理器
func (zrm *ZeroRTTManager) Stop() {
	zrm.mu.Lock()
	defer zrm.mu.Unlock()

	if zrm.stopped {
		return
	}

	zrm.stopped = true
	close(zrm.stopChan)
}

// IssueTicket 签发新票据（服务器端）
func (zrm *ZeroRTTManager) IssueTicket(peerAddr string, sessionKey []byte, remotePubKey [32]byte) (*SessionTicket, error) {
	if !zrm.config.Enabled {
		return nil, fmt.Errorf("0-RTT not enabled")
	}

	zrm.mu.Lock()
	defer zrm.mu.Unlock()

	// 生成票据ID
	ticketID := [16]byte{}
	if _, err := rand.Read(ticketID[:]); err != nil {
		return nil, fmt.Errorf("failed to generate ticket ID: %w", err)
	}

	// 创建票据
	now := time.Now()
	ticket := &SessionTicket{
		ID:              ticketID,
		SessionKey:      make([]byte, len(sessionKey)),
		RemotePublicKey: remotePubKey,
		IssuedAt:        now,
		ExpiresAt:       now.Add(zrm.config.TicketLifetime),
		UsageCount:      0,
		MaxUsage:        zrm.config.MaxTicketUsage,
	}
	copy(ticket.SessionKey, sessionKey)

	// 存储票据
	tickets := zrm.tickets[peerAddr]
	tickets = append(tickets, ticket)

	// 限制每个对等点的票据数量
	if len(tickets) > zrm.config.MaxTicketsPerPeer {
		// 删除最旧的票据
		tickets = tickets[1:]
	}

	zrm.tickets[peerAddr] = tickets
	zrm.ticketsIssued++

	return ticket, nil
}

// UseTicket 使用票据（客户端）
func (zrm *ZeroRTTManager) UseTicket(peerAddr string) (*SessionTicket, error) {
	if !zrm.config.Enabled {
		return nil, fmt.Errorf("0-RTT not enabled")
	}

	zrm.mu.Lock()
	defer zrm.mu.Unlock()

	tickets := zrm.tickets[peerAddr]
	if len(tickets) == 0 {
		return nil, fmt.Errorf("no tickets available for peer %s", peerAddr)
	}

	// 查找最新的有效票据
	now := time.Now()
	for i := len(tickets) - 1; i >= 0; i-- {
		ticket := tickets[i]

		// 检查是否过期
		if ticket.ExpiresAt.Before(now) {
			continue
		}

		// 检查使用次数
		if ticket.UsageCount >= ticket.MaxUsage {
			continue
		}

		// 使用票据
		ticket.UsageCount++
		zrm.ticketsUsed++

		return ticket, nil
	}

	return nil, fmt.Errorf("no valid tickets available")
}

// ValidateTicket 验证票据（服务器端）
func (zrm *ZeroRTTManager) ValidateTicket(ticket *SessionTicket) error {
	if !zrm.config.Enabled {
		return fmt.Errorf("0-RTT not enabled")
	}

	zrm.mu.Lock()
	defer zrm.mu.Unlock()

	now := time.Now()

	// 检查是否过期
	if ticket.ExpiresAt.Before(now) {
		zrm.zeroRTTFailed++
		return fmt.Errorf("ticket expired")
	}

	// 检查使用次数
	if ticket.UsageCount > ticket.MaxUsage {
		zrm.zeroRTTFailed++
		return fmt.Errorf("ticket usage exceeded")
	}

	// 防重放检查
	if zrm.config.AntiReplay {
		if usedTime, exists := zrm.usedTickets[ticket.ID]; exists {
			// 票据已被使用
			// 但如果使用时间很近，可能是合法的重试
			if time.Since(usedTime) < 5*time.Second {
				// 允许5秒内的重试
				zrm.zeroRTTSuccess++
				return nil
			}
			zrm.zeroRTTFailed++
			return fmt.Errorf("ticket replay detected")
		}

		// 记录使用
		zrm.usedTickets[ticket.ID] = now
	}

	zrm.zeroRTTSuccess++
	return nil
}

// StoreTicket 存储票据（客户端）
func (zrm *ZeroRTTManager) StoreTicket(peerAddr string, ticket *SessionTicket) {
	zrm.mu.Lock()
	defer zrm.mu.Unlock()

	tickets := zrm.tickets[peerAddr]
	tickets = append(tickets, ticket)

	// 限制数量
	if len(tickets) > zrm.config.MaxTicketsPerPeer {
		tickets = tickets[1:]
	}

	zrm.tickets[peerAddr] = tickets
}

// RemoveTicket 删除票据
func (zrm *ZeroRTTManager) RemoveTicket(peerAddr string, ticketID [16]byte) {
	zrm.mu.Lock()
	defer zrm.mu.Unlock()

	tickets := zrm.tickets[peerAddr]
	filtered := make([]*SessionTicket, 0, len(tickets))

	for _, ticket := range tickets {
		if ticket.ID != ticketID {
			filtered = append(filtered, ticket)
		}
	}

	zrm.tickets[peerAddr] = filtered
}

// GetStats 获取统计信息
func (zrm *ZeroRTTManager) GetStats() ZeroRTTStats {
	zrm.mu.RLock()
	defer zrm.mu.RUnlock()

	totalTickets := 0
	for _, tickets := range zrm.tickets {
		totalTickets += len(tickets)
	}

	return ZeroRTTStats{
		TicketsIssued:  zrm.ticketsIssued,
		TicketsUsed:    zrm.ticketsUsed,
		TicketsStored:  uint64(totalTickets),
		ZeroRTTSuccess: zrm.zeroRTTSuccess,
		ZeroRTTFailed:  zrm.zeroRTTFailed,
		UsedTickets:    uint64(len(zrm.usedTickets)),
	}
}

// ZeroRTTStats 0-RTT统计
type ZeroRTTStats struct {
	TicketsIssued  uint64
	TicketsUsed    uint64
	TicketsStored  uint64
	ZeroRTTSuccess uint64
	ZeroRTTFailed  uint64
	UsedTickets    uint64
}

// cleanupRoutine 清理过期数据
func (zrm *ZeroRTTManager) cleanupRoutine() {
	ticker := time.NewTicker(zrm.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-zrm.stopChan:
			return
		case <-ticker.C:
			zrm.cleanup()
		}
	}
}

// cleanup 清理过期票据和使用记录
func (zrm *ZeroRTTManager) cleanup() {
	zrm.mu.Lock()
	defer zrm.mu.Unlock()

	now := time.Now()

	// 清理过期票据
	for peerAddr, tickets := range zrm.tickets {
		filtered := make([]*SessionTicket, 0, len(tickets))

		for _, ticket := range tickets {
			if ticket.ExpiresAt.After(now) && ticket.UsageCount < ticket.MaxUsage {
				filtered = append(filtered, ticket)
			}
		}

		if len(filtered) == 0 {
			delete(zrm.tickets, peerAddr)
		} else {
			zrm.tickets[peerAddr] = filtered
		}
	}

	// 清理旧的使用记录（保留最近1小时）
	cutoff := now.Add(-1 * time.Hour)
	for ticketID, usedTime := range zrm.usedTickets {
		if usedTime.Before(cutoff) {
			delete(zrm.usedTickets, ticketID)
		}
	}
}

// SerializeTicket 序列化票据
func SerializeTicket(ticket *SessionTicket) []byte {
	buf := make([]byte, 0, 1024)

	// 票据ID (16 bytes)
	buf = append(buf, ticket.ID[:]...)

	// 会话密钥长度 (2 bytes) + 密钥
	keyLen := uint16(len(ticket.SessionKey))
	lenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(lenBuf, keyLen)
	buf = append(buf, lenBuf...)
	buf = append(buf, ticket.SessionKey...)

	// 远程公钥 (32 bytes)
	buf = append(buf, ticket.RemotePublicKey[:]...)

	// 签发时间 (8 bytes)
	issuedAt := ticket.IssuedAt.Unix()
	timeBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(timeBuf, uint64(issuedAt))
	buf = append(buf, timeBuf...)

	// 过期时间 (8 bytes)
	expiresAt := ticket.ExpiresAt.Unix()
	binary.BigEndian.PutUint64(timeBuf, uint64(expiresAt))
	buf = append(buf, timeBuf...)

	// 使用次数 (2 bytes)
	usageCountBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(usageCountBuf, uint16(ticket.UsageCount))
	buf = append(buf, usageCountBuf...)

	// 最大使用次数 (2 bytes)
	maxUsageBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(maxUsageBuf, uint16(ticket.MaxUsage))
	buf = append(buf, maxUsageBuf...)

	// 添加校验和
	checksum := sha256.Sum256(buf)
	buf = append(buf, checksum[:4]...)

	return buf
}

// DeserializeTicket 反序列化票据
func DeserializeTicket(data []byte) (*SessionTicket, error) {
	if len(data) < 80 { // 最小长度检查
		return nil, fmt.Errorf("ticket data too short")
	}

	ticket := &SessionTicket{}
	offset := 0

	// 票据ID
	copy(ticket.ID[:], data[offset:offset+16])
	offset += 16

	// 会话密钥
	keyLen := binary.BigEndian.Uint16(data[offset : offset+2])
	offset += 2

	if len(data) < offset+int(keyLen)+4 {
		return nil, fmt.Errorf("invalid ticket data")
	}

	ticket.SessionKey = make([]byte, keyLen)
	copy(ticket.SessionKey, data[offset:offset+int(keyLen)])
	offset += int(keyLen)

	// 远程公钥
	copy(ticket.RemotePublicKey[:], data[offset:offset+32])
	offset += 32

	// 签发时间
	issuedAt := int64(binary.BigEndian.Uint64(data[offset : offset+8]))
	ticket.IssuedAt = time.Unix(issuedAt, 0)
	offset += 8

	// 过期时间
	expiresAt := int64(binary.BigEndian.Uint64(data[offset : offset+8]))
	ticket.ExpiresAt = time.Unix(expiresAt, 0)
	offset += 8

	// 使用次数
	ticket.UsageCount = int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2

	// 最大使用次数
	ticket.MaxUsage = int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2

	// 验证校验和
	expectedChecksum := data[offset : offset+4]
	actualChecksum := sha256.Sum256(data[:offset])

	for i := 0; i < 4; i++ {
		if expectedChecksum[i] != actualChecksum[i] {
			return nil, fmt.Errorf("ticket checksum mismatch")
		}
	}

	return ticket, nil
}

// ZeroRTTData 0-RTT数据包
type ZeroRTTData struct {
	Ticket  *SessionTicket
	Payload []byte
}

// EncodeZeroRTTData 编码0-RTT数据
func EncodeZeroRTTData(data *ZeroRTTData) []byte {
	ticketData := SerializeTicket(data.Ticket)

	// 总长度 = 票据长度字段(2) + 票据 + payload
	totalLen := 2 + len(ticketData) + len(data.Payload)
	buf := make([]byte, 0, totalLen)

	// 票据长度
	ticketLen := make([]byte, 2)
	binary.BigEndian.PutUint16(ticketLen, uint16(len(ticketData)))
	buf = append(buf, ticketLen...)

	// 票据数据
	buf = append(buf, ticketData...)

	// Payload
	buf = append(buf, data.Payload...)

	return buf
}

// DecodeZeroRTTData 解码0-RTT数据
func DecodeZeroRTTData(data []byte) (*ZeroRTTData, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("data too short")
	}

	// 读取票据长度
	ticketLen := binary.BigEndian.Uint16(data[0:2])

	if len(data) < 2+int(ticketLen) {
		return nil, fmt.Errorf("incomplete ticket data")
	}

	// 反序列化票据
	ticket, err := DeserializeTicket(data[2 : 2+ticketLen])
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize ticket: %w", err)
	}

	// 剩余数据为payload
	payload := data[2+ticketLen:]

	return &ZeroRTTData{
		Ticket:  ticket,
		Payload: payload,
	}, nil
}
