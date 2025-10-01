package packet

import "testing"

func TestPacketEncodeDecode(t *testing.T) {
	payload := []byte{0x01, 0x02, 0x03}
	pkt, err := NewDataPacket("peer-a", payload)
	if err != nil {
		t.Fatalf("new data packet: %v", err)
	}

	encoded := Encode(pkt)
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	peer, data, err := ExtractData(decoded)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if peer != "peer-a" {
		t.Fatalf("expected peer %q, got %q", "peer-a", peer)
	}
	if string(data) != string(payload) {
		t.Fatalf("expected payload %v, got %v", payload, data)
	}
}

func TestNewDataPacketPeerLength(t *testing.T) {
	longPeer := make([]byte, 300)
	if _, err := NewDataPacket(string(longPeer), nil); err == nil {
		t.Fatalf("expected error for long peer name")
	}
}
