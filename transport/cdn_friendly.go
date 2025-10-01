package transport

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// CDNMode CDN模式
type CDNMode int

const (
	CDNModeNone       CDNMode = iota // 直连模式
	CDNModeWebSocket                 // WebSocket模式
	CDNModeHTTP2                     // HTTP/2模式
	CDNModeTLS                       // 纯TLS模式
	CDNModeWebSocketTLS              // WebSocket over TLS
)

// CDNConfig CDN配置
type CDNConfig struct {
	Mode         CDNMode       // CDN模式
	Host         string        // 主机名（用于SNI和Host头）
	Path         string        // WebSocket路径
	TLSConfig    *tls.Config   // TLS配置
	Headers      http.Header   // 自定义HTTP头
	EnableMux    bool          // 启用多路复用
	IdleTimeout  time.Duration // 空闲超时
	PingInterval time.Duration // Ping间隔
}

// DefaultCDNConfig 默认CDN配置
func DefaultCDNConfig(host, path string) CDNConfig {
	return CDNConfig{
		Mode:         CDNModeWebSocketTLS,
		Host:         host,
		Path:         path,
		TLSConfig:    &tls.Config{ServerName: host},
		Headers:      make(http.Header),
		EnableMux:    true,
		IdleTimeout:  60 * time.Second,
		PingInterval: 30 * time.Second,
	}
}

// CDNTransport CDN传输层
type CDNTransport struct {
	config CDNConfig
	conn   net.Conn
	wsConn *websocket.Conn
	mu     sync.RWMutex

	// 读写缓冲
	readBuf  []byte
	writeBuf []byte

	// 状态
	closed      bool
	lastPing    time.Time
	lastPong    time.Time
	pingTicker  *time.Ticker
	stopChan    chan struct{}

	// 统计
	bytesSent     uint64
	bytesReceived uint64
}

// NewCDNTransport 创建CDN传输层
func NewCDNTransport(config CDNConfig) *CDNTransport {
	return &CDNTransport{
		config:   config,
		readBuf:  make([]byte, 0, 32768),
		writeBuf: make([]byte, 0, 32768),
		stopChan: make(chan struct{}),
	}
}

// Dial 连接到服务器
func (ct *CDNTransport) Dial(address string) error {
	switch ct.config.Mode {
	case CDNModeWebSocket, CDNModeWebSocketTLS:
		return ct.dialWebSocket(address)
	case CDNModeTLS:
		return ct.dialTLS(address)
	case CDNModeHTTP2:
		return ct.dialHTTP2(address)
	default:
		return ct.dialDirect(address)
	}
}

// dialWebSocket 使用WebSocket连接
func (ct *CDNTransport) dialWebSocket(address string) error {
	// 构建WebSocket URL
	scheme := "ws"
	if ct.config.Mode == CDNModeWebSocketTLS {
		scheme = "wss"
	}

	u := url.URL{
		Scheme: scheme,
		Host:   address,
		Path:   ct.config.Path,
	}

	// 设置HTTP头
	headers := ct.config.Headers
	if headers == nil {
		headers = make(http.Header)
	}

	// 设置Host头
	if ct.config.Host != "" {
		headers.Set("Host", ct.config.Host)
	}

	// 设置User-Agent（伪装成浏览器）
	headers.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	// 配置拨号器
	dialer := &websocket.Dialer{
		TLSClientConfig:  ct.config.TLSConfig,
		HandshakeTimeout: 10 * time.Second,
		Subprotocols:     []string{"binary"},
	}

	// 连接
	wsConn, _, err := dialer.Dial(u.String(), headers)
	if err != nil {
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	ct.mu.Lock()
	ct.wsConn = wsConn
	ct.mu.Unlock()

	// 启动ping协程
	if ct.config.PingInterval > 0 {
		go ct.pingRoutine()
	}

	return nil
}

// dialTLS 使用纯TLS连接
func (ct *CDNTransport) dialTLS(address string) error {
	conn, err := tls.Dial("tcp", address, ct.config.TLSConfig)
	if err != nil {
		return fmt.Errorf("tls dial failed: %w", err)
	}

	ct.mu.Lock()
	ct.conn = conn
	ct.mu.Unlock()

	return nil
}

// dialHTTP2 使用HTTP/2连接
func (ct *CDNTransport) dialHTTP2(address string) error {
	// HTTP/2通常在TLS之上
	tlsConn, err := tls.Dial("tcp", address, ct.config.TLSConfig)
	if err != nil {
		return fmt.Errorf("tls dial failed: %w", err)
	}

	// 验证协商的协议是否为h2
	state := tlsConn.ConnectionState()
	if state.NegotiatedProtocol != "h2" {
		tlsConn.Close()
		return fmt.Errorf("http/2 not negotiated")
	}

	ct.mu.Lock()
	ct.conn = tlsConn
	ct.mu.Unlock()

	return nil
}

// dialDirect 直接连接
func (ct *CDNTransport) dialDirect(address string) error {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return fmt.Errorf("direct dial failed: %w", err)
	}

	ct.mu.Lock()
	ct.conn = conn
	ct.mu.Unlock()

	return nil
}

