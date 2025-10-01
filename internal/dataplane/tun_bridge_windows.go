//go:build windows

package dataplane

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"golang.zx2c4.com/wireguard/tun"
)

const defaultTUNMTU = 1420

type TUNBridge struct {
	device   tun.Device
	outbound chan Frame
	mu       sync.RWMutex
	closed   bool
	name     string
}

// NewTUNBridge creates a TUN interface on Windows using Wintun driver.
// The name parameter is used as the tunnel name.
// Wintun driver must be installed on the system.
func NewTUNBridge(name string, mtu int, peers []string) (*TUNBridge, error) {
	if mtu <= 0 {
		mtu = defaultTUNMTU
	}

	if name == "" {
		name = "stp0"
	}

	// Create TUN device using wireguard-go's tun package
	// This will use Wintun driver on Windows
	dev, err := tun.CreateTUN(name, mtu)
	if err != nil {
		return nil, fmt.Errorf("failed to create TUN device: %w", err)
	}

	actualName, err := dev.Name()
	if err != nil {
		dev.Close()
		return nil, fmt.Errorf("failed to get TUN device name: %w", err)
	}

	log.Printf("Created TUN device: %s (MTU: %d)", actualName, mtu)

	bridge := &TUNBridge{
		device:   dev,
		outbound: make(chan Frame, 256),
		name:     actualName,
	}

	// Start background goroutine to read from TUN device
	go bridge.readLoop(mtu)

	return bridge, nil
}

func (t *TUNBridge) Outbound() <-chan Frame {
	return t.outbound
}

// Deliver writes a payload to the TUN device
func (t *TUNBridge) Deliver(peer string, payload []byte) error {
	t.mu.RLock()
	if t.closed {
		t.mu.RUnlock()
		return errors.New("dataplane closed")
	}
	t.mu.RUnlock()

	// Write to TUN device
	// wireguard-go tun.Device.Write uses batch API
	bufs := [][]byte{payload}
	if _, err := t.device.Write(bufs, 0); err != nil {
		return fmt.Errorf("failed to write to TUN device: %w", err)
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

	log.Printf("Closing TUN device: %s", t.name)
	return t.device.Close()
}

// readLoop continuously reads packets from the TUN device
func (t *TUNBridge) readLoop(mtu int) {
	// Allocate buffers for batch reading
	// wireguard-go uses batch API
	bufs := make([][]byte, 1)
	bufs[0] = make([]byte, mtu+4)
	sizes := make([]int, 1)

	for {
		// Read from TUN device (batch API)
		n, err := t.device.Read(bufs, sizes, 0)
		if err != nil {
			t.mu.RLock()
			closed := t.closed
			t.mu.RUnlock()

			if closed {
				return
			}

			// Log error but continue (device might recover)
			log.Printf("TUN read error: %v", err)
			continue
		}

		if n == 0 || sizes[0] == 0 {
			continue
		}

		// Make a copy of the data
		payload := append([]byte(nil), bufs[0][:sizes[0]]...)

		// Create frame without peer (routing will determine peer)
		frame := Frame{
			Peer:    "", // Will be determined by IP routing
			Payload: payload,
		}

		// Try to send to outbound channel (non-blocking)
		select {
		case t.outbound <- frame:
		default:
			// Channel full, drop packet (back-pressure)
			log.Printf("TUN outbound channel full, dropping packet (%d bytes)", sizes[0])
		}
	}
}

// Name returns the actual TUN device name
func (t *TUNBridge) Name() string {
	return t.name
}
