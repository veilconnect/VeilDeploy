package packet

import "errors"

const (
	TypeUnknown uint8 = iota
	TypeData
)

type Packet struct {
	Type    uint8
	Flags   uint8
	Payload []byte
}

func Encode(p *Packet) []byte {
	if p == nil {
		return nil
	}
	data := make([]byte, 2+len(p.Payload))
	data[0] = p.Type
	data[1] = p.Flags
	copy(data[2:], p.Payload)
	return data
}

func Decode(data []byte) (*Packet, error) {
	if len(data) < 2 {
		return nil, errors.New("packet too short")
	}
	pkt := &Packet{
		Type:    data[0],
		Flags:   data[1],
		Payload: make([]byte, len(data)-2),
	}
	copy(pkt.Payload, data[2:])
	return pkt, nil
}

// NewDataPacket builds a data packet for the supplied peer.
func NewDataPacket(peer string, payload []byte) (*Packet, error) {
	if len(peer) > 255 {
		return nil, errors.New("peer name too long")
	}
	buf := make([]byte, len(peer)+len(payload))
	copy(buf, peer)
	copy(buf[len(peer):], payload)
	return &Packet{Type: TypeData, Flags: uint8(len(peer)), Payload: buf}, nil
}

// ExtractData splits a data packet into its logical peer and payload portions.
func ExtractData(pkt *Packet) (peer string, payload []byte, err error) {
	if pkt == nil {
		return "", nil, errors.New("nil packet")
	}
	if pkt.Type != TypeData {
		return "", nil, errors.New("packet is not data")
	}
	peerLen := int(pkt.Flags)
	if peerLen > len(pkt.Payload) {
		return "", nil, errors.New("peer metadata truncated")
	}
	peer = string(pkt.Payload[:peerLen])
	payload = append([]byte(nil), pkt.Payload[peerLen:]...)
	return peer, payload, nil
}
