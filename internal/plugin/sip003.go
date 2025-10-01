package plugin

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SIP003Plugin SIP003插件接口
// SIP003是Shadowsocks的插件协议标准
type SIP003Plugin struct {
	// 插件配置
	pluginPath string
	pluginOpts string
	remoteHost string
	remotePort int
	localHost  string
	localPort  int

	// 插件进程
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	running bool
	mu      sync.Mutex

	// 状态
	started   time.Time
	lastError error

	// 统计
	connections uint64
	bytesSent   uint64
	bytesRecv   uint64
}

// PluginConfig 插件配置
type PluginConfig struct {
	Plugin     string // 插件可执行文件路径
	PluginOpts string // 插件选项
	RemoteHost string // 远程主机
	RemotePort int    // 远程端口
	LocalHost  string // 本地监听地址
	LocalPort  int    // 本地监听端口
}

// NewSIP003Plugin 创建SIP003插件
func NewSIP003Plugin(config PluginConfig) *SIP003Plugin {
	return &SIP003Plugin{
		pluginPath: config.Plugin,
		pluginOpts: config.PluginOpts,
		remoteHost: config.RemoteHost,
		remotePort: config.RemotePort,
		localHost:  config.LocalHost,
		localPort:  config.LocalPort,
	}
}

// Start 启动插件
func (p *SIP003Plugin) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("plugin already running")
	}

	// 创建命令
	p.cmd = exec.Command(p.pluginPath)

	// 设置环境变量（SIP003标准）
	env := os.Environ()
	env = append(env, fmt.Sprintf("SS_REMOTE_HOST=%s", p.remoteHost))
	env = append(env, fmt.Sprintf("SS_REMOTE_PORT=%d", p.remotePort))
	env = append(env, fmt.Sprintf("SS_LOCAL_HOST=%s", p.localHost))
	env = append(env, fmt.Sprintf("SS_LOCAL_PORT=%d", p.localPort))

	if p.pluginOpts != "" {
		env = append(env, fmt.Sprintf("SS_PLUGIN_OPTIONS=%s", p.pluginOpts))
	}

	p.cmd.Env = env

	// 设置标准输入输出
	var err error
	p.stdin, err = p.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	p.stdout, err = p.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	p.stderr, err = p.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// 启动进程
	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start plugin: %w", err)
	}

	p.running = true
	p.started = time.Now()

	// 启动输出监控
	go p.monitorOutput()

	return nil
}

// Stop 停止插件
func (p *SIP003Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return nil
	}

	// 关闭标准输入（通知插件退出）
	if p.stdin != nil {
		p.stdin.Close()
	}

	// 等待进程退出（最多5秒）
	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()

	select {
	case <-time.After(5 * time.Second):
		// 超时，强制杀死进程
		if p.cmd.Process != nil {
			p.cmd.Process.Kill()
		}
		return fmt.Errorf("plugin stop timeout")
	case err := <-done:
		p.running = false
		return err
	}
}

// monitorOutput 监控插件输出
func (p *SIP003Plugin) monitorOutput() {
	// 监控标准输出
	go func() {
		scanner := bufio.NewScanner(p.stdout)
		for scanner.Scan() {
			line := scanner.Text()
			// 可以在这里记录日志
			_ = line
		}
	}()

	// 监控标准错误
	go func() {
		scanner := bufio.NewScanner(p.stderr)
		for scanner.Scan() {
			line := scanner.Text()
			// 记录错误
			p.mu.Lock()
			p.lastError = fmt.Errorf("plugin error: %s", line)
			p.mu.Unlock()
		}
	}()
}

// IsRunning 检查插件是否运行
func (p *SIP003Plugin) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// GetStats 获取统计信息
func (p *SIP003Plugin) GetStats() PluginStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	uptime := time.Duration(0)
	if p.running {
		uptime = time.Since(p.started)
	}

	return PluginStats{
		Running:     p.running,
		Uptime:      uptime,
		Connections: p.connections,
		BytesSent:   p.bytesSent,
		BytesRecv:   p.bytesRecv,
		LastError:   p.lastError,
	}
}

// PluginStats 插件统计
type PluginStats struct {
	Running     bool
	Uptime      time.Duration
	Connections uint64
	BytesSent   uint64
	BytesRecv   uint64
	LastError   error
}

// PluginManager 插件管理器
type PluginManager struct {
	mu      sync.RWMutex
	plugins map[string]*SIP003Plugin
}

// NewPluginManager 创建插件管理器
func NewPluginManager() *PluginManager {
	return &PluginManager{
		plugins: make(map[string]*SIP003Plugin),
	}
}

// Register 注册插件
func (pm *PluginManager) Register(name string, plugin *SIP003Plugin) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.plugins[name]; exists {
		return fmt.Errorf("plugin %s already registered", name)
	}

	pm.plugins[name] = plugin
	return nil
}

// Unregister 注销插件
func (pm *PluginManager) Unregister(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	plugin, exists := pm.plugins[name]
	if !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	// 停止插件
	if plugin.IsRunning() {
		plugin.Stop()
	}

	delete(pm.plugins, name)
	return nil
}

// Get 获取插件
func (pm *PluginManager) Get(name string) (*SIP003Plugin, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	plugin, exists := pm.plugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin %s not found", name)
	}

	return plugin, nil
}

// List 列出所有插件
func (pm *PluginManager) List() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	names := make([]string, 0, len(pm.plugins))
	for name := range pm.plugins {
		names = append(names, name)
	}

	return names
}

