package peer

import (
	"net"
	"sync"
	"time"
)

type Peer struct {
	Name          string
	AllowedIPs    []string
	mu            sync.RWMutex
	sessionID     [16]byte
	endpoint      net.Addr
	lastHandshake time.Time
	lastRekey     time.Time
	lastSend      time.Time
	lastReceive   time.Time
	messagesSent  uint64
	messagesRecv  uint64
	rekeyEpoch    uint32
}

func NewPeer(name string, endpoint net.Addr, allowed []string) *Peer {
	return &Peer{
		Name:       name,
		endpoint:   endpoint,
		AllowedIPs: append([]string(nil), allowed...),
	}
}

func (p *Peer) UpdateHandshake(sessionID [16]byte, endpoint net.Addr, epoch uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sessionID = sessionID
	p.endpoint = endpoint
	p.lastHandshake = time.Now()
	p.rekeyEpoch = epoch
}

func (p *Peer) UpdateRekey(epoch uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rekeyEpoch = epoch
	p.lastRekey = time.Now()
}

func (p *Peer) TouchSend() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastSend = time.Now()
	p.messagesSent++
}

func (p *Peer) TouchReceive() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastReceive = time.Now()
	p.messagesRecv++
}

func (p *Peer) UpdateAllowedIPs(allowedIPs []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.AllowedIPs = append([]string(nil), allowedIPs...)
}

func (p *Peer) Snapshot() Snapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	snapshot := Snapshot{
		Name:          p.Name,
		AllowedIPs:    append([]string(nil), p.AllowedIPs...),
		SessionID:     p.sessionID,
		RekeyEpoch:    p.rekeyEpoch,
		LastHandshake: p.lastHandshake,
		LastRekey:     p.lastRekey,
		LastSend:      p.lastSend,
		LastReceive:   p.lastReceive,
		MessagesSent:  p.messagesSent,
		MessagesRecv:  p.messagesRecv,
	}
	if p.endpoint != nil {
		snapshot.Endpoint = p.endpoint.String()
	}
	return snapshot
}

type Snapshot struct {
	Name          string    `json:"name"`
	Endpoint      string    `json:"endpoint"`
	AllowedIPs    []string  `json:"allowedIPs"`
	SessionID     [16]byte  `json:"sessionId"`
	RekeyEpoch    uint32    `json:"rekeyEpoch"`
	LastHandshake time.Time `json:"lastHandshake"`
	LastRekey     time.Time `json:"lastRekey"`
	LastSend      time.Time `json:"lastSend"`
	LastReceive   time.Time `json:"lastReceive"`
	MessagesSent  uint64    `json:"messagesSent"`
	MessagesRecv  uint64    `json:"messagesRecv"`
}
