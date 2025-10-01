package transport

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"crypto/hmac"
	"crypto/sha256"

	"golang.org/x/crypto/hkdf"

	"stp/crypto"
	"stp/internal/logging"
)

const (
	recordHeaderSize = 5
	frameHeaderSize  = 1 + 1 + 8
)

type FrameFlag uint8

const (
	FlagData FrameFlag = 1 << iota
	FlagKeepAlive
	FlagRekey
	FlagBind
)

type Frame struct {
	Flags   FrameFlag
	Payload []byte
}

type Transport struct {
	mu       sync.RWMutex
	session  *sessionState
	lastSend time.Time
	lastRecv time.Time
	logger   *logging.Logger
}

type sessionState struct {
	sendCipher     *crypto.CipherState
	recvCipher     *crypto.CipherState
	obfuscationKey []byte
	sendCounter    uint64
	recvCounter    uint64
	maxPadding     uint8
	keepAlive      time.Duration
	epoch          uint32
}

var ErrSessionUnset = errors.New("transport session not established")

func NewTransport(logger *logging.Logger) *Transport {
	return &Transport{logger: logger}
}

func (t *Transport) InstallSession(secrets crypto.SessionSecrets, params crypto.TransportParameters) error {
	sendCipher, err := crypto.NewCipherState(secrets.SendKey)
	if err != nil {
		return err
	}
	recvCipher, err := crypto.NewCipherState(secrets.ReceiveKey)
	if err != nil {
		return err
	}
	keepalive := params.KeepAlive
	if keepalive <= 0 {
		keepalive = 15 * time.Second
	}
	maxPadding := params.MaxPadding
	if maxPadding == 0 {
		maxPadding = 96
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.session = &sessionState{
		sendCipher:     sendCipher,
		recvCipher:     recvCipher,
		obfuscationKey: append([]byte(nil), secrets.ObfuscationKey...),
		maxPadding:     maxPadding,
		keepAlive:      keepalive,
		epoch:          secrets.Epoch,
	}
	now := time.Now()
	t.lastSend = now
	t.lastRecv = now
	return nil
}

func (t *Transport) UpdateSessionKeys(secrets crypto.SessionSecrets) error {
	sendCipher, err := crypto.NewCipherState(secrets.SendKey)
	if err != nil {
		return err
	}
	recvCipher, err := crypto.NewCipherState(secrets.ReceiveKey)
	if err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.session == nil {
		return ErrSessionUnset
	}
	t.session.sendCipher = sendCipher
	t.session.recvCipher = recvCipher
	t.session.obfuscationKey = append([]byte(nil), secrets.ObfuscationKey...)
	t.session.sendCounter = 0
	t.session.recvCounter = 0
	t.session.epoch = secrets.Epoch
	return nil
}

func (t *Transport) SendPayload(conn net.Conn, payload []byte) error {
	return t.writeFrame(conn, FlagData, payload)
}

func (t *Transport) SendKeepAlive(conn net.Conn) error {
	return t.writeFrame(conn, FlagKeepAlive, nil)
}

func (t *Transport) SendBind(conn net.Conn) error {
	return t.writeFrame(conn, FlagBind, nil)
}

func (t *Transport) SendRekey(conn net.Conn, payload []byte) error {
	return t.writeFrame(conn, FlagRekey, payload)
}

func (t *Transport) writeFrame(conn net.Conn, flag FrameFlag, payload []byte) error {
	t.mu.Lock()
	if t.session == nil {
		t.mu.Unlock()
		return ErrSessionUnset
	}
	sess := t.session

	pad, padLen, err := derivePadding(sess.obfuscationKey, sess.sendCounter, sess.maxPadding)
	if err != nil {
		t.mu.Unlock()
		return err
	}

	aad := []byte{byte(flag), padLen}
	plaintext := buildPlaintext(payload)
	ciphertext, err := sess.sendCipher.Seal(sess.sendCounter, aad, plaintext)
	if err != nil {
		t.mu.Unlock()
		return err
	}

	flagByte, padByte := maskHeader(sess.obfuscationKey, sess.sendCounter, byte(flag), padLen)

	bodyLen := frameHeaderSize + len(ciphertext) + int(padLen)
	if bodyLen > 0xFFFF {
		t.mu.Unlock()
		return errors.New("frame exceeds maximum size")
	}
	body := make([]byte, bodyLen)
	body[0] = flagByte
	body[1] = padByte
	binary.BigEndian.PutUint64(body[2:10], sess.sendCounter)
	copy(body[10:], ciphertext)
	if padLen > 0 {
		copy(body[10+len(ciphertext):], pad)
	}

	header := make([]byte, recordHeaderSize)
	header[0] = 0x17
	header[1] = 0x03
	header[2] = 0x03
	binary.BigEndian.PutUint16(header[3:], uint16(bodyLen))

	sess.sendCounter++
	t.lastSend = time.Now()
	t.mu.Unlock()

	if err := writeAll(conn, header); err != nil {
		return err
	}
	return writeAll(conn, body)
}

func (t *Transport) Receive(conn net.Conn) (*Frame, error) {
	header := make([]byte, recordHeaderSize)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint16(header[3:])
	if length < frameHeaderSize {
		return nil, errors.New("frame too short")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, err
	}

	flagMasked := body[0]
	padMasked := body[1]
	counter := binary.BigEndian.Uint64(body[2:10])

	t.mu.Lock()
	if t.session == nil {
		t.mu.Unlock()
		return nil, ErrSessionUnset
	}
	sess := t.session
	flagByte, padLen := unmaskHeader(sess.obfuscationKey, counter, flagMasked, padMasked)
	if int(padLen) > len(body)-frameHeaderSize {
		t.mu.Unlock()
		return nil, errors.New("invalid padding length")
	}
	ciphertextLen := len(body) - frameHeaderSize - int(padLen)
	if ciphertextLen < 0 {
		t.mu.Unlock()
		return nil, errors.New("invalid ciphertext length")
	}
	ciphertext := body[frameHeaderSize : frameHeaderSize+ciphertextLen]

	aad := []byte{flagByte, padLen}
	plaintext, err := sess.recvCipher.Open(counter, aad, ciphertext)
	if err != nil {
		t.mu.Unlock()
		return nil, err
	}
	if len(plaintext) < 2 {
		t.mu.Unlock()
		return nil, errors.New("plaintext truncated")
	}
	payloadLen := binary.BigEndian.Uint16(plaintext[:2])
	if int(payloadLen) > len(plaintext)-2 {
		t.mu.Unlock()
		return nil, errors.New("declared payload length exceeds data")
	}
	payload := append([]byte(nil), plaintext[2:2+payloadLen]...)

	if counter < sess.recvCounter {
		t.mu.Unlock()
		return nil, errors.New("replayed frame detected")
	}
	sess.recvCounter = counter + 1
	t.lastRecv = time.Now()
	t.mu.Unlock()

	if padLen > 0 && t.logger != nil {
		t.logger.Debug("frame padding consumed", map[string]interface{}{"bytes": padLen})
	}

	return &Frame{Flags: FrameFlag(flagByte), Payload: payload}, nil
}

func (t *Transport) SessionKeepAlive() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.session == nil {
		return 0
	}
	return t.session.keepAlive
}

