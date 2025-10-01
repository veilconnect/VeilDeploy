package crypto

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"time"

	"golang.org/x/crypto/curve25519"
)

const (
	rekeyVersion      = 1
	rekeyFlagResponse = 1 << 0
	rekeyNonceSize    = 16
)

var ErrRekeyResponseRequired = errors.New("rekey response required")

type RekeyContext struct {
	Payload        []byte
	role           HandshakeRole
	privateKey     [curve25519.ScalarSize]byte
	initiatorNonce [rekeyNonceSize]byte
}

type rekeyMessage struct {
	Version uint8
	Flags   uint8
	Nonce   [rekeyNonceSize]byte
	Public  [curve25519.PointSize]byte
}

type RekeyRequest struct {
	Payload []byte
}

func NewRekeyRequest(secrets SessionSecrets, role HandshakeRole) (*RekeyContext, error) {
	var nonce [rekeyNonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	private, public, err := ephemeralKeypair()
	if err != nil {
		return nil, err
	}
	msg := rekeyMessage{
		Version: rekeyVersion,
		Flags:   0,
		Nonce:   nonce,
	}
	copy(msg.Public[:], public[:])
	payload := encodeRekeyMessage(msg)
	var ctx RekeyContext
	ctx.Payload = payload
	ctx.role = role
	copy(ctx.privateKey[:], private)
	ctx.initiatorNonce = nonce
	return &ctx, nil
}

func ProcessRekey(current SessionSecrets, payload []byte, pending *RekeyContext, role HandshakeRole) (*SessionSecrets, []byte, error) {
	msg, err := decodeRekeyMessage(payload)
	if err != nil {
		return nil, nil, err
	}

	if (msg.Flags & rekeyFlagResponse) != 0 {
		if pending == nil {
			return nil, nil, errors.New("unexpected rekey response")
		}
		shared, err := deriveSharedSecret(pending.privateKey[:], msg.Public[:])
		if err != nil {
			return nil, nil, err
		}
		secrets, err := deriveRekeySecrets(current, shared, role, pending.initiatorNonce, msg.Nonce, msg.Public)
		if err != nil {
			return nil, nil, err
		}
		return secrets, nil, nil
	}

	private, public, err := ephemeralKeypair()
	if err != nil {
		return nil, nil, err
	}
	var responderNonce [rekeyNonceSize]byte
	if _, err := rand.Read(responderNonce[:]); err != nil {
		return nil, nil, err
	}
	shared, err := deriveSharedSecret(private, msg.Public[:])
	if err != nil {
		return nil, nil, err
	}
	secrets, err := deriveRekeySecrets(current, shared, role, msg.Nonce, responderNonce, msg.Public)
	if err != nil {
		return nil, nil, err
	}
	responseMsg := rekeyMessage{
		Version: rekeyVersion,
		Flags:   rekeyFlagResponse,
		Nonce:   responderNonce,
	}
	copy(responseMsg.Public[:], public[:])
	response := encodeRekeyMessage(responseMsg)
	return secrets, response, ErrRekeyResponseRequired
}

func deriveRekeySecrets(current SessionSecrets, shared []byte, role HandshakeRole, initiatorNonce, responderNonce [rekeyNonceSize]byte, peerPub [32]byte) (*SessionSecrets, error) {
	saltMac := hmac.New(sha256.New, current.ObfuscationKey)
	saltMac.Write(current.SessionID[:])
	saltMac.Write(initiatorNonce[:])
	saltMac.Write(responderNonce[:])
	salt := saltMac.Sum(nil)

	sendLabel := []byte("stp/rekey/send")
	recvLabel := []byte("stp/rekey/recv")
	if role == RoleServer {
		sendLabel, recvLabel = recvLabel, sendLabel
	}

	nonceBuf := append(append([]byte(nil), initiatorNonce[:]...), responderNonce[:]...)
	sendKey, err := expandKey(shared, salt, append(sendLabel, nonceBuf...))
	if err != nil {
		return nil, err
	}
	recvKey, err := expandKey(shared, salt, append(recvLabel, nonceBuf...))
	if err != nil {
		return nil, err
	}
	obfKey, err := expandKey(shared, salt, []byte("stp/rekey/obf"))
	if err != nil {
		return nil, err
	}

	var newSessionID [16]byte
	mac := hmac.New(sha256.New, current.SessionID[:])
	mac.Write(initiatorNonce[:])
	mac.Write(responderNonce[:])
	copy(newSessionID[:], mac.Sum(nil)[:16])

	secrets := &SessionSecrets{
		SessionID:      newSessionID,
		SendKey:        sendKey,
		ReceiveKey:     recvKey,
		ObfuscationKey: obfKey,
		PeerPublicKey:  peerPub,
		Epoch:          current.Epoch + 1,
		Established:    time.Now().UTC(),
	}
	return secrets, nil
}

func encodeRekeyMessage(msg rekeyMessage) []byte {
	buf := make([]byte, 1+1+rekeyNonceSize+curve25519.PointSize)
	buf[0] = msg.Version
	buf[1] = msg.Flags
	copy(buf[2:2+rekeyNonceSize], msg.Nonce[:])
	copy(buf[2+rekeyNonceSize:], msg.Public[:])
	return buf
}

func decodeRekeyMessage(payload []byte) (*rekeyMessage, error) {
	if len(payload) != 1+1+rekeyNonceSize+curve25519.PointSize {
		return nil, errors.New("invalid rekey payload length")
	}
	msg := &rekeyMessage{
		Version: payload[0],
		Flags:   payload[1],
	}
	copy(msg.Nonce[:], payload[2:2+rekeyNonceSize])
	copy(msg.Public[:], payload[2+rekeyNonceSize:])
	if msg.Version != rekeyVersion {
		return nil, errors.New("unsupported rekey version")
	}
	return msg, nil
}
