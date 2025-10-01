package dataplane

import (
	"bytes"
	"testing"
)

func TestLoopbackDeliverAndInject(t *testing.T) {
	plane := NewLoopback([]string{"alpha"})

	plane.EnsurePeer("beta")

	sub, err := plane.Subscribe("alpha", 1)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	payload := []byte("hello")
	if err := plane.Deliver("alpha", payload); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	received := <-sub
	if !bytes.Equal(received, payload) {
		t.Fatalf("expected %q, got %q", payload, received)
	}

	outbound := []byte("outbound")
	if err := plane.Inject("beta", outbound); err != nil {
		t.Fatalf("inject: %v", err)
	}

	frame := <-plane.Outbound()
	if frame.Peer != "beta" {
		t.Fatalf("unexpected peer %q", frame.Peer)
	}
	if !bytes.Equal(frame.Payload, outbound) {
		t.Fatalf("expected outbound payload %q, got %q", outbound, frame.Payload)
	}

	// Closing the dataplane should prevent further injection.
	if err := plane.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := plane.Inject("beta", []byte("again")); err == nil {
		t.Fatalf("expected inject to fail after close")
	}
}
