//go:build !windows

package dataplane

import (
	"errors"
	"sync"

	"golang.zx2c4.com/wireguard/tun"
)

const defaultTUNMTU = 1420

type TUNBridge struct {
	device   tun.Device
	outbound chan Frame
	mu       sync.RWMutex
	closed   bool
}

func NewTUNBridge(name string, mtu int, peers []string) (*TUNBridge, error) {
	if mtu <= 0 {
		mtu = defaultTUNMTU
	}
	dev, err := tun.CreateTUN(name, mtu)
	if err != nil {
		return nil, err
	}
	bridge := &TUNBridge{
		device:   dev,
		outbound: make(chan Frame, 256),
	}
	go bridge.readLoop(mtu)
	return bridge, nil
}

func (t *TUNBridge) Outbound() <-chan Frame {
	return t.outbound
}

func (t *TUNBridge) Deliver(peer string, payload []byte) error {
	t.mu.RLock()
	if t.closed {
		t.mu.RUnlock()
		return errors.New("dataplane closed")
	}
	t.mu.RUnlock()
	// wireguard-go tun.Device.Write uses batch API
	bufs := [][]byte{payload}
	if _, err := t.device.Write(bufs, 0); err != nil {
		return err
	}
	return nil
}

func (t *TUNBridge) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	close(t.outbound)
	t.mu.Unlock()
	return t.device.Close()
}

func (t *TUNBridge) readLoop(mtu int) {
	// Allocate buffers for batch reading
	bufs := make([][]byte, 1)
	bufs[0] = make([]byte, mtu+4)
	sizes := make([]int, 1)

	for {
		n, err := t.device.Read(bufs, sizes, 0)
		if err != nil {
			t.mu.RLock()
			closed := t.closed
			t.mu.RUnlock()
			if closed {
				return
			}
			continue
		}
		if n == 0 || sizes[0] == 0 {
			continue
		}
		payload := append([]byte(nil), bufs[0][:sizes[0]]...)
		frame := Frame{Peer: "", Payload: payload}
		select {
		case t.outbound <- frame:
		default:
		}
	}
}