// StartAll 启动所有插件
func (pm *PluginManager) StartAll() error {
	pm.mu.RLock()
	plugins := make([]*SIP003Plugin, 0, len(pm.plugins))
	for _, plugin := range pm.plugins {
		plugins = append(plugins, plugin)
	}
	pm.mu.RUnlock()

	var lastError error
	for _, plugin := range plugins {
		if err := plugin.Start(); err != nil {
			lastError = err
		}
	}

	return lastError
}

// StopAll 停止所有插件
func (pm *PluginManager) StopAll() error {
	pm.mu.RLock()
	plugins := make([]*SIP003Plugin, 0, len(pm.plugins))
	for _, plugin := range pm.plugins {
		plugins = append(plugins, plugin)
	}
	pm.mu.RUnlock()

	var lastError error
	for _, plugin := range plugins {
		if err := plugin.Stop(); err != nil {
			lastError = err
		}
	}

	return lastError
}

// PluginConn 插件连接包装器
type PluginConn struct {
	net.Conn
	plugin *SIP003Plugin
}

// NewPluginConn 创建插件连接
func NewPluginConn(conn net.Conn, plugin *SIP003Plugin) *PluginConn {
	return &PluginConn{
		Conn:   conn,
		plugin: plugin,
	}
}

// Read 读取数据
func (pc *PluginConn) Read(b []byte) (n int, err error) {
	n, err = pc.Conn.Read(b)
	if err == nil && pc.plugin != nil {
		pc.plugin.mu.Lock()
		pc.plugin.bytesRecv += uint64(n)
		pc.plugin.mu.Unlock()
	}
	return n, err
}

// Write 写入数据
func (pc *PluginConn) Write(b []byte) (n int, err error) {
	n, err = pc.Conn.Write(b)
	if err == nil && pc.plugin != nil {
		pc.plugin.mu.Lock()
		pc.plugin.bytesSent += uint64(n)
		pc.plugin.mu.Unlock()
	}
	return n, err
}

// Close 关闭连接
func (pc *PluginConn) Close() error {
	if pc.plugin != nil {
		pc.plugin.mu.Lock()
		if pc.plugin.connections > 0 {
			pc.plugin.connections--
		}
		pc.plugin.mu.Unlock()
	}
	return pc.Conn.Close()
}

// PluginDialer 插件拨号器
type PluginDialer struct {
	plugin *SIP003Plugin
}

// NewPluginDialer 创建插件拨号器
func NewPluginDialer(plugin *SIP003Plugin) *PluginDialer {
	return &PluginDialer{
		plugin: plugin,
	}
}

// Dial 拨号
func (pd *PluginDialer) Dial(network, address string) (net.Conn, error) {
	// 连接到插件的本地端口
	localAddr := fmt.Sprintf("%s:%d", pd.plugin.localHost, pd.plugin.localPort)
	conn, err := net.Dial("tcp", localAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to plugin: %w", err)
	}

	// 增加连接计数
	pd.plugin.mu.Lock()
	pd.plugin.connections++
	pd.plugin.mu.Unlock()

	// 包装连接
	return NewPluginConn(conn, pd.plugin), nil
}

// PluginListener 插件监听器
type PluginListener struct {
	listener net.Listener
	plugin   *SIP003Plugin
	closed   bool
	mu       sync.Mutex
}

// NewPluginListener 创建插件监听器
func NewPluginListener(plugin *SIP003Plugin) (*PluginListener, error) {
	// 监听本地端口
	addr := fmt.Sprintf("%s:%d", plugin.localHost, plugin.localPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	return &PluginListener{
		listener: listener,
		plugin:   plugin,
	}, nil
}

// Accept 接受连接
func (pl *PluginListener) Accept() (net.Conn, error) {
	conn, err := pl.listener.Accept()
	if err != nil {
		return nil, err
	}

	// 增加连接计数
	pl.plugin.mu.Lock()
	pl.plugin.connections++
	pl.plugin.mu.Unlock()

	// 包装连接
	return NewPluginConn(conn, pl.plugin), nil
}

// Close 关闭监听器
func (pl *PluginListener) Close() error {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	if pl.closed {
		return nil
	}

	pl.closed = true
	return pl.listener.Close()
}

// Addr 获取监听地址
func (pl *PluginListener) Addr() net.Addr {
	return pl.listener.Addr()
}

// ParsePluginOptions 解析插件选项
func ParsePluginOptions(opts string) map[string]string {
	result := make(map[string]string)

	if opts == "" {
		return result
	}

	// 分号分隔的键值对
	pairs := strings.Split(opts, ";")
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			result[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		} else if len(kv) == 1 {
			result[strings.TrimSpace(kv[0])] = "true"
		}
	}

	return result
}

// FormatPluginOptions 格式化插件选项
func FormatPluginOptions(opts map[string]string) string {
	if len(opts) == 0 {
		return ""
	}

	pairs := make([]string, 0, len(opts))
	for key, value := range opts {
		if value == "true" {
			pairs = append(pairs, key)
		} else {
			pairs = append(pairs, fmt.Sprintf("%s=%s", key, value))
		}
	}

	return strings.Join(pairs, ";")
}

// FindFreePort 查找可用端口
func FindFreePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	return addr.Port, nil
}

// ParseAddr 解析地址
func ParseAddr(addr string) (host string, port int, err error) {
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid address format")
	}

	host = parts[0]
	port, err = strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("invalid port: %w", err)
	}

	return host, port, nil
}
