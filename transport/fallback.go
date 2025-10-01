package transport

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// FallbackMode 回落模式
type FallbackMode int

const (
	FallbackModeNone   FallbackMode = iota // 无回落
	FallbackModeHTTP                       // HTTP回落
	FallbackModeHTTPS                      // HTTPS回落
	FallbackModeTrojan                     // Trojan风格回落
)

// FallbackConfig 回落配置
type FallbackConfig struct {
	Mode          FallbackMode  // 回落模式
	FallbackAddr  string        // 回落地址（如回落到真实网站）
	FallbackHost  string        // 回落Host头
	TLSConfig     *tls.Config   // TLS配置
	DetectionTime time.Duration // 检测时间窗口
	BufferSize    int           // 缓冲区大小
	UseHTTPServer bool          // 是否启用HTTP服务器伪装
	StaticDir     string        // 静态文件目录
}

// DefaultFallbackConfig 默认回落配置
func DefaultFallbackConfig(fallbackAddr string) FallbackConfig {
	return FallbackConfig{
		Mode:          FallbackModeTrojan,
		FallbackAddr:  fallbackAddr,
		FallbackHost:  "www.bing.com",
		DetectionTime: 1 * time.Second,
		BufferSize:    4096,
		UseHTTPServer: true,
		StaticDir:     "./www",
	}
}

// FallbackListener 回落监听器
type FallbackListener struct {
	config       FallbackConfig
	listener     net.Listener
	httpServer   *http.Server
	validConns   chan net.Conn
	mu           sync.Mutex
	closed       bool
	statsLock    sync.RWMutex
	validCount   uint64
	fallbackCount uint64
}

// NewFallbackListener 创建回落监听器
func NewFallbackListener(config FallbackConfig, addr string) (*FallbackListener, error) {
	// 创建底层监听器
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	fl := &FallbackListener{
		config:     config,
		listener:   listener,
		validConns: make(chan net.Conn, 16),
	}

	// 如果启用了HTTP服务器，设置HTTP处理器
	if config.UseHTTPServer {
		fl.setupHTTPServer()
	}

	// 启动接受协程
	go fl.acceptRoutine()

	return fl, nil
}

