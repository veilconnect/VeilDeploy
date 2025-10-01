package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// StreamID identifies a multiplexed stream
type StreamID uint32

// Stream represents a multiplexed connection stream
type Stream struct {
	id        StreamID
	mux       *Multiplexer
	readBuf   chan []byte
	writeCh   chan []byte
	closed    uint32
	closeCh   chan struct{}
	readDeadline  time.Time
	writeDeadline time.Time
	mu        sync.RWMutex
}

// Multiplexer multiplexes multiple streams over a single connection
type Multiplexer struct {
	conn          net.Conn
	streams       map[StreamID]*Stream
	nextStreamID  uint32
	isClient      bool
	mu            sync.RWMutex
	closed        uint32
	closeCh       chan struct{}
	acceptCh      chan *Stream
	maxStreams    int
	streamTimeout time.Duration
}

const (
	// Frame types
	frameTypeData  = 0x01
	frameTypeOpen  = 0x02
	frameTypeClose = 0x03
	frameTypePing  = 0x04
	frameTypePong  = 0x05

	// Frame header size: type(1) + id(4) + length(4) = 9 bytes
	muxFrameHeaderSize = 9

	// Max frame payload size
	maxFrameSize = 65535

	// Default settings
	defaultMaxStreams    = 256
	defaultStreamTimeout = 60 * time.Second
)

// NewMultiplexer creates a new multiplexer
func NewMultiplexer(conn net.Conn, isClient bool) *Multiplexer {
	mux := &Multiplexer{
		conn:          conn,
		streams:       make(map[StreamID]*Stream),
		nextStreamID:  1,
		isClient:      isClient,
		closeCh:       make(chan struct{}),
		acceptCh:      make(chan *Stream, 32),
		maxStreams:    defaultMaxStreams,
		streamTimeout: defaultStreamTimeout,
	}

	// Client uses odd stream IDs, server uses even
	if !isClient {
		mux.nextStreamID = 2
	}

	go mux.readLoop()
	go mux.pingLoop()

	return mux
}

// OpenStream opens a new stream
func (m *Multiplexer) OpenStream() (*Stream, error) {
	if atomic.LoadUint32(&m.closed) == 1 {
		return nil, errors.New("multiplexer closed")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.streams) >= m.maxStreams {
		return nil, errors.New("max streams reached")
	}

	id := StreamID(m.nextStreamID)
	m.nextStreamID += 2 // Increment by 2 to maintain odd/even pattern

	stream := &Stream{
		id:      id,
		mux:     m,
		readBuf: make(chan []byte, 32),
		writeCh: make(chan []byte, 32),
		closeCh: make(chan struct{}),
	}

	m.streams[id] = stream

	// Send OPEN frame
	if err := m.writeFrame(frameTypeOpen, id, nil); err != nil {
		delete(m.streams, id)
		return nil, err
	}

	go m.streamWriter(stream)

	return stream, nil
}

// AcceptStream accepts a new incoming stream
func (m *Multiplexer) AcceptStream() (*Stream, error) {
	select {
	case stream := <-m.acceptCh:
		return stream, nil
	case <-m.closeCh:
		return nil, errors.New("multiplexer closed")
	}
}

// Close closes the multiplexer
func (m *Multiplexer) Close() error {
	if !atomic.CompareAndSwapUint32(&m.closed, 0, 1) {
		return nil
	}

	close(m.closeCh)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Close all streams
	for _, stream := range m.streams {
		stream.close()
	}

	return m.conn.Close()
}

// readLoop reads frames from the connection
func (m *Multiplexer) readLoop() {
	for {
		if atomic.LoadUint32(&m.closed) == 1 {
			return
		}

		frameType, streamID, payload, err := m.readFrame()
		if err != nil {
			if err != io.EOF {
				// Log error
			}
			m.Close()
			return
		}

		switch frameType {
		case frameTypeData:
			m.handleDataFrame(streamID, payload)

		case frameTypeOpen:
			m.handleOpenFrame(streamID)

		case frameTypeClose:
			m.handleCloseFrame(streamID)

		case frameTypePing:
			m.writeFrame(frameTypePong, streamID, payload)

		case frameTypePong:
			// Handle pong
		}
	}
}

// streamWriter writes data for a stream
func (m *Multiplexer) streamWriter(stream *Stream) {
	for {
		select {
		case data := <-stream.writeCh:
			if err := m.writeFrame(frameTypeData, stream.id, data); err != nil {
				stream.close()
				return
			}
		case <-stream.closeCh:
			return
		case <-m.closeCh:
			return
		}
	}
}