// Read 读取数据
func (ct *CDNTransport) Read(b []byte) (n int, err error) {
	ct.mu.RLock()
	wsConn := ct.wsConn
	conn := ct.conn
	ct.mu.RUnlock()

	if wsConn != nil {
		return ct.readWebSocket(b)
	}

	if conn != nil {
		n, err = conn.Read(b)
		if err == nil {
			ct.bytesReceived += uint64(n)
		}
		return n, err
	}

	return 0, fmt.Errorf("no connection")
}

// readWebSocket 从WebSocket读取数据
func (ct *CDNTransport) readWebSocket(b []byte) (int, error) {
	ct.mu.RLock()
	wsConn := ct.wsConn
	ct.mu.RUnlock()

	if wsConn == nil {
		return 0, fmt.Errorf("websocket connection is nil")
	}

	// 读取WebSocket消息
	messageType, message, err := wsConn.ReadMessage()
	if err != nil {
		return 0, err
	}

	// 只处理二进制消息
	if messageType != websocket.BinaryMessage {
		return 0, fmt.Errorf("unexpected message type: %d", messageType)
	}

	// 复制到缓冲区
	n := copy(b, message)
	ct.bytesReceived += uint64(n)

	return n, nil
}

// Write 写入数据
func (ct *CDNTransport) Write(b []byte) (n int, err error) {
	ct.mu.RLock()
	wsConn := ct.wsConn
	conn := ct.conn
	ct.mu.RUnlock()

	if wsConn != nil {
		return ct.writeWebSocket(b)
	}

	if conn != nil {
		n, err = conn.Write(b)
		if err == nil {
			ct.bytesSent += uint64(n)
		}
		return n, err
	}

	return 0, fmt.Errorf("no connection")
}

// writeWebSocket 向WebSocket写入数据
func (ct *CDNTransport) writeWebSocket(b []byte) (int, error) {
	ct.mu.RLock()
	wsConn := ct.wsConn
	ct.mu.RUnlock()

	if wsConn == nil {
		return 0, fmt.Errorf("websocket connection is nil")
	}

	// 发送二进制消息
	err := wsConn.WriteMessage(websocket.BinaryMessage, b)
	if err != nil {
		return 0, err
	}

	ct.bytesSent += uint64(len(b))
	return len(b), nil
}

// Close 关闭连接
func (ct *CDNTransport) Close() error {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.closed {
		return nil
	}

	ct.closed = true
	close(ct.stopChan)

	if ct.pingTicker != nil {
		ct.pingTicker.Stop()
	}

	var err error
	if ct.wsConn != nil {
		// 发送关闭消息
		ct.wsConn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		err = ct.wsConn.Close()
	}

	if ct.conn != nil {
		if connErr := ct.conn.Close(); connErr != nil && err == nil {
			err = connErr
		}
	}

	return err
}

