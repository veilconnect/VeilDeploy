package device

import "testing"

func TestDestinationIPv4(t *testing.T) {
	packet := make([]byte, 20)
	packet[0] = 0x45
	copy(packet[16:20], []byte{192, 0, 2, 1})

	addr, err := destinationIP(packet)
	if err != nil {
		t.Fatalf("ipv4 destination: %v", err)
	}
	if got := addr.String(); got != "192.0.2.1" {
		t.Fatalf("expected 192.0.2.1, got %s", got)
	}
}

func TestDestinationIPv6(t *testing.T) {
	packet := make([]byte, 40)
	packet[0] = 0x60
	copy(packet[24:40], []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1})

	addr, err := destinationIP(packet)
	if err != nil {
		t.Fatalf("ipv6 destination: %v", err)
	}
	if got := addr.String(); got != "2001:db8::1" {
		t.Fatalf("expected 2001:db8::1, got %s", got)
	}
}

func TestDestinationIPErrors(t *testing.T) {
	if _, err := destinationIP(nil); err == nil {
		t.Fatalf("expected error for empty payload")
	}
	if _, err := destinationIP([]byte{0x40}); err == nil {
		t.Fatalf("expected error for truncated ipv4")
	}
	if _, err := destinationIP(append([]byte{0x60}, make([]byte, 10)...)); err == nil {
		t.Fatalf("expected error for truncated ipv6")
	}
	if _, err := destinationIP([]byte{0x30, 0, 0, 0}); err == nil {
		t.Fatalf("expected error for unsupported version")
	}
}
