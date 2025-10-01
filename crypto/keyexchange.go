package crypto

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

type HandshakeRole int

type HandshakeOptions struct {
	PreSharedKey []byte
	KeepAlive    time.Duration
	MaxPadding   uint8
	CookieTTL    time.Duration
}

type TransportParameters struct {
	KeepAlive  time.Duration
	MaxPadding uint8
}

type SessionSecrets struct {
	SessionID      [16]byte
	SendKey        []byte
	ReceiveKey     []byte
	ObfuscationKey []byte
	PeerPublicKey  [32]byte
	Epoch          uint32
	Established    time.Time
}

type HandshakeResult struct {
	Secrets    SessionSecrets
	Parameters TransportParameters
}

const (
	RoleClient HandshakeRole = iota
	RoleServer

	handshakeVersion = 1
	handshakeMacSize = 16

	msgTypeClientHello = 1
	msgTypeServerHello = 2
	msgTypeCookie      = 3

	clientFlagHasCookie = 0x01

	recordHeaderSize = 5
	cookieMacSize    = 16
)

var ()

func GeneratePrivateKey() ([]byte, error) {
	key := make([]byte, curve25519.ScalarSize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	key[0] &= 248
	key[31] &= 127
	key[31] |= 64
	return key, nil
}

func PerformHandshake(privateKey []byte, conn net.Conn, role HandshakeRole, opts HandshakeOptions) (*HandshakeResult, error) {
	if len(privateKey) != curve25519.ScalarSize {
		return nil, errors.New("invalid private key length")
	}
	if len(opts.PreSharedKey) == 0 {
		return nil, errors.New("pre-shared key required")
	}

	switch role {
	case RoleClient:
		return clientHandshake(privateKey, conn, opts)
	case RoleServer:
		return serverHandshake(privateKey, conn, opts)
	default:
		return nil, errors.New("unknown handshake role")
	}
}

func clientHandshake(privateKey []byte, conn net.Conn, opts HandshakeOptions) (*HandshakeResult, error) {
	sessionID, err := randomSessionID()
	if err != nil {
		return nil, err
	}

	clientEphemeral, clientPub, err := ephemeralKeypair()
	if err != nil {
		return nil, err
	}

	padLimit := opts.MaxPadding
	if padLimit == 0 {
		padLimit = 96
	}

	mac := computeMAC(opts.PreSharedKey, sessionID[:], clientPub[:])
	cookie := []byte(nil)
	attempts := 0

resend:
	padding, err := randomPadding(padLimit)
	if err != nil {
		return nil, err
	}
	clientHello := encodeClientHello(sessionID, clientPub, cookie, padding, mac)

	if err := writeRecord(conn, clientHello); err != nil {
		return nil, err
	}

	transcript := bytes.NewBuffer(clientHello)

	payload, err := readRecord(conn)
	if err != nil {
		return nil, err
	}

	switch payload[0] {
	case msgTypeCookie:
		cookieMsg, err := decodeCookieMessage(payload)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(cookieMsg.SessionID[:], sessionID[:]) {
			return nil, errors.New("cookie session mismatch")
		}
		cookie = cookieMsg.Cookie
		attempts++
		if attempts > 3 {
			return nil, errors.New("too many cookie retries")
		}
		transcript.Reset()
		goto resend
	case msgTypeServerHello:
		transcript.Write(payload)
	default:
		return nil, fmt.Errorf("unexpected handshake message type %d", payload[0])
	}

	serverMsg, err := decodeServerHello(payload)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(serverMsg.SessionID[:], sessionID[:]) {
		return nil, errors.New("session identifier mismatch")
	}

	expectedMac := computeMAC(opts.PreSharedKey, sessionID[:], clientPub[:], serverMsg.PublicKey[:])
	if !hmac.Equal(serverMsg.MAC[:], expectedMac[:]) {
		return nil, errors.New("server MAC verification failed")
	}

	sharedSecret, err := deriveSharedSecret(clientEphemeral, serverMsg.PublicKey[:])
	if err != nil {
		return nil, err
	}

	secrets, err := deriveSessionSecrets(sharedSecret, transcript.Bytes(), opts.PreSharedKey, RoleClient, sessionID, serverMsg.PublicKey)
	if err != nil {
		return nil, err
	}

	params := TransportParameters{
		KeepAlive:  serverMsg.KeepAlive,
		MaxPadding: serverMsg.MaxPadding,
	}

	return &HandshakeResult{Secrets: *secrets, Parameters: params}, nil
}

func serverHandshake(privateKey []byte, conn net.Conn, opts HandshakeOptions) (*HandshakeResult, error) {
	if opts.KeepAlive == 0 {
		opts.KeepAlive = 15 * time.Second
	}
	if opts.MaxPadding == 0 {
		opts.MaxPadding = 96
	}
	if opts.CookieTTL <= 0 {
		opts.CookieTTL = 60 * time.Second
	}

	remote := conn.RemoteAddr().String()

	var clientMsg *clientHelloMessage
	attempts := 0
	for {
		payload, err := readRecord(conn)
		if err != nil {
			return nil, err
		}
		msg, err := decodeClientHello(payload)
		if err != nil {
			return nil, err
		}

		mac := computeMAC(opts.PreSharedKey, msg.SessionID[:], msg.PublicKey[:])
		if !hmac.Equal(msg.MAC[:], mac[:]) {
			return nil, errors.New("client MAC verification failed")
		}

		if len(msg.Cookie) == 0 || !verifyCookie(opts.PreSharedKey, remote, msg.SessionID, msg.PublicKey, msg.Cookie, opts.CookieTTL) {
			cookiePayload := encodeCookieMessage(msg.SessionID, issueCookieNow(opts.PreSharedKey, remote, msg.SessionID, msg.PublicKey))
			if err := writeRecord(conn, cookiePayload); err != nil {
				return nil, err
			}
			attempts++
			if attempts > 3 {
				return nil, errors.New("client failed cookie validation")
			}
			continue
		}

		clientMsg = msg
		break
	}

	serverEphemeral, serverPub, err := ephemeralKeypair()
	if err != nil {
		return nil, err
	}

	padding, err := randomPadding(opts.MaxPadding)
	if err != nil {
		return nil, err
	}

	keepAlive := opts.KeepAlive
	keepAliveMillis := uint16(keepAlive / time.Millisecond)
	serverMac := computeMAC(opts.PreSharedKey, clientMsg.SessionID[:], clientMsg.PublicKey[:], serverPub[:])
	serverHello := encodeServerHello(clientMsg.SessionID, serverPub, keepAliveMillis, opts.MaxPadding, padding, serverMac)

	if err := writeRecord(conn, serverHello); err != nil {
		return nil, err
	}

	transcript := bytes.NewBuffer(nil)
	transcript.Write(encodeClientHello(clientMsg.SessionID, clientMsg.PublicKey, clientMsg.Cookie, clientMsg.Padding, clientMsg.MAC))
	transcript.Write(serverHello)

	sharedSecret, err := deriveSharedSecret(serverEphemeral, clientMsg.PublicKey[:])
	if err != nil {
		return nil, err
	}

	secrets, err := deriveSessionSecrets(sharedSecret, transcript.Bytes(), opts.PreSharedKey, RoleServer, clientMsg.SessionID, clientMsg.PublicKey)
	if err != nil {
		return nil, err
	}

	params := TransportParameters{
		KeepAlive:  keepAlive,
		MaxPadding: opts.MaxPadding,
	}

	return &HandshakeResult{Secrets: *secrets, Parameters: params}, nil
}

func randomSessionID() ([16]byte, error) {
	var id [16]byte
	_, err := rand.Read(id[:])
	return id, err
}

func ephemeralKeypair() ([]byte, [32]byte, error) {
	priv, err := GeneratePrivateKey()
	if err != nil {
		return nil, [32]byte{}, err
	}
	pub, err := derivePublicKey(priv)
	if err != nil {
		return nil, [32]byte{}, err
	}
	return priv, pub, nil
}

func derivePublicKey(privateKey []byte) ([32]byte, error) {
	if len(privateKey) != curve25519.ScalarSize {
		return [32]byte{}, errors.New("invalid private key length")
	}
	var privateArray [curve25519.ScalarSize]byte
	copy(privateArray[:], privateKey)
	var publicArray [curve25519.PointSize]byte
	curve25519.ScalarBaseMult(&publicArray, &privateArray)
	return publicArray, nil
}

func deriveSharedSecret(privateKey []byte, peerPublic []byte) ([]byte, error) {
	if len(privateKey) != curve25519.ScalarSize {
		return nil, errors.New("invalid private key length")
	}
	if len(peerPublic) != curve25519.PointSize {
		return nil, errors.New("invalid peer public key length")
	}
	var privateArray [curve25519.ScalarSize]byte
	copy(privateArray[:], privateKey)
	var peerArray [curve25519.PointSize]byte
	copy(peerArray[:], peerPublic)
	var shared [curve25519.PointSize]byte
	curve25519.ScalarMult(&shared, &privateArray, &peerArray)
	out := make([]byte, curve25519.PointSize)
	copy(out, shared[:])
	return out, nil
}

func computeMAC(psk []byte, parts ...[]byte) [handshakeMacSize]byte {
	mac := hmac.New(sha256.New, psk)
	for _, part := range parts {
		mac.Write(part)
	}
	var out [handshakeMacSize]byte
	sum := mac.Sum(nil)
	copy(out[:], sum[:handshakeMacSize])
	return out
}

func randomPadding(max uint8) ([]byte, error) {
	if max == 0 {
		return nil, nil
	}
	sizeBuf := make([]byte, 1)
	if _, err := rand.Read(sizeBuf); err != nil {
		return nil, err
	}
	length := int(sizeBuf[0]) % int(max+1)
	if length == 0 {
		return nil, nil
	}
	padding := make([]byte, length)
	if _, err := rand.Read(padding); err != nil {
		return nil, err
	}
	return padding, nil
}

func deriveSessionSecrets(sharedSecret, transcript, psk []byte, role HandshakeRole, sessionID [16]byte, peerPub [32]byte) (*SessionSecrets, error) {
	saltMac := computeMAC(psk, transcript)

	sendLabel := []byte("stp/send")
	recvLabel := []byte("stp/recv")
	if role == RoleServer {
		sendLabel, recvLabel = recvLabel, sendLabel
	}

	sendKey, err := expandKey(sharedSecret, saltMac[:], sendLabel)
	if err != nil {
		return nil, err
	}
	recvKey, err := expandKey(sharedSecret, saltMac[:], recvLabel)
	if err != nil {
		return nil, err
	}
	obfKey, err := expandKey(sharedSecret, saltMac[:], []byte("stp/obf"))
	if err != nil {
		return nil, err
	}

	secrets := &SessionSecrets{
		SessionID:      sessionID,
		SendKey:        sendKey,
		ReceiveKey:     recvKey,
		ObfuscationKey: obfKey,
		PeerPublicKey:  peerPub,
		Epoch:          1,
		Established:    time.Now().UTC(),
	}
	return secrets, nil
}

func expandKey(sharedSecret, salt, info []byte) ([]byte, error) {
	reader := hkdf.New(sha256.New, sharedSecret, salt, info)
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

func writeRecord(conn net.Conn, payload []byte) error {
	if len(payload) > 0xFFFF {
		return errors.New("handshake payload too large")
	}
	header := make([]byte, recordHeaderSize)
	header[0] = 0x17
	header[1] = 0x03
	header[2] = 0x03
	binary.BigEndian.PutUint16(header[3:], uint16(len(payload)))
	if _, err := conn.Write(header); err != nil {
		return err
	}
	_, err := conn.Write(payload)
	return err
}

func readRecord(conn net.Conn) ([]byte, error) {
	header := make([]byte, recordHeaderSize)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint16(header[3:])
	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

type clientHelloMessage struct {
	Flags     uint8
	SessionID [16]byte
	PublicKey [32]byte
	Cookie    []byte
	Padding   []byte
	MAC       [handshakeMacSize]byte
}

type serverHelloMessage struct {
	Flags      uint8
	SessionID  [16]byte
	PublicKey  [32]byte
	KeepAlive  time.Duration
	MaxPadding uint8
	Padding    []byte
	MAC        [handshakeMacSize]byte
}

type cookieMessage struct {
	SessionID [16]byte
	Cookie    []byte
}

func encodeClientHello(sessionID [16]byte, publicKey [32]byte, cookie []byte, padding []byte, mac [handshakeMacSize]byte) []byte {
	buf := bytes.NewBuffer(nil)
	var flags uint8
	if len(cookie) > 0 {
		flags |= clientFlagHasCookie
	}
	buf.WriteByte(msgTypeClientHello)
	buf.WriteByte(handshakeVersion)
	buf.WriteByte(flags)
	buf.Write(sessionID[:])
	buf.Write(publicKey[:])
	buf.WriteByte(uint8(len(cookie)))
	buf.Write(cookie)
	buf.WriteByte(uint8(len(padding)))
	buf.Write(padding)
	buf.Write(mac[:])
	return buf.Bytes()
}

func decodeClientHello(payload []byte) (*clientHelloMessage, error) {
	if len(payload) < 1+1+1+16+32+1+handshakeMacSize {
		return nil, errors.New("client hello too short")
	}
	if payload[0] != msgTypeClientHello {
		return nil, errors.New("unexpected client handshake type")
	}
	if payload[1] != handshakeVersion {
		return nil, errors.New("unsupported handshake version")
	}
	offset := 3
	var sessionID [16]byte
	copy(sessionID[:], payload[offset:offset+16])
	offset += 16
	var publicKey [32]byte
	copy(publicKey[:], payload[offset:offset+32])
	offset += 32
	cookieLen := int(payload[offset])
	offset++
	if len(payload) < offset+cookieLen+1+handshakeMacSize {
		return nil, errors.New("client hello truncated")
	}
	cookie := append([]byte(nil), payload[offset:offset+cookieLen]...)
	offset += cookieLen
	paddingLen := int(payload[offset])
	offset++
	if len(payload) < offset+paddingLen+handshakeMacSize {
		return nil, errors.New("client hello truncated (padding)")
	}
	padding := append([]byte(nil), payload[offset:offset+paddingLen]...)
	offset += paddingLen
	var mac [handshakeMacSize]byte
	copy(mac[:], payload[offset:offset+handshakeMacSize])

	return &clientHelloMessage{
		Flags:     payload[2],
		SessionID: sessionID,
		PublicKey: publicKey,
		Cookie:    cookie,
		Padding:   padding,
		MAC:       mac,
	}, nil
}

func encodeServerHello(sessionID [16]byte, publicKey [32]byte, keepAliveMillis uint16, maxPadding uint8, padding []byte, mac [handshakeMacSize]byte) []byte {
	buf := bytes.NewBuffer(nil)
	buf.WriteByte(msgTypeServerHello)
	buf.WriteByte(handshakeVersion)
	buf.WriteByte(0)
	buf.Write(sessionID[:])
	buf.Write(publicKey[:])
	buf.WriteByte(uint8(maxPadding))
	var keepAliveField [2]byte
	binary.BigEndian.PutUint16(keepAliveField[:], keepAliveMillis)
	buf.Write(keepAliveField[:])
	buf.WriteByte(uint8(len(padding)))
	buf.Write(padding)
	buf.Write(mac[:])
	return buf.Bytes()
}

func decodeServerHello(payload []byte) (*serverHelloMessage, error) {
	minimum := 1 + 1 + 1 + 16 + 32 + 1 + 2 + 1 + handshakeMacSize
	if len(payload) < minimum {
		return nil, errors.New("server hello too short")
	}
	if payload[0] != msgTypeServerHello {
		return nil, errors.New("unexpected server handshake type")
	}
	if payload[1] != handshakeVersion {
		return nil, errors.New("unsupported server handshake version")
	}
	offset := 3
	var sessionID [16]byte
	copy(sessionID[:], payload[offset:offset+16])
	offset += 16
	var publicKey [32]byte
	copy(publicKey[:], payload[offset:offset+32])
	offset += 32
	maxPadding := payload[offset]
	offset++
	keepAliveMillis := binary.BigEndian.Uint16(payload[offset : offset+2])
	offset += 2
	paddingLen := int(payload[offset])
	offset++
	if len(payload) < offset+paddingLen+handshakeMacSize {
		return nil, errors.New("server hello truncated")
	}
	padding := append([]byte(nil), payload[offset:offset+paddingLen]...)
	offset += paddingLen
	var mac [handshakeMacSize]byte
	copy(mac[:], payload[offset:offset+handshakeMacSize])

	msg := &serverHelloMessage{
		Flags:      payload[2],
		SessionID:  sessionID,
		PublicKey:  publicKey,
		KeepAlive:  time.Duration(keepAliveMillis) * time.Millisecond,
		MaxPadding: maxPadding,
		Padding:    padding,
		MAC:        mac,
	}
	return msg, nil
}

func encodeCookieMessage(sessionID [16]byte, cookie []byte) []byte {
	buf := bytes.NewBuffer(nil)
	buf.WriteByte(msgTypeCookie)
	buf.WriteByte(handshakeVersion)
	buf.WriteByte(0)
	buf.Write(sessionID[:])
	buf.WriteByte(uint8(len(cookie)))
	buf.Write(cookie)
	return buf.Bytes()
}

func decodeCookieMessage(payload []byte) (*cookieMessage, error) {
	if len(payload) < 1+1+1+16+1 {
		return nil, errors.New("cookie message too short")
	}
	if payload[0] != msgTypeCookie {
		return nil, errors.New("unexpected cookie message type")
	}
	if payload[1] != handshakeVersion {
		return nil, errors.New("unsupported cookie version")
	}
	offset := 3
	var sessionID [16]byte
	copy(sessionID[:], payload[offset:offset+16])
	offset += 16
	length := int(payload[offset])
	offset++
	if len(payload) < offset+length {
		return nil, errors.New("cookie message truncated")
	}
	cookie := append([]byte(nil), payload[offset:offset+length]...)
	return &cookieMessage{SessionID: sessionID, Cookie: cookie}, nil
}

func issueCookie(psk []byte, remote string, sessionID [16]byte, clientPub [32]byte, at time.Time) []byte {
	at = at.UTC()
	var ts [4]byte
	binary.BigEndian.PutUint32(ts[:], uint32(at.Unix()))
	mac := hmac.New(sha256.New, psk)
	mac.Write([]byte(remote))
	mac.Write(sessionID[:])
	mac.Write(clientPub[:])
	mac.Write(ts[:])
	sum := mac.Sum(nil)
	cookie := make([]byte, 4+cookieMacSize)
	copy(cookie[:4], ts[:])
	copy(cookie[4:], sum[:cookieMacSize])
	return cookie
}

func issueCookieNow(psk []byte, remote string, sessionID [16]byte, clientPub [32]byte) []byte {
	return issueCookie(psk, remote, sessionID, clientPub, time.Now())
}

func verifyCookie(psk []byte, remote string, sessionID [16]byte, clientPub [32]byte, cookie []byte, ttl time.Duration) bool {
	if len(cookie) < 4+cookieMacSize {
		return false
	}
	timestamp := binary.BigEndian.Uint32(cookie[:4])
	issued := time.Unix(int64(timestamp), 0).UTC()
	if time.Since(issued) > ttl {
		return false
	}
	expected := issueCookie(psk, remote, sessionID, clientPub, issued)
	return hmac.Equal(cookie, expected)
}