func (t *Transport) LastActivity() (time.Time, time.Time) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.lastSend, t.lastRecv
}

func buildPlaintext(payload []byte) []byte {
	length := len(payload)
	buf := make([]byte, 2+length)
	binary.BigEndian.PutUint16(buf[:2], uint16(length))
	copy(buf[2:], payload)
	return buf
}

func writeAll(conn net.Conn, data []byte) error {
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}

func derivePadding(seed []byte, counter uint64, max uint8) ([]byte, uint8, error) {
	if max == 0 || len(seed) == 0 {
		return nil, 0, nil
	}
	var counterBytes [8]byte
	binary.BigEndian.PutUint64(counterBytes[:], counter)
	reader := hkdf.New(sha256.New, seed, counterBytes[:], []byte("stp/padding"))
	var lengthBuf [1]byte
	if _, err := io.ReadFull(reader, lengthBuf[:]); err != nil {
		return nil, 0, err
	}
	limit := int(max) + 1
	padLen := int(lengthBuf[0]) % limit
	if padLen == 0 {
		return nil, 0, nil
	}
	pad := make([]byte, padLen)
	if _, err := io.ReadFull(reader, pad); err != nil {
		return nil, 0, err
	}
	return pad, uint8(padLen), nil
}

func maskHeader(seed []byte, counter uint64, flag byte, padLen uint8) (byte, byte) {
	if len(seed) == 0 {
		return flag, byte(padLen)
	}
	maskA := headerMask(seed, counter, 0)
	maskB := headerMask(seed, counter, 1)
	return flag ^ maskA, byte(padLen) ^ maskB
}

