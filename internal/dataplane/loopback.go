package dataplane

import (
	"errors"
	"sync"
)

// Loopback implements Interface with in-memory channels. It is useful for
// tests, demos, or environments where a full TUN/TAP integration is not yet
// available.
type Loopback struct {
	outbound    chan Frame
	mu          sync.RWMutex
	subscribers map[string][]chan []byte
	closed      bool
}

// NewLoopback constructs a loopback dataplane initialised with the provided
// peer names. Additional peers can be added dynamically via EnsurePeer.
func NewLoopback(peers []string) *Loopback {
	registry := make(map[string][]chan []byte, len(peers))
	for _, name := range peers {
		if name != "" {
			registry[name] = nil
		}
	}
	return &Loopback{
		outbound:    make(chan Frame, 256),
		subscribers: registry,
	}
}

// EnsurePeer makes sure the loopback dataplane is aware of a peer so that
// deliveries can be consumed.
func (l *Loopback) EnsurePeer(name string) {
	if name == "" {
		return
	}
	l.mu.Lock()
	if _, exists := l.subscribers[name]; !exists {
		l.subscribers[name] = nil
	}
	l.mu.Unlock()
}

// Inject queues a payload so that it will be sent across the secure transport.
func (l *Loopback) Inject(peer string, payload []byte) error {
	frame := Frame{Peer: peer, Payload: append([]byte(nil), payload...)}
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		return errors.New("dataplane closed")
	}
	select {
	case l.outbound <- frame:
		return nil
	default:
		return errors.New("dataplane outbound queue full")
	}
}

// Outbound exposes the channel of locally generated frames.
func (l *Loopback) Outbound() <-chan Frame {
	return l.outbound
}

// Deliver fans out payloads that arrived over STP to any subscribers that have
// registered interest in the peer.
func (l *Loopback) Deliver(peer string, payload []byte) error {
	l.mu.RLock()
	if l.closed {
		l.mu.RUnlock()
		return errors.New("dataplane closed")
	}
	listeners := l.subscribers[peer]
	l.mu.RUnlock()

	if len(listeners) == 0 {
		return nil
	}

	for _, ch := range listeners {
		dup := append([]byte(nil), payload...)
		select {
		case ch <- dup:
		default:
		}
	}
	return nil
}

// Subscribe registers a consumer for traffic targeting the supplied peer. The
// returned channel will be closed when the dataplane shuts down.
func (l *Loopback) Subscribe(peer string, buffer int) (<-chan []byte, error) {
	if peer == "" {
		return nil, errors.New("peer name required")
	}
	if buffer <= 0 {
		buffer = 32
	}
	ch := make(chan []byte, buffer)

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		close(ch)
		return nil, errors.New("dataplane closed")
	}
	l.subscribers[peer] = append(l.subscribers[peer], ch)
	l.mu.Unlock()
	return ch, nil
}

// Close tears down the loopback dataplane.
func (l *Loopback) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	for _, listeners := range l.subscribers {
		for _, ch := range listeners {
			close(ch)
		}
	}
	close(l.outbound)
	l.mu.Unlock()
	return nil
}