// setupHTTPServer 设置HTTP服务器
func (fl *FallbackListener) setupHTTPServer() {
	mux := http.NewServeMux()

	// 处理根路径
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// 返回伪装的网页
		w.Header().Set("Server", "nginx/1.18.0")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>Welcome</title>
    <meta charset="utf-8">
</head>
<body>
    <h1>Welcome to nginx!</h1>
    <p>If you see this page, the nginx web server is successfully installed and working.</p>
</body>
</html>`)
	})

	// 处理其他路径
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	fl.httpServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
}

// acceptRoutine 接受连接协程
func (fl *FallbackListener) acceptRoutine() {
	for {
		conn, err := fl.listener.Accept()
		if err != nil {
			if !fl.closed {
				// 记录错误
			}
			return
		}

		// 处理连接
		go fl.handleConnection(conn)
	}
}

// handleConnection 处理连接
func (fl *FallbackListener) handleConnection(conn net.Conn) {
	// 创建缓冲连接用于检测
	bufferedConn := newBufferedConn(conn, fl.config.BufferSize)

	// 检测是否为有效的VPN流量
	isValid, err := fl.detectValidTraffic(bufferedConn)
	if err != nil {
		conn.Close()
		return
	}

	if isValid {
		// 有效连接，发送到validConns通道
		fl.statsLock.Lock()
		fl.validCount++
		fl.statsLock.Unlock()

		select {
		case fl.validConns <- bufferedConn:
		default:
			// 通道满了，关闭连接
			conn.Close()
		}
	} else {
		// 无效连接，执行回落
		fl.statsLock.Lock()
		fl.fallbackCount++
		fl.statsLock.Unlock()

		fl.handleFallback(bufferedConn)
	}
}

// detectValidTraffic 检测是否为有效流量
func (fl *FallbackListener) detectValidTraffic(conn *bufferedConn) (bool, error) {
	// 设置检测超时
	conn.SetReadDeadline(time.Now().Add(fl.config.DetectionTime))

	// 读取前几个字节用于检测
	header, err := conn.Peek(32)
	if err != nil && err != io.EOF {
		return false, err
	}

	// 重置超时
	conn.SetReadDeadline(time.Time{})

	if len(header) == 0 {
		return false, fmt.Errorf("no data")
	}

	// 检测协议特征
	return fl.isValidProtocol(header), nil
}

// isValidProtocol 检测是否为有效协议
func (fl *FallbackListener) isValidProtocol(data []byte) bool {
	if len(data) < 4 {
		return false
	}

	// 检查魔术字节（假设我们的协议有特定的魔术字节）
	// 这里使用一个简单的检测：第一个字节不是HTTP常见字符
	firstByte := data[0]

	// HTTP请求通常以 'G'(GET), 'P'(POST), 'H'(HEAD) 等开头
	httpMethods := []byte{'G', 'P', 'H', 'D', 'O', 'C', 'T'}
	for _, method := range httpMethods {
		if firstByte == method {
			return false
		}
	}

	// TLS握手以 0x16 开头
	if firstByte == 0x16 {
		// 检查是否为TLS ClientHello
		if len(data) >= 6 {
			// TLS记录头: type(1) + version(2) + length(2)
			if data[1] == 0x03 && (data[2] == 0x01 || data[2] == 0x03) {
				// 这是TLS流量，但我们需要进一步检查SNI
				// 如果有我们的特殊SNI，才认为是有效流量
				// 为简化，这里假设所有TLS都可能是有效的
				return fl.checkTLSSNI(data)
			}
		}
		return false
	}

	// 检查我们的协议魔术字节
	// 假设我们的协议前4字节为特定值
	if len(data) >= 8 {
		// 读取版本和类型字段（根据实际协议调整）
		version := data[0]
		msgType := data[1]

		// 协议版本检查（假设版本号在1-10之间）
		if version >= 1 && version <= 10 {
			// 消息类型检查（假设类型在特定范围）
			if msgType >= 1 && msgType <= 20 {
				return true
			}
		}
	}

	return false
}

// checkTLSSNI 检查TLS SNI
func (fl *FallbackListener) checkTLSSNI(data []byte) bool {
	// 简化的SNI检查
	// 实际实现需要解析TLS ClientHello
	// 这里只做简单的字符串搜索

	// 如果配置了特定的Host，检查是否包含
	if fl.config.FallbackHost != "" {
		return !bytes.Contains(data, []byte(fl.config.FallbackHost))
	}

	// 默认不允许普通TLS（认为是探测）
	return false
}

// handleFallback 处理回落
func (fl *FallbackListener) handleFallback(conn *bufferedConn) {
	defer conn.Close()

	switch fl.config.Mode {
	case FallbackModeHTTP, FallbackModeHTTPS:
		fl.handleHTTPFallback(conn)
	case FallbackModeTrojan:
		fl.handleTrojanFallback(conn)
	default:
		// 直接关闭连接
	}
}

// handleHTTPFallback 处理HTTP回落
func (fl *FallbackListener) handleHTTPFallback(conn *bufferedConn) {
	// 读取HTTP请求
	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}

	// 如果启用了HTTP服务器，使用内置处理器
	if fl.httpServer != nil {
		// 创建响应写入器
		rw := &responseWriter{
			conn:   conn,
			header: make(http.Header),
		}

		// 处理请求
		fl.httpServer.Handler.ServeHTTP(rw, req)
		return
	}

	// 否则，代理到回落地址
	if fl.config.FallbackAddr != "" {
		fl.proxyToFallback(conn, req)
	}
}

// handleTrojanFallback Trojan风格回落
func (fl *FallbackListener) handleTrojanFallback(conn *bufferedConn) {
	// 读取所有缓冲数据
	buffered := conn.Buffered()

	// 如果有回落地址，代理过去
	if fl.config.FallbackAddr != "" {
		// 连接到回落地址
		fallbackConn, err := net.Dial("tcp", fl.config.FallbackAddr)
		if err != nil {
			return
		}
		defer fallbackConn.Close()

		// 转发缓冲的数据
		if buffered > 0 {
			data := make([]byte, buffered)
			conn.Read(data)
			fallbackConn.Write(data)
		}

		// 双向复制
		go io.Copy(fallbackConn, conn)
		io.Copy(conn, fallbackConn)
	}
}

// proxyToFallback 代理到回落地址
func (fl *FallbackListener) proxyToFallback(conn net.Conn, req *http.Request) {
	// 连接到回落地址
	fallbackConn, err := net.Dial("tcp", fl.config.FallbackAddr)
	if err != nil {
		fl.sendErrorResponse(conn)
		return
	}
	defer fallbackConn.Close()

	// 转发请求
	req.Write(fallbackConn)

	// 双向复制
	go io.Copy(fallbackConn, conn)
	io.Copy(conn, fallbackConn)
}

// sendErrorResponse 发送错误响应
func (fl *FallbackListener) sendErrorResponse(conn net.Conn) {
	response := "HTTP/1.1 502 Bad Gateway\r\n" +
		"Server: nginx/1.18.0\r\n" +
		"Content-Type: text/html\r\n" +
		"Content-Length: 0\r\n" +
		"Connection: close\r\n" +
		"\r\n"
	conn.Write([]byte(response))
}

// Accept 接受有效连接
func (fl *FallbackListener) Accept() (net.Conn, error) {
	conn, ok := <-fl.validConns
	if !ok {
		return nil, fmt.Errorf("listener closed")
	}
	return conn, nil
}

// Close 关闭监听器
func (fl *FallbackListener) Close() error {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	if fl.closed {
		return nil
	}

	fl.closed = true
	close(fl.validConns)

	return fl.listener.Close()
}

// Addr 监听地址
func (fl *FallbackListener) Addr() net.Addr {
	return fl.listener.Addr()
}

// GetStats 获取统计信息
func (fl *FallbackListener) GetStats() FallbackStats {
	fl.statsLock.RLock()
	defer fl.statsLock.RUnlock()

	return FallbackStats{
		ValidConnections:    fl.validCount,
		FallbackConnections: fl.fallbackCount,
	}
}

// FallbackStats 回落统计
type FallbackStats struct {
	ValidConnections    uint64
	FallbackConnections uint64
}

// bufferedConn 带缓冲的连接
type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

// newBufferedConn 创建带缓冲的连接
func newBufferedConn(conn net.Conn, bufferSize int) *bufferedConn {
	return &bufferedConn{
		Conn:   conn,
		reader: bufio.NewReaderSize(conn, bufferSize),
	}
}

// Read 读取数据
func (bc *bufferedConn) Read(b []byte) (int, error) {
	return bc.reader.Read(b)
}

// Peek 查看数据但不消费
func (bc *bufferedConn) Peek(n int) ([]byte, error) {
	return bc.reader.Peek(n)
}

// Buffered 返回缓冲的字节数
func (bc *bufferedConn) Buffered() int {
	return bc.reader.Buffered()
}

// responseWriter HTTP响应写入器
type responseWriter struct {
	conn          net.Conn
	header        http.Header
	wroteHeader   bool
	statusCode    int
}

// Header 获取响应头
func (rw *responseWriter) Header() http.Header {
	return rw.header
}

// Write 写入响应体
func (rw *responseWriter) Write(data []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.conn.Write(data)
}

// WriteHeader 写入响应头
func (rw *responseWriter) WriteHeader(statusCode int) {
	if rw.wroteHeader {
		return
	}

	rw.wroteHeader = true
	rw.statusCode = statusCode

	// 写入状态行
	statusLine := fmt.Sprintf("HTTP/1.1 %d %s\r\n", statusCode, http.StatusText(statusCode))
	rw.conn.Write([]byte(statusLine))

	// 写入响应头
	for key, values := range rw.header {
		for _, value := range values {
			headerLine := fmt.Sprintf("%s: %s\r\n", key, value)
			rw.conn.Write([]byte(headerLine))
		}
	}

	// 空行
	rw.conn.Write([]byte("\r\n"))
}

// TrojanProtocol Trojan协议实现
type TrojanProtocol struct {
	password []byte // SHA224(password)
}

// NewTrojanProtocol 创建Trojan协议
func NewTrojanProtocol(password []byte) *TrojanProtocol {
	return &TrojanProtocol{
		password: password,
	}
}

// ValidateRequest 验证Trojan请求
func (tp *TrojanProtocol) ValidateRequest(data []byte) bool {
	// Trojan请求格式: password_hash(56 bytes hex) + CRLF + cmd(1) + ...
	if len(data) < 58 {
		return false
	}

	// 验证密码哈希
	passwordHash := data[:56]

	// 比较密码（实际应该是SHA224哈希比较）
	// 这里简化处理
	_ = passwordHash

	// 检查CRLF
	if data[56] != '\r' || data[57] != '\n' {
		return false
	}

	return true
}

// EncodeRequest 编码Trojan请求
func (tp *TrojanProtocol) EncodeRequest(cmd byte, addr string, port uint16, payload []byte) []byte {
	buf := &bytes.Buffer{}

	// 写入密码哈希（56字节十六进制）
	buf.Write(tp.password)
	buf.Write([]byte("\r\n"))

	// 写入命令
	buf.WriteByte(cmd)

	// 写入地址类型、地址、端口
	// 简化实现
	buf.WriteString(addr)
	binary.Write(buf, binary.BigEndian, port)

	// 写入payload
	buf.Write(payload)

	return buf.Bytes()
}