func unmaskHeader(seed []byte, counter uint64, maskedFlag byte, maskedPad byte) (byte, uint8) {
	if len(seed) == 0 {
		return maskedFlag, uint8(maskedPad)
	}
	maskA := headerMask(seed, counter, 0)
	maskB := headerMask(seed, counter, 1)
	return maskedFlag ^ maskA, uint8(maskedPad ^ maskB)
}

func headerMask(seed []byte, counter uint64, offset byte) byte {
	if len(seed) == 0 {
		return 0
	}
	var counterBytes [8]byte
	binary.BigEndian.PutUint64(counterBytes[:], counter)
	mac := hmac.New(sha256.New, seed)
	mac.Write(counterBytes[:])
	mac.Write([]byte{offset})
	sum := mac.Sum(nil)
	return sum[offset%byte(len(sum))]
}

func Dial(network, addr string) (net.Conn, error) {
	switch network {
	case "udp", "udp4", "udp6":
		return dialUDP(addr)
	default:
		return net.Dial(network, addr)
	}
}

func Listen(network, addr string) (Listener, error) {
	switch network {
	case "udp", "udp4", "udp6":
		return newUDPListener(addr)
	default:
		ln, err := net.Listen(network, addr)
		if err != nil {
			return nil, err
		}
		return &tcpListener{Listener: ln}, nil
	}
}

type Listener interface {
	Accept() (net.Conn, error)
	Close() error
	Addr() net.Addr
}

type tcpListener struct {
	net.Listener
}

func (l *tcpListener) Accept() (net.Conn, error) {
	return l.Listener.Accept()
}

type udpListener struct {
	conn     *net.UDPConn
	sessions map[string]*udpSession
	mu       sync.RWMutex
	accept   chan *udpSession
	closed   bool
}

func newUDPListener(addr string) (*udpListener, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}

	listener := &udpListener{
		conn:     conn,
		sessions: make(map[string]*udpSession),
		accept:   make(chan *udpSession, 16),
	}

	// Start background goroutine to demultiplex UDP packets
	go listener.demux()

	return listener, nil
}

func (l *udpListener) demux() {
	buf := make([]byte, 65536) // Max UDP packet size
	for {
		n, remote, err := l.conn.ReadFromUDP(buf)
		if err != nil {
			l.mu.RLock()
			closed := l.closed
			l.mu.RUnlock()
			if closed {
				return
			}
			continue
		}

		if n == 0 {
			continue
		}

		// Create session key from remote address
		key := remote.String()

		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			return
		}

		session, exists := l.sessions[key]
		if !exists {
			// New connection
			session = newUDPSession(l.conn, remote, func() {
				l.removeSession(key)
			})
			l.sessions[key] = session

			// Push initial data
			session.pushData(buf[:n])

			// Send to accept channel (non-blocking)
			select {
			case l.accept <- session:
			default:
				// Accept channel full, drop the session
				delete(l.sessions, key)
				session.closeInternal()
			}
		} else {
			// Existing connection, push data to session
			session.pushData(buf[:n])
		}
		l.mu.Unlock()
	}
}

func (l *udpListener) removeSession(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.sessions, key)
}