// LocalAddr 本地地址
func (ct *CDNTransport) LocalAddr() net.Addr {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	if ct.wsConn != nil {
		return ct.wsConn.LocalAddr()
	}
	if ct.conn != nil {
		return ct.conn.LocalAddr()
	}
	return nil
}

// RemoteAddr 远程地址
func (ct *CDNTransport) RemoteAddr() net.Addr {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	if ct.wsConn != nil {
		return ct.wsConn.RemoteAddr()
	}
	if ct.conn != nil {
		return ct.conn.RemoteAddr()
	}
	return nil
}

// SetDeadline 设置超时
func (ct *CDNTransport) SetDeadline(t time.Time) error {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	if ct.wsConn != nil {
		if err := ct.wsConn.SetReadDeadline(t); err != nil {
			return err
		}
		return ct.wsConn.SetWriteDeadline(t)
	}

	if ct.conn != nil {
		return ct.conn.SetDeadline(t)
	}

	return fmt.Errorf("no connection")
}

// SetReadDeadline 设置读超时
func (ct *CDNTransport) SetReadDeadline(t time.Time) error {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	if ct.wsConn != nil {
		return ct.wsConn.SetReadDeadline(t)
	}
	if ct.conn != nil {
		return ct.conn.SetReadDeadline(t)
	}
	return fmt.Errorf("no connection")
}

// SetWriteDeadline 设置写超时
func (ct *CDNTransport) SetWriteDeadline(t time.Time) error {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	if ct.wsConn != nil {
		return ct.wsConn.SetWriteDeadline(t)
	}
	if ct.conn != nil {
		return ct.conn.SetWriteDeadline(t)
	}
	return fmt.Errorf("no connection")
}

// pingRoutine WebSocket ping协程
func (ct *CDNTransport) pingRoutine() {
	ct.pingTicker = time.NewTicker(ct.config.PingInterval)
	defer ct.pingTicker.Stop()

	for {
		select {
		case <-ct.stopChan:
			return
		case <-ct.pingTicker.C:
			ct.sendPing()
		}
	}
}

// sendPing 发送ping
func (ct *CDNTransport) sendPing() {
	ct.mu.RLock()
	wsConn := ct.wsConn
	ct.mu.RUnlock()

	if wsConn == nil {
		return
	}

	ct.lastPing = time.Now()
	err := wsConn.WriteMessage(websocket.PingMessage, nil)
	if err != nil {
		// Ping失败，可能连接已断开
	}
}

// GetStats 获取统计信息
func (ct *CDNTransport) GetStats() CDNTransportStats {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	return CDNTransportStats{
		Mode:          ct.config.Mode,
		BytesSent:     ct.bytesSent,
		BytesReceived: ct.bytesReceived,
		LastPing:      ct.lastPing,
		LastPong:      ct.lastPong,
	}
}

// CDNTransportStats CDN传输统计
type CDNTransportStats struct {
	Mode          CDNMode
	BytesSent     uint64
	BytesReceived uint64
	LastPing      time.Time
	LastPong      time.Time
}

// CDNListener CDN监听器（服务器端）
type CDNListener struct {
	config   CDNConfig
	listener net.Listener
	upgrader websocket.Upgrader
	connChan chan net.Conn
	mu       sync.Mutex
	closed   bool
}

// NewCDNListener 创建CDN监听器
func NewCDNListener(config CDNConfig, addr string) (*CDNListener, error) {
	cl := &CDNListener{
		config:   config,
		connChan: make(chan net.Conn, 16),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // 接受所有来源
			},
			Subprotocols: []string{"binary"},
		},
	}

	// 创建HTTP服务器
	mux := http.NewServeMux()
	mux.HandleFunc(config.Path, cl.handleWebSocket)

	// 启动监听
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	cl.listener = listener

	// 启动HTTP服务器
	go func() {
		server := &http.Server{
			Handler:      mux,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
		}

		if config.Mode == CDNModeWebSocketTLS {
			server.ServeTLS(listener, "", "")
		} else {
			server.Serve(listener)
		}
	}()

	return cl, nil
}

