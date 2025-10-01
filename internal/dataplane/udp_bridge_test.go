package dataplane

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestUDPBridgeDeliverAndReceive(t *testing.T) {
	remoteAddr, remoteConn := createUDPListener(t)
	defer remoteConn.Close()

	listen := "127.0.0.1:0"
	bridge, err := NewUDPBridge(listen, map[string]string{"remote": remoteAddr.String()})
	if err != nil {
		t.Fatalf("new udp bridge: %v", err)
	}
	defer bridge.Close()

	// Ensure Deliver sends payload to remote peer
	want := []byte("hello")
	if err := bridge.Deliver("remote", want); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	_ = remoteConn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 64)
	n, _, err := remoteConn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read from remote: %v", err)
	}
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("unexpected payload %q", buf[:n])
	}

	// Send data from remote into the bridge and ensure it reaches outbound channel
	bridgeAddr := bridge.LocalAddr().(*net.UDPAddr)
	inbound := []byte("from-remote")
	if _, err := remoteConn.WriteToUDP(inbound, bridgeAddr); err != nil {
		t.Fatalf("write to bridge: %v", err)
	}

	select {
	case frame := <-bridge.Outbound():
		if frame.Peer != "remote" {
			t.Fatalf("unexpected peer %q", frame.Peer)
		}
		if !bytes.Equal(frame.Payload, inbound) {
			t.Fatalf("unexpected inbound payload %q", frame.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for outbound frame")
	}
}

func createUDPListener(t *testing.T) (*net.UDPAddr, *net.UDPConn) {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve udp: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	return conn.LocalAddr().(*net.UDPAddr), conn
}
