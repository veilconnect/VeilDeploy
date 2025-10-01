package dataplane

import (
	"errors"
	"net"
	"sync"
)

type UDPBridge struct {
	conn     *net.UDPConn
	outbound chan Frame
	peers    map[string]*net.UDPAddr
	reverse  map[string]string
	mu       sync.RWMutex
	closed   bool
}

func NewUDPBridge(listen string, peerEndpoints map[string]string) (*UDPBridge, error) {
	if listen == "" {
		return nil, errors.New("udp bridge requires listen address")
	}
	addr, err := net.ResolveUDPAddr("udp", listen)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	peers := make(map[string]*net.UDPAddr, len(peerEndpoints))
	reverse := make(map[string]string, len(peerEndpoints))
	for name, endpoint := range peerEndpoints {
		udpAddr, err := net.ResolveUDPAddr("udp", endpoint)
		if err != nil {
			conn.Close()
			return nil, err
		}
		peers[name] = udpAddr
		reverse[udpAddr.String()] = name
	}

	bridge := &UDPBridge{
		conn:     conn,
		outbound: make(chan Frame, 128),
		peers:    peers,
		reverse:  reverse,
	}
	go bridge.readLoop()
	return bridge, nil
}

func (b *UDPBridge) Outbound() <-chan Frame {
	return b.outbound
}

func (b *UDPBridge) Deliver(peer string, payload []byte) error {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return errors.New("dataplane closed")
	}
	addr, ok := b.peers[peer]
	b.mu.RUnlock()
	if !ok {
		return errors.New("unknown peer")
	}
	if _, err := b.conn.WriteToUDP(payload, addr); err != nil {
		return err
	}
	return nil
}

func (b *UDPBridge) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	close(b.outbound)
	b.mu.Unlock()
	return b.conn.Close()
}

func (b *UDPBridge) LocalAddr() net.Addr {
	if b.conn == nil {
		return nil
	}
	return b.conn.LocalAddr()
}

func (b *UDPBridge) readLoop() {
	buf := make([]byte, 65535)
	for {
		n, addr, err := b.conn.ReadFromUDP(buf)
		if err != nil {
			b.mu.RLock()
			closed := b.closed
			b.mu.RUnlock()
			if closed {
				return
			}
			continue
		}
		peer := b.lookupPeer(addr)
		if peer == "" {
			continue
		}
		payload := append([]byte(nil), buf[:n]...)
		frame := Frame{Peer: peer, Payload: payload}
		select {
		case b.outbound <- frame:
		default:
			// drop if outbound queue full, consistent with UDP semantics
		}
	}
}

func (b *UDPBridge) lookupPeer(addr *net.UDPAddr) string {
	if addr == nil {
		return ""
	}
	b.mu.RLock()
	peer := b.reverse[addr.String()]
	b.mu.RUnlock()
	return peer
}