// handleWebSocket 处理WebSocket连接
func (cl *CDNListener) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 升级到WebSocket
	wsConn, err := cl.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	// 创建包装连接
	conn := &websocketConn{
		wsConn: wsConn,
	}

	// 发送到连接通道
	select {
	case cl.connChan <- conn:
	default:
		// 通道满了，关闭连接
		conn.Close()
	}
}

// Accept 接受连接
func (cl *CDNListener) Accept() (net.Conn, error) {
	conn, ok := <-cl.connChan
	if !ok {
		return nil, fmt.Errorf("listener closed")
	}
	return conn, nil
}

// Close 关闭监听器
func (cl *CDNListener) Close() error {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	if cl.closed {
		return nil
	}

	cl.closed = true
	close(cl.connChan)

	if cl.listener != nil {
		return cl.listener.Close()
	}

	return nil
}

// Addr 监听地址
func (cl *CDNListener) Addr() net.Addr {
	if cl.listener != nil {
		return cl.listener.Addr()
	}
	return nil
}

// websocketConn WebSocket连接包装器
type websocketConn struct {
	wsConn *websocket.Conn
	reader io.Reader
	mu     sync.Mutex
}

// Read 读取数据
func (wc *websocketConn) Read(b []byte) (int, error) {
	if wc.reader != nil {
		n, err := wc.reader.Read(b)
		if err == io.EOF {
			wc.reader = nil
			return n, nil
		}
		if err != nil {
			return n, err
		}
		if n > 0 {
			return n, nil
		}
	}

	// 读取新消息
	messageType, message, err := wc.wsConn.ReadMessage()
	if err != nil {
		return 0, err
	}

	if messageType != websocket.BinaryMessage {
		return 0, fmt.Errorf("unexpected message type")
	}

	wc.reader = bufio.NewReader(strings.NewReader(string(message)))
	return wc.reader.Read(b)
}

// Write 写入数据
func (wc *websocketConn) Write(b []byte) (int, error) {
	wc.mu.Lock()
	defer wc.mu.Unlock()

	err := wc.wsConn.WriteMessage(websocket.BinaryMessage, b)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

// Close 关闭连接
func (wc *websocketConn) Close() error {
	return wc.wsConn.Close()
}

// LocalAddr 本地地址
func (wc *websocketConn) LocalAddr() net.Addr {
	return wc.wsConn.LocalAddr()
}

// RemoteAddr 远程地址
func (wc *websocketConn) RemoteAddr() net.Addr {
	return wc.wsConn.RemoteAddr()
}

// SetDeadline 设置超时
func (wc *websocketConn) SetDeadline(t time.Time) error {
	if err := wc.wsConn.SetReadDeadline(t); err != nil {
		return err
	}
	return wc.wsConn.SetWriteDeadline(t)
}

// SetReadDeadline 设置读超时
func (wc *websocketConn) SetReadDeadline(t time.Time) error {
	return wc.wsConn.SetReadDeadline(t)
}

// SetWriteDeadline 设置写超时
func (wc *websocketConn) SetWriteDeadline(t time.Time) error {
	return wc.wsConn.SetWriteDeadline(t)
}

// EncodeCDNFriendly 将数据编码为CDN友好格式
func EncodeCDNFriendly(data []byte) string {
	return base64.URLEncoding.EncodeToString(data)
}

// DecodeCDNFriendly 解码CDN友好格式
func DecodeCDNFriendly(encoded string) ([]byte, error) {
	return base64.URLEncoding.DecodeString(encoded)
}