func (l *udpListener) Accept() (net.Conn, error) {
	session, ok := <-l.accept
	if !ok {
		return nil, net.ErrClosed
	}
	return session, nil
}

func (l *udpListener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true

	// Close all sessions first
	sessions := make([]*udpSession, 0, len(l.sessions))
	for _, session := range l.sessions {
		sessions = append(sessions, session)
	}
	l.sessions = make(map[string]*udpSession)
	l.mu.Unlock()

	// Close sessions without holding lock
	for _, session := range sessions {
		session.closeInternal()
	}

	// Close accept channel
	close(l.accept)

	// Close underlying connection
	return l.conn.Close()
}

func (l *udpListener) Addr() net.Addr {
	return l.conn.LocalAddr()
}

type udpSession struct {
	conn          *net.UDPConn
	remote        *net.UDPAddr
	mu            sync.Mutex
	pending       []byte
	closed        bool
	readDeadline  time.Time
	writeDeadline time.Time
	readChan      chan []byte
	cleanupFunc   func()
}

func newUDPSession(conn *net.UDPConn, remote *net.UDPAddr, cleanup func()) *udpSession {
	return &udpSession{
		conn:        conn,
		remote:      remote,
		readChan:    make(chan []byte, 64),
		cleanupFunc: cleanup,
	}
}

func (s *udpSession) pushData(data []byte) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	// Make a copy of the data
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	// Try to send to read channel (non-blocking)
	select {
	case s.readChan <- dataCopy:
	default:
		// Channel full, drop packet (UDP semantics)
	}
}

func (s *udpSession) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, io.EOF
	}

	// Check if we have pending data from previous read
	if len(s.pending) > 0 {
		n := copy(p, s.pending)
		s.pending = s.pending[n:]
		s.mu.Unlock()
		return n, nil
	}

	deadline := s.readDeadline
	s.mu.Unlock()

	// Wait for data from read channel
	var data []byte
	if deadline.IsZero() {
		// No deadline, block indefinitely
		select {
		case data = <-s.readChan:
		case <-time.After(time.Hour): // Prevent infinite block
			return 0, io.EOF
		}
	} else {
		// With deadline
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()

		select {
		case data = <-s.readChan:
		case <-timer.C:
			return 0, os.ErrDeadlineExceeded
		}
	}

	// Copy data to output buffer
	n := copy(p, data)
	if n < len(data) {
		// Save remaining data for next read
		s.mu.Lock()
		s.pending = append(s.pending, data[n:]...)
		s.mu.Unlock()
	}

	return n, nil
}

func (s *udpSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, io.EOF
	}
	deadline := s.writeDeadline
	s.mu.Unlock()

	if !deadline.IsZero() {
		_ = s.conn.SetWriteDeadline(deadline)
	}
	return s.conn.WriteToUDP(p, s.remote)
}

func (s *udpSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.readChan)
	cleanup := s.cleanupFunc
	s.mu.Unlock()

	// Call cleanup without holding lock
	if cleanup != nil {
		cleanup()
	}
	return nil
}

// closeInternal closes without calling cleanup (used by cleanup itself)
func (s *udpSession) closeInternal() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.readChan)
}

func (s *udpSession) LocalAddr() net.Addr {
	return s.conn.LocalAddr()
}

func (s *udpSession) RemoteAddr() net.Addr {
	return s.remote
}

func (s *udpSession) SetDeadline(t time.Time) error {
	s.mu.Lock()
	s.readDeadline = t
	s.writeDeadline = t
	s.mu.Unlock()
	return nil
}

func (s *udpSession) SetReadDeadline(t time.Time) error {
	s.mu.Lock()
	s.readDeadline = t
	s.mu.Unlock()
	return nil
}

func (s *udpSession) SetWriteDeadline(t time.Time) error {
	s.mu.Lock()
	s.writeDeadline = t
	s.mu.Unlock()
	return nil
}

func dialUDP(addr string) (net.Conn, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	return net.DialUDP("udp", nil, udpAddr)
}
