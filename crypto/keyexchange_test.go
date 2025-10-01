package crypto

import (
	"bytes"
	"errors"
	"net"
	"testing"
	"time"
)

func TestHandshakeClientServer(t *testing.T) {
	clientPriv, err := GeneratePrivateKey()
	if err != nil {
		t.Fatalf("client private key: %v", err)
	}
	serverPriv, err := GeneratePrivateKey()
	if err != nil {
		t.Fatalf("server private key: %v", err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	psk := []byte("0123456789abcdef0123456789abcdef")

	serverCh := make(chan *HandshakeResult, 1)
	errCh := make(chan error, 1)
	go func() {
		opts := HandshakeOptions{
			PreSharedKey: psk,
			KeepAlive:    20 * time.Second,
			MaxPadding:   64,
			CookieTTL:    30 * time.Second,
		}
		res, err := PerformHandshake(serverPriv, serverConn, RoleServer, opts)
		if err != nil {
			errCh <- err
			return
		}
		serverCh <- res
	}()

	clientOpts := HandshakeOptions{
		PreSharedKey: psk,
		MaxPadding:   64,
	}
	clientRes, err := PerformHandshake(clientPriv, clientConn, RoleClient, clientOpts)
	if err != nil {
		t.Fatalf("client handshake failed: %v", err)
	}

	select {
	case err := <-errCh:
		t.Fatalf("server handshake failed: %v", err)
	case serverRes := <-serverCh:
		if clientRes == nil || serverRes == nil {
			t.Fatalf("handshake results missing")
		}
		if clientRes.Parameters.KeepAlive <= 0 {
			t.Fatalf("expected positive keepalive, got %v", clientRes.Parameters.KeepAlive)
		}
		if clientRes.Secrets.SessionID != serverRes.Secrets.SessionID {
			t.Fatalf("session ids differ: client=%v server=%v", clientRes.Secrets.SessionID, serverRes.Secrets.SessionID)
		}
		if len(clientRes.Secrets.SendKey) != KeySize || len(serverRes.Secrets.ReceiveKey) != KeySize {
			t.Fatalf("unexpected key lengths")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for server handshake")
	}
}

func TestRekeyFlow(t *testing.T) {
	clientSecrets, serverSecrets := performHandshake(t)
	prevClientSendKey := append([]byte(nil), clientSecrets.SendKey...)
	prevServerRecvKey := append([]byte(nil), serverSecrets.ReceiveKey...)

	ctx, err := NewRekeyRequest(clientSecrets, RoleClient)
	if err != nil {
		t.Fatalf("new rekey request: %v", err)
	}

	updatedServer, response, err := ProcessRekey(serverSecrets, ctx.Payload, nil, RoleServer)
	if !errors.Is(err, ErrRekeyResponseRequired) {
		t.Fatalf("expected ErrRekeyResponseRequired, got %v", err)
	}
	if updatedServer == nil || len(response) == 0 {
		t.Fatalf("server update or response missing")
	}

	updatedClient, _, err := ProcessRekey(clientSecrets, response, ctx, RoleClient)
	if err != nil {
		t.Fatalf("client rekey processing failed: %v", err)
	}
	if updatedClient == nil {
		t.Fatal("client secrets missing after rekey")
	}

	if updatedClient.Epoch != clientSecrets.Epoch+1 {
		t.Fatalf("client epoch not incremented: got %d want %d", updatedClient.Epoch, clientSecrets.Epoch+1)
	}
	if updatedServer.Epoch != serverSecrets.Epoch+1 {
		t.Fatalf("server epoch not incremented: got %d want %d", updatedServer.Epoch, serverSecrets.Epoch+1)
	}
	if bytes.Equal(updatedClient.SendKey, prevClientSendKey) {
		if bytes.Equal(updatedServer.ReceiveKey, prevServerRecvKey) {
			t.Fatalf("receive key did not change")
		}
		t.Fatalf("send key did not change")
	}
}

func performHandshake(t *testing.T) (SessionSecrets, SessionSecrets) {
	t.Helper()
	clientPriv, err := GeneratePrivateKey()
	if err != nil {
		t.Fatalf("client private key: %v", err)
	}
	serverPriv, err := GeneratePrivateKey()
	if err != nil {
		t.Fatalf("server private key: %v", err)
	}

	clientConn, serverConn := net.Pipe()
	psk := []byte("0123456789abcdef0123456789abcdef")

	serverCh := make(chan *HandshakeResult, 1)
	errCh := make(chan error, 1)
	go func() {
		opts := HandshakeOptions{
			PreSharedKey: psk,
			KeepAlive:    20 * time.Second,
			MaxPadding:   32,
			CookieTTL:    30 * time.Second,
		}
		res, err := PerformHandshake(serverPriv, serverConn, RoleServer, opts)
		if err != nil {
			errCh <- err
			return
		}
		serverCh <- res
	}()

	clientOpts := HandshakeOptions{
		PreSharedKey: psk,
		MaxPadding:   32,
	}
	clientRes, err := PerformHandshake(clientPriv, clientConn, RoleClient, clientOpts)
	if err != nil {
		t.Fatalf("client handshake failed: %v", err)
	}

	var serverRes *HandshakeResult
	select {
	case err := <-errCh:
		t.Fatalf("server handshake failed: %v", err)
	case serverRes = <-serverCh:
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for server handshake")
	}

	clientConn.Close()
	serverConn.Close()

	if clientRes == nil || serverRes == nil {
		t.Fatalf("handshake results missing")
	}
	return clientRes.Secrets, serverRes.Secrets
}