// handleDataFrame handles a data frame
func (m *Multiplexer) handleDataFrame(id StreamID, payload []byte) {
	m.mu.RLock()
	stream, exists := m.streams[id]
	m.mu.RUnlock()

	if !exists {
		return
	}

	select {
	case stream.readBuf <- payload:
	case <-stream.closeCh:
	case <-m.closeCh:
	}
}

// handleOpenFrame handles an open frame
func (m *Multiplexer) handleOpenFrame(id StreamID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.streams) >= m.maxStreams {
		m.writeFrame(frameTypeClose, id, nil)
		return
	}

	stream := &Stream{
		id:      id,
		mux:     m,
		readBuf: make(chan []byte, 32),
		writeCh: make(chan []byte, 32),
		closeCh: make(chan struct{}),
	}

	m.streams[id] = stream
	go m.streamWriter(stream)

	select {
	case m.acceptCh <- stream:
	case <-m.closeCh:
		stream.close()
	default:
		// Channel full, reject stream
		stream.close()
	}
}

// handleCloseFrame handles a close frame
func (m *Multiplexer) handleCloseFrame(id StreamID) {
	m.mu.Lock()
	stream, exists := m.streams[id]
	if exists {
		delete(m.streams, id)
	}
	m.mu.Unlock()

	if exists {
		stream.close()
	}
}

// writeFrame writes a frame to the connection
func (m *Multiplexer) writeFrame(frameType byte, id StreamID, payload []byte) error {
	header := make([]byte, muxFrameHeaderSize)
	header[0] = frameType
	putUint32(header[1:], uint32(id))
	putUint32(header[5:], uint32(len(payload)))

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := m.conn.Write(header); err != nil {
		return err
	}

	if len(payload) > 0 {
		if _, err := m.conn.Write(payload); err != nil {
			return err
		}
	}

	return nil
}

// readFrame reads a frame from the connection
func (m *Multiplexer) readFrame() (byte, StreamID, []byte, error) {
	header := make([]byte, muxFrameHeaderSize)
	if _, err := io.ReadFull(m.conn, header); err != nil {
		return 0, 0, nil, err
	}

	frameType := header[0]
	id := StreamID(getUint32(header[1:]))
	length := getUint32(header[5:])

	if length > maxFrameSize {
		return 0, 0, nil, fmt.Errorf("frame too large: %d", length)
	}

	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(m.conn, payload); err != nil {
			return 0, 0, nil, err
		}
	}

	return frameType, id, payload, nil
}

// pingLoop sends periodic pings
func (m *Multiplexer) pingLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.writeFrame(frameTypePing, 0, nil); err != nil {
				m.Close()
				return
			}
		case <-m.closeCh:
			return
		}
	}
}

// Stream methods

// Read reads data from the stream
func (s *Stream) Read(p []byte) (int, error) {
	if atomic.LoadUint32(&s.closed) == 1 {
		return 0, io.EOF
	}

	select {
	case data := <-s.readBuf:
		n := copy(p, data)
		return n, nil
	case <-s.closeCh:
		return 0, io.EOF
	}
}

// Write writes data to the stream
func (s *Stream) Write(p []byte) (int, error) {
	if atomic.LoadUint32(&s.closed) == 1 {
		return 0, errors.New("stream closed")
	}

	// Split large writes into frames
	written := 0
	for written < len(p) {
		end := written + maxFrameSize
		if end > len(p) {
			end = len(p)
		}

		chunk := make([]byte, end-written)
		copy(chunk, p[written:end])

		select {
		case s.writeCh <- chunk:
			written += len(chunk)
		case <-s.closeCh:
			return written, errors.New("stream closed")
		}
	}

	return written, nil
}

// Close closes the stream
func (s *Stream) Close() error {
	s.close()
	s.mux.writeFrame(frameTypeClose, s.id, nil)
	return nil
}

func (s *Stream) close() {
	if atomic.CompareAndSwapUint32(&s.closed, 0, 1) {
		close(s.closeCh)
	}
}

// LocalAddr returns the local address
func (s *Stream) LocalAddr() net.Addr {
	return s.mux.conn.LocalAddr()
}

// RemoteAddr returns the remote address
func (s *Stream) RemoteAddr() net.Addr {
	return s.mux.conn.RemoteAddr()
}

// SetDeadline sets read and write deadlines
func (s *Stream) SetDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readDeadline = t
	s.writeDeadline = t
	return nil
}

// SetReadDeadline sets the read deadline
func (s *Stream) SetReadDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readDeadline = t
	return nil
}

// SetWriteDeadline sets the write deadline
func (s *Stream) SetWriteDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeDeadline = t
	return nil
}

// Helper functions
func putUint32(b []byte, v uint32) {
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
}

func getUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// DialMux creates a multiplexed connection as a client
func DialMux(ctx context.Context, network, address string) (*Multiplexer, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	return NewMultiplexer(conn, true), nil
}
