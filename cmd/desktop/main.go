package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"stp/config"
	"stp/device"
	"stp/internal/logging"
	"stp/internal/management"
	"stp/internal/ratelimit"
	"stp/internal/state"
	"stp/transport"
)

type DesktopApp struct {
	cfg            *config.Config
	cfgPath        string
	device         *device.Device
	mgmt           *management.Server
	reloadTracker  *state.ReloadTracker
	logger         *logging.Logger
	running        bool
	runningMu      sync.RWMutex
	serverCancel   context.CancelFunc
	limiter        *ratelimit.ConnectionLimiter
}

func main() {
	port := 9999

	desktopApp := &DesktopApp{
		cfgPath:       "config.json",
		reloadTracker: state.NewReloadTracker(10),
	}

	// Setup HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", desktopApp.handleIndex)
	mux.HandleFunc("/api/config", desktopApp.handleConfig)
	mux.HandleFunc("/api/start", desktopApp.handleStart)
	mux.HandleFunc("/api/stop", desktopApp.handleStop)
	mux.HandleFunc("/api/status", desktopApp.handleStatus)
	mux.HandleFunc("/api/state", desktopApp.handleState)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	// Start server
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(500 * time.Millisecond)

	// Open browser in app mode (kiosk-like experience)
	url := fmt.Sprintf("http://localhost:%d", port)
	log.Printf("🛡️  VeilDeploy 桌面版已启动")
	log.Printf("📱 正在打开应用界面...")

	openAppWindow(url)

	// Wait for interrupt
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("\n正在关闭桌面应用...")
	desktopApp.stopService()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("服务器关闭错误: %v", err)
	}

	log.Println("桌面应用已停止")
}

func (d *DesktopApp) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(modernUITemplate))
}

func (d *DesktopApp) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "GET" {
		data, err := os.ReadFile(d.cfgPath)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		w.Write(data)
		return
	}

	if r.Method == "POST" {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusBadRequest)
			return
		}

		var testCfg config.Config
		if err := json.Unmarshal(body, &testCfg); err != nil {
			http.Error(w, fmt.Sprintf(`{"error": "Invalid JSON: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}

		if err := os.WriteFile(d.cfgPath, body, 0644); err != nil {
			http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}

		d.cfg = &testCfg
		w.Write([]byte(`{"success": true}`))
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (d *DesktopApp) handleStart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	d.runningMu.Lock()
	if d.running {
		d.runningMu.Unlock()
		w.Write([]byte(`{"error": "Service already running"}`))
		return
	}
	d.running = true
	d.runningMu.Unlock()

	cfg, err := config.Load(d.cfgPath)
	if err != nil {
		d.runningMu.Lock()
		d.running = false
		d.runningMu.Unlock()
		http.Error(w, fmt.Sprintf(`{"error": "Config load failed: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	d.cfg = cfg

	ctx, cancel := context.WithCancel(context.Background())
	d.serverCancel = cancel

	go func() {
		level := logging.ParseLevel(cfg.NormalisedLevel())
		baseLogger := logging.New(level, os.Stdout)
		d.logger = baseLogger.With(map[string]interface{}{"component": "stp"})

		mode := strings.ToLower(cfg.Mode)
		log.Printf("⚡ 启动服务 (模式: %s)", mode)

		if mode == "client" {
			if err := d.runClient(ctx, cfg, baseLogger); err != nil {
				d.logger.Error("client error", map[string]interface{}{"error": err.Error()})
			}
		} else if mode == "server" {
			if err := d.runServer(ctx, cfg, baseLogger); err != nil {
				d.logger.Error("server error", map[string]interface{}{"error": err.Error()})
			}
		}

		d.runningMu.Lock()
		d.running = false
		d.runningMu.Unlock()
	}()

	w.Write([]byte(`{"success": true, "mode": "` + cfg.Mode + `"}`))
}

func (d *DesktopApp) handleStop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	d.stopService()
	w.Write([]byte(`{"success": true}`))
}

func (d *DesktopApp) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	d.runningMu.RLock()
	running := d.running
	d.runningMu.RUnlock()

	mode := "none"
	if d.cfg != nil {
		mode = d.cfg.Mode
	}

	resp := map[string]interface{}{
		"running": running,
		"mode":    mode,
	}

	json.NewEncoder(w).Encode(resp)
}

func (d *DesktopApp) handleState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if d.mgmt == nil {
		http.Error(w, `{"error": "Service not running"}`, http.StatusServiceUnavailable)
		return
	}

	resp, err := http.Get("http://" + d.cfg.Management.Bind + "/state")
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	io.Copy(w, resp.Body)
}

func (d *DesktopApp) stopService() {
	d.runningMu.Lock()
	if !d.running {
		d.runningMu.Unlock()
		return
	}
	d.running = false
	d.runningMu.Unlock()

	log.Println("⏹️  停止服务...")

	if d.serverCancel != nil {
		d.serverCancel()
	}

	if d.mgmt != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		d.mgmt.Close(ctx)
	}

	if d.device != nil {
		d.device.Close()
	}
}

func (d *DesktopApp) runClient(ctx context.Context, cfg *config.Config, baseLogger *logging.Logger) error {
	logger := baseLogger.With(map[string]interface{}{"role": "client"})
	dev, err := device.NewDevice(device.RoleClient, cfg, logger)
	if err != nil {
		return err
	}
	d.device = dev
	defer dev.Close()

	network, address := parseEndpoint(cfg.Endpoint)
	conn, err := transport.Dial(network, address)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := dev.Handshake(conn, cfg); err != nil {
		return err
	}

	mgmt, err := management.New(cfg.Management.Bind, func() interface{} {
		snapshot := dev.Snapshot()
		return map[string]interface{}{
			"device":  snapshot,
			"reloads": d.reloadTracker.GetHistory(),
		}
	}, logger, management.WithMetrics(dev.Metrics), management.WithACL(cfg.ManagementPrefixes()))
	if err != nil {
		return err
	}
	d.mgmt = mgmt
	mgmt.Start()

	done := make(chan struct{})
	go func() {
		dev.TunnelLoop(conn)
		close(done)
	}()

	select {
	case <-ctx.Done():
		return nil
	case <-done:
		return nil
	}
}

func (d *DesktopApp) runServer(ctx context.Context, cfg *config.Config, baseLogger *logging.Logger) error {
	logger := baseLogger.With(map[string]interface{}{"role": "server"})

	network, address := parseEndpoint(cfg.Listen)
	listener, err := transport.Listen(network, address)
	if err != nil {
		return err
	}
	defer listener.Close()

	d.limiter = ratelimit.NewConnectionLimiter(
		cfg.EffectiveMaxConnections(),
		cfg.EffectiveConnectionRate(),
		cfg.EffectiveConnectionBurst(),
	)

	mgmt, err := management.New(cfg.Management.Bind, func() interface{} {
		return map[string]interface{}{
			"server":  map[string]interface{}{"sessions": 0},
			"reloads": d.reloadTracker.GetHistory(),
		}
	}, logger, management.WithACL(cfg.ManagementPrefixes()))
	if err != nil {
		return err
	}
	d.mgmt = mgmt
	mgmt.Start()

	logger.Info("server listening", map[string]interface{}{
		"addr": address,
	})

	<-ctx.Done()
	return nil
}

func parseEndpoint(endpoint string) (network, address string) {
	network = "udp"
	address = endpoint
	if strings.Contains(endpoint, "://") {
		parts := strings.SplitN(endpoint, "://", 2)
		network = parts[0]
		address = parts[1]
	}
	return network, address
}

func openAppWindow(url string) {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		// Open Chrome in app mode for native-like experience
		chromePaths := []string{
			os.Getenv("LOCALAPPDATA") + "\\Google\\Chrome\\Application\\chrome.exe",
			os.Getenv("PROGRAMFILES") + "\\Google\\Chrome\\Application\\chrome.exe",
			os.Getenv("PROGRAMFILES(X86)") + "\\Google\\Chrome\\Application\\chrome.exe",
		}

		chromeFound := false
		for _, path := range chromePaths {
			if _, err := os.Stat(path); err == nil {
				cmd = exec.Command(path,
					"--app="+url,
					"--window-size=1400,900",
					"--window-position=200,100",
				)
				chromeFound = true
				break
			}
		}

		if !chromeFound {
			// Fallback to Edge
			edgePath := os.Getenv("PROGRAMFILES(X86)") + "\\Microsoft\\Edge\\Application\\msedge.exe"
			if _, err := os.Stat(edgePath); err == nil {
				cmd = exec.Command(edgePath, "--app="+url)
			} else {
				// Final fallback
				cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
			}
		}
	case "darwin":
		cmd = exec.Command("open", "-a", "Google Chrome", "--args", "--app="+url)
	default:
		cmd = exec.Command("google-chrome", "--app="+url)
	}

	if err := cmd.Start(); err != nil {
		log.Printf("⚠️  无法以应用模式打开，尝试默认浏览器...")
		fallbackOpen(url)
	}
}

func fallbackOpen(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

const modernUITemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>VeilDeploy</title>
    <link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='.9em' font-size='90'>🛡️</text></svg>">
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700;800&display=swap" rel="stylesheet">
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }

        :root {
            --bg-primary: #0f1419;
            --bg-secondary: #1a1f29;
            --bg-tertiary: #242b38;
            --text-primary: #e6edf3;
            --text-secondary: #8b949e;
            --accent-primary: #667eea;
            --accent-secondary: #764ba2;
            --success: #3fb950;
            --danger: #f85149;
            --warning: #d29922;
            --border-color: #30363d;
        }

        body {
            font-family: 'Inter', -apple-system, BlinkMacSystemFont, sans-serif;
            background: var(--bg-primary);
            color: var(--text-primary);
            height: 100vh;
            overflow: hidden;
            -webkit-font-smoothing: antialiased;
        }

        .app-container {
            display: flex;
            height: 100vh;
        }

        /* Sidebar */
        .sidebar {
            width: 280px;
            background: var(--bg-secondary);
            border-right: 1px solid var(--border-color);
            display: flex;
            flex-direction: column;
            padding: 24px 0;
        }

        .logo-section {
            padding: 0 24px 32px;
            border-bottom: 1px solid var(--border-color);
        }

        .logo {
            display: flex;
            align-items: center;
            gap: 12px;
        }

        .logo-icon {
            font-size: 32px;
        }

        .logo-text h1 {
            font-size: 20px;
            font-weight: 700;
            background: linear-gradient(135deg, var(--accent-primary), var(--accent-secondary));
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }

        .logo-text p {
            font-size: 12px;
            color: var(--text-secondary);
            margin-top: 2px;
        }

        .nav-menu {
            flex: 1;
            padding: 24px 12px;
        }

        .nav-item {
            display: flex;
            align-items: center;
            gap: 12px;
            padding: 12px 16px;
            margin-bottom: 4px;
            border-radius: 8px;
            color: var(--text-secondary);
            cursor: pointer;
            transition: all 0.2s;
            font-size: 14px;
            font-weight: 500;
        }

        .nav-item:hover {
            background: var(--bg-tertiary);
            color: var(--text-primary);
        }

        .nav-item.active {
            background: linear-gradient(135deg, var(--accent-primary), var(--accent-secondary));
            color: white;
        }

        .nav-icon {
            font-size: 18px;
            width: 20px;
        }

        .status-indicator {
            margin-top: auto;
            padding: 16px 24px;
            border-top: 1px solid var(--border-color);
        }

        .status-badge {
            display: flex;
            align-items: center;
            gap: 8px;
            padding: 10px 14px;
            border-radius: 8px;
            font-size: 13px;
            font-weight: 600;
        }

        .status-running {
            background: rgba(63, 185, 80, 0.1);
            color: var(--success);
        }

        .status-stopped {
            background: rgba(248, 81, 73, 0.1);
            color: var(--danger);
        }

        .status-dot {
            width: 8px;
            height: 8px;
            border-radius: 50%;
            animation: pulse 2s infinite;
        }

        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.5; }
        }

        /* Main Content */
        .main-content {
            flex: 1;
            display: flex;
            flex-direction: column;
            overflow: hidden;
        }

        .titlebar {
            -webkit-app-region: drag;
            height: 50px;
            background: var(--bg-secondary);
            border-bottom: 1px solid var(--border-color);
            display: flex;
            align-items: center;
            padding: 0 24px;
            justify-content: space-between;
        }

        .titlebar-title {
            font-size: 13px;
            color: var(--text-secondary);
        }

        .content-area {
            flex: 1;
            overflow-y: auto;
            padding: 32px;
        }

        /* Cards */
        .card {
            background: var(--bg-secondary);
            border: 1px solid var(--border-color);
            border-radius: 12px;
            padding: 24px;
            margin-bottom: 24px;
        }

        .card-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            margin-bottom: 20px;
        }

        .card-title {
            font-size: 18px;
            font-weight: 600;
            display: flex;
            align-items: center;
            gap: 10px;
        }

        /* Buttons */
        .btn-group {
            display: flex;
            gap: 12px;
            flex-wrap: wrap;
        }

        .controls {
            display: flex;
            gap: 12px;
            flex-wrap: wrap;
        }

        .controls .btn {
            min-width: 140px;
        }

        .btn {
            padding: 10px 20px;
            border: none;
            border-radius: 8px;
            font-size: 14px;
            font-weight: 600;
            cursor: pointer;
            transition: all 0.2s;
            display: inline-flex;
            align-items: center;
            gap: 8px;
        }

        .btn-primary {
            background: linear-gradient(135deg, var(--accent-primary), var(--accent-secondary));
            color: white;
        }

        .btn-primary:hover {
            transform: translateY(-1px);
            box-shadow: 0 4px 12px rgba(102, 126, 234, 0.4);
        }

        .btn-danger {
            background: var(--danger);
            color: white;
        }

        .btn-secondary {
            background: var(--bg-tertiary);
            color: var(--text-primary);
            border: 1px solid var(--border-color);
        }

        .btn:disabled {
            opacity: 0.5;
            cursor: not-allowed;
        }

        /* Stats Grid */
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 16px;
            margin-top: 20px;
        }

        .stat-card {
            background: var(--bg-tertiary);
            border: 1px solid var(--border-color);
            border-radius: 10px;
            padding: 20px;
            transition: all 0.2s;
            position: relative;
        }

        .stat-card:hover {
            transform: translateY(-2px);
            border-color: var(--accent-primary);
        }

        .stat-card.status-active {
            background: rgba(63, 185, 80, 0.05);
            border-color: var(--success);
        }

        .stat-card.status-inactive {
            background: rgba(139, 148, 158, 0.05);
            border-color: var(--border-color);
        }

        .stat-icon {
            position: absolute;
            top: 16px;
            right: 16px;
            font-size: 24px;
            opacity: 0.3;
        }

        .stat-value {
            font-size: 32px;
            font-weight: 700;
            background: linear-gradient(135deg, var(--accent-primary), var(--accent-secondary));
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            margin-bottom: 8px;
        }

        .stat-label {
            font-size: 13px;
            color: var(--text-secondary);
            font-weight: 500;
            margin-bottom: 4px;
        }

        .stat-update-time {
            font-size: 11px;
            color: var(--text-secondary);
            opacity: 0.6;
        }

        /* Config Editor */
        .config-editor-container {
            position: relative;
        }

        .config-editor {
            background: var(--bg-primary);
            border: 1px solid var(--border-color);
            border-radius: 8px;
            padding: 16px;
            font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
            font-size: 13px;
            line-height: 1.6;
            color: var(--text-primary);
            resize: vertical;
            min-height: 400px;
            width: 100%;
        }

        .config-editor:focus {
            outline: 2px solid var(--accent-primary);
            outline-offset: 2px;
        }

        .config-validation {
            margin-top: 8px;
            padding: 8px 12px;
            border-radius: 6px;
            font-size: 12px;
            display: none;
        }

        .config-validation.valid {
            display: block;
            background: rgba(63, 185, 80, 0.1);
            color: var(--success);
        }

        .config-validation.invalid {
            display: block;
            background: rgba(248, 81, 73, 0.1);
            color: var(--danger);
        }

        .log-panel {
            background: var(--bg-primary);
            border: 1px solid var(--border-color);
            border-radius: 8px;
            padding: 16px;
            font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
            font-size: 12px;
            line-height: 1.6;
            color: var(--text-primary);
            height: 300px;
            overflow-y: auto;
        }

        .log-entry {
            padding: 4px 0;
            border-bottom: 1px solid rgba(48, 54, 61, 0.3);
        }

        .log-time {
            color: var(--text-secondary);
            margin-right: 8px;
        }

        .log-level-info {
            color: #58a6ff;
        }

        .log-level-warn {
            color: var(--warning);
        }

        .log-level-error {
            color: var(--danger);
        }

        @media (min-width: 1200px) {
            .two-column-layout {
                display: grid;
                grid-template-columns: 1fr 1fr;
                gap: 24px;
            }
        }

        .loading-spinner {
            display: inline-block;
            width: 14px;
            height: 14px;
            border: 2px solid rgba(255, 255, 255, 0.3);
            border-radius: 50%;
            border-top-color: white;
            animation: spin 0.8s linear infinite;
        }

        @keyframes spin {
            to { transform: rotate(360deg); }
        }

        .btn.loading {
            position: relative;
            pointer-events: none;
        }

        .btn.loading::before {
            content: '';
            position: absolute;
            left: 50%;
            top: 50%;
            transform: translate(-50%, -50%);
            width: 16px;
            height: 16px;
            border: 2px solid rgba(255, 255, 255, 0.3);
            border-radius: 50%;
            border-top-color: white;
            animation: spin 0.8s linear infinite;
        }

        .btn.loading span {
            opacity: 0.3;
        }

        /* Alert */
        .alert {
            padding: 12px 16px;
            border-radius: 8px;
            margin-bottom: 16px;
            font-size: 14px;
            display: flex;
            align-items: center;
            gap: 10px;
            animation: slideIn 0.3s;
        }

        @keyframes slideIn {
            from {
                opacity: 0;
                transform: translateY(-10px);
            }
            to {
                opacity: 1;
                transform: translateY(0);
            }
        }

        .alert-success {
            background: rgba(63, 185, 80, 0.1);
            color: var(--success);
            border: 1px solid var(--success);
        }

        .alert-error {
            background: rgba(248, 81, 73, 0.1);
            color: var(--danger);
            border: 1px solid var(--danger);
        }

        /* Scrollbar */
        ::-webkit-scrollbar {
            width: 12px;
        }

        ::-webkit-scrollbar-track {
            background: var(--bg-primary);
        }

        ::-webkit-scrollbar-thumb {
            background: var(--bg-tertiary);
            border-radius: 6px;
        }

        ::-webkit-scrollbar-thumb:hover {
            background: var(--border-color);
        }

        .page {
            display: none;
        }

        .page.active {
            display: block;
        }

        /* Form Styles */
        .form-section {
            background: var(--bg-tertiary);
            border: 1px solid var(--border-color);
            border-radius: 10px;
            padding: 24px;
            margin-bottom: 20px;
        }

        .form-section-title {
            font-size: 16px;
            font-weight: 600;
            margin-bottom: 20px;
            padding-bottom: 12px;
            border-bottom: 2px solid var(--border-color);
        }

        .form-group {
            margin-bottom: 20px;
        }

        .form-label {
            display: block;
            font-size: 14px;
            font-weight: 500;
            margin-bottom: 8px;
            color: var(--text-primary);
        }

        .form-hint {
            display: block;
            font-size: 12px;
            font-weight: 400;
            color: var(--text-secondary);
            margin-top: 4px;
        }

        .form-input, .form-select {
            width: 100%;
            padding: 12px 16px;
            background: var(--bg-primary);
            border: 1px solid var(--border-color);
            border-radius: 8px;
            color: var(--text-primary);
            font-size: 14px;
            transition: all 0.2s;
        }

        .form-input:focus, .form-select:focus {
            outline: none;
            border-color: var(--accent-primary);
            box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.1);
        }

        .form-select {
            cursor: pointer;
        }

        .btn-icon {
            background: none;
            border: none;
            cursor: pointer;
            font-size: 16px;
            padding: 4px;
        }

        .peer-item {
            background: var(--bg-primary);
            border: 1px solid var(--border-color);
            border-radius: 8px;
            padding: 16px;
            margin-bottom: 12px;
        }

        .peer-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 12px;
        }

        .alert-warning {
            background: rgba(210, 153, 34, 0.1);
            color: var(--warning);
            border: 1px solid var(--warning);
        }

        .form-input::placeholder {
            color: var(--text-secondary);
        }
    </style>
</head>
<body>
    <div class="app-container">
        <div class="sidebar">
            <div class="logo-section">
                <div class="logo">
                    <span class="logo-icon">🛡️</span>
                    <div class="logo-text">
                        <h1>VeilDeploy</h1>
                        <p>Desktop Edition</p>
                    </div>
                </div>
            </div>

            <div class="nav-menu">
                <div class="nav-item active" onclick="switchPage('dashboard')">
                    <span class="nav-icon">📊</span>
                    <span>仪表板</span>
                </div>
                <div class="nav-item" onclick="switchPage('config')">
                    <span class="nav-icon">⚙️</span>
                    <span>配置管理</span>
                </div>
                <div class="nav-item" onclick="switchPage('status')">
                    <span class="nav-icon">📈</span>
                    <span>实时监控</span>
                </div>
            </div>

            <div class="status-indicator">
                <div class="status-badge status-stopped" id="sidebarStatus">
                    <span class="status-dot" style="background: var(--danger)"></span>
                    <span>已停止</span>
                </div>
            </div>
        </div>

        <div class="main-content">
            <div class="titlebar">
                <div class="titlebar-title">VeilDeploy Desktop v1.0.0</div>
            </div>

            <div class="content-area">
                <!-- Dashboard Page -->
                <div class="page active" id="dashboard">
                    <div class="card">
                        <div class="card-header">
                            <h2 class="card-title">
                                <span>⚡</span>
                                服务控制
                            </h2>
                        </div>
                        <div class="controls">
                            <button class="btn btn-primary" id="startBtn" onclick="startService()">
                                <span>🚀</span>
                                <span>启动服务</span>
                            </button>
                            <button class="btn btn-danger" id="stopBtn" onclick="stopService()" disabled>
                                <span>⏹️</span>
                                <span>停止服务</span>
                            </button>
                            <button class="btn btn-secondary" onclick="refreshState()">
                                <span>🔄</span>
                                <span>刷新状态</span>
                            </button>
                        </div>
                        <div id="alertBox" style="margin-top: 16px;"></div>
                    </div>

                    <div class="two-column-layout">
                        <div class="card">
                            <div class="card-header">
                                <h2 class="card-title">
                                    <span>📊</span>
                                    系统概览
                                </h2>
                            </div>
                            <div class="stats-grid">
                                <div class="stat-card" id="cardSessions">
                                    <span class="stat-icon">👥</span>
                                    <div class="stat-value" id="statSessions">0</div>
                                    <div class="stat-label">活跃会话</div>
                                    <div class="stat-update-time" id="updateTime1">从未更新</div>
                                </div>
                                <div class="stat-card" id="cardConnections">
                                    <span class="stat-icon">🔗</span>
                                    <div class="stat-value" id="statConnections">0</div>
                                    <div class="stat-label">当前连接</div>
                                    <div class="stat-update-time" id="updateTime2">从未更新</div>
                                </div>
                                <div class="stat-card" id="cardMessages">
                                    <span class="stat-icon">📨</span>
                                    <div class="stat-value" id="statMessages">0</div>
                                    <div class="stat-label">消息总数</div>
                                    <div class="stat-update-time" id="updateTime3">从未更新</div>
                                </div>
                                <div class="stat-card" id="cardMode">
                                    <span class="stat-icon">⚙️</span>
                                    <div class="stat-value" id="statMode">-</div>
                                    <div class="stat-label">运行模式</div>
                                    <div class="stat-update-time" id="updateTime4">从未更新</div>
                                </div>
                            </div>
                        </div>

                        <div class="card">
                            <div class="card-header">
                                <h2 class="card-title">
                                    <span>📋</span>
                                    实时日志
                                </h2>
                                <button class="btn btn-secondary" onclick="clearLogs()" style="padding: 6px 12px; font-size: 12px;">
                                    <span>🗑️</span>
                                    清空
                                </button>
                            </div>
                            <div class="log-panel" id="logPanel">
                                <div class="log-entry">
                                    <span class="log-time">--:--:--</span>
                                    <span class="log-level-info">等待服务启动...</span>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>

                <!-- Config Page -->
                <div class="page" id="config">
                    <div class="card">
                        <div class="card-header">
                            <h2 class="card-title">
                                <span>⚙️</span>
                                配置管理
                            </h2>
                            <div style="display: flex; gap: 12px;">
                                <button class="btn btn-secondary" onclick="toggleAdvancedMode()">
                                    <span>🔧</span>
                                    <span id="modeToggleText">切换到高级模式</span>
                                </button>
                                <button class="btn btn-primary" onclick="saveConfigForm()">
                                    <span>💾</span>
                                    保存配置
                                </button>
                            </div>
                        </div>

                        <!-- 简易配置表单 -->
                        <div id="simpleConfig">
                            <div class="form-section">
                                <h3 class="form-section-title">🎯 基本设置</h3>

                                <div class="form-group">
                                    <label class="form-label">
                                        运行模式
                                        <span class="form-hint">选择服务器模式或客户端模式</span>
                                    </label>
                                    <select class="form-select" id="cfgMode" onchange="toggleModeFields()">
                                        <option value="server">🖥️ 服务器模式 (Server)</option>
                                        <option value="client">📱 客户端模式 (Client)</option>
                                    </select>
                                </div>

                                <div class="form-group" id="serverFields">
                                    <label class="form-label">
                                        监听地址
                                        <span class="form-hint">服务器监听的地址和端口</span>
                                    </label>
                                    <input type="text" class="form-input" id="cfgListen" placeholder="0.0.0.0:51820" value="0.0.0.0:51820">
                                </div>

                                <div class="form-group" id="clientFields" style="display: none;">
                                    <label class="form-label">
                                        服务器地址
                                        <span class="form-hint">要连接的服务器地址</span>
                                    </label>
                                    <input type="text" class="form-input" id="cfgEndpoint" placeholder="server.example.com:51820">
                                </div>

                                <div class="form-group">
                                    <label class="form-label">
                                        预共享密钥 (PSK)
                                        <span class="form-hint">用于加密连接的密钥，至少16字符</span>
                                    </label>
                                    <div style="position: relative;">
                                        <input type="password" class="form-input" id="cfgPSK" placeholder="输入安全的密钥">
                                        <button class="btn-icon" onclick="togglePasswordVisibility('cfgPSK')" style="position: absolute; right: 10px; top: 10px;">
                                            👁️
                                        </button>
                                    </div>
                                    <button class="btn btn-secondary" onclick="generatePSK()" style="margin-top: 8px;">
                                        <span>🎲</span>
                                        生成随机密钥
                                    </button>
                                </div>
                            </div>

                            <div class="form-section">
                                <h3 class="form-section-title">🔧 高级设置</h3>

                                <div class="form-group">
                                    <label class="form-label">
                                        管理接口地址
                                        <span class="form-hint">管理 API 监听地址，用于状态监控</span>
                                    </label>
                                    <input type="text" class="form-input" id="cfgManagement" placeholder="127.0.0.1:7777" value="127.0.0.1:7777">
                                </div>

                                <div class="form-group">
                                    <label class="form-label">
                                        保活间隔
                                        <span class="form-hint">心跳包发送间隔，例如: 15s, 30s</span>
                                    </label>
                                    <input type="text" class="form-input" id="cfgKeepalive" placeholder="15s" value="15s">
                                </div>

                                <div class="form-group">
                                    <label class="form-label">
                                        日志级别
                                        <span class="form-hint">控制日志输出详细程度</span>
                                    </label>
                                    <select class="form-select" id="cfgLogLevel">
                                        <option value="debug">🐛 Debug (调试)</option>
                                        <option value="info" selected>ℹ️ Info (信息)</option>
                                        <option value="warn">⚠️ Warn (警告)</option>
                                        <option value="error">❌ Error (错误)</option>
                                    </select>
                                </div>
                            </div>

                            <div class="form-section" id="peerSection" style="display: none;">
                                <h3 class="form-section-title">👥 对等节点配置 (服务器模式)</h3>
                                <div id="peersList"></div>
                                <button class="btn btn-secondary" onclick="addPeer()">
                                    <span>➕</span>
                                    添加节点
                                </button>
                            </div>
                        </div>

                        <!-- 高级配置编辑器 -->
                        <div id="advancedConfig" style="display: none;">
                            <div class="alert alert-warning" style="margin-bottom: 16px;">
                                <span>⚠️</span>
                                <span>高级模式: 直接编辑 JSON 配置文件。请确保格式正确！</span>
                            </div>
                            <div class="config-editor-container">
                                <textarea class="config-editor" id="configEditor" spellcheck="false" oninput="validateJSON()"></textarea>
                                <div class="config-validation" id="jsonValidation"></div>
                            </div>
                        </div>
                    </div>
                </div>

                <!-- Status Page -->
                <div class="page" id="status">
                    <div class="card">
                        <div class="card-header">
                            <h2 class="card-title">
                                <span>📈</span>
                                实时数据监控
                            </h2>
                            <button class="btn btn-secondary" onclick="refreshState()">
                                <span>🔄</span>
                                刷新
                            </button>
                        </div>
                        <div class="stats-grid">
                            <div class="stat-card">
                                <div class="stat-value" id="statSessions2">0</div>
                                <div class="stat-label">活跃会话</div>
                            </div>
                            <div class="stat-card">
                                <div class="stat-value" id="statConnections2">0</div>
                                <div class="stat-label">当前连接</div>
                            </div>
                            <div class="stat-card">
                                <div class="stat-value" id="statMessages2">0</div>
                                <div class="stat-label">消息总数</div>
                            </div>
                            <div class="stat-card">
                                <div class="stat-value" id="statMode2">-</div>
                                <div class="stat-label">运行模式</div>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    </div>

    <script>
        let refreshInterval = null;
        let lastUpdateTime = null;
        let logs = [];

        function switchPage(pageName) {
            document.querySelectorAll('.page').forEach(p => p.classList.remove('active'));
            document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));

            document.getElementById(pageName).classList.add('active');
            event.target.closest('.nav-item').classList.add('active');
        }

        function addLog(level, message) {
            const now = new Date();
            const time = now.toLocaleTimeString('zh-CN', { hour12: false });
            logs.push({ time, level, message });

            if (logs.length > 100) {
                logs.shift();
            }

            updateLogPanel();
        }

        function updateLogPanel() {
            const panel = document.getElementById('logPanel');
            panel.innerHTML = logs.map(log =>
                '<div class="log-entry">' +
                '<span class="log-time">' + log.time + '</span>' +
                '<span class="log-level-' + log.level + '">[' + log.level.toUpperCase() + ']</span> ' +
                '<span>' + log.message + '</span>' +
                '</div>'
            ).join('');
            panel.scrollTop = panel.scrollHeight;
        }

        function clearLogs() {
            logs = [];
            const panel = document.getElementById('logPanel');
            panel.innerHTML = '<div class="log-entry">' +
                '<span class="log-time">--:--:--</span>' +
                '<span class="log-level-info">日志已清空</span>' +
                '</div>';
        }

        function validateJSON() {
            const editor = document.getElementById('configEditor');
            const validation = document.getElementById('jsonValidation');

            try {
                JSON.parse(editor.value);
                validation.className = 'config-validation valid';
                validation.textContent = '✓ JSON 格式正确';
            } catch (err) {
                validation.className = 'config-validation invalid';
                validation.textContent = '✗ JSON 格式错误: ' + err.message;
            }
        }

        function updateUpdateTimes() {
            if (!lastUpdateTime) return;

            const timeStr = lastUpdateTime.toLocaleTimeString('zh-CN', { hour12: false });
            for (let i = 1; i <= 4; i++) {
                const el = document.getElementById('updateTime' + i);
                if (el) el.textContent = '更新于 ' + timeStr;
            }
        }

        async function loadConfig() {
            try {
                const resp = await fetch('/api/config');
                const data = await resp.json();
                document.getElementById('configEditor').value = JSON.stringify(data, null, 2);
            } catch (err) {
                showAlert('加载配置失败: ' + err.message, 'error');
            }
        }

        async function saveConfig() {
            try {
                const configText = document.getElementById('configEditor').value;
                const config = JSON.parse(configText);

                const resp = await fetch('/api/config', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(config)
                });

                if (resp.ok) {
                    showAlert('✅ 配置保存成功', 'success');
                } else {
                    const err = await resp.json();
                    showAlert('保存失败: ' + err.error, 'error');
                }
            } catch (err) {
                showAlert('配置格式错误: ' + err.message, 'error');
            }
        }

        async function startService() {
            const btn = document.getElementById('startBtn');
            btn.classList.add('loading');
            addLog('info', '正在启动服务...');

            try {
                const resp = await fetch('/api/start', { method: 'POST' });
                const data = await resp.json();

                if (data.success) {
                    showAlert('✅ 服务启动成功 (模式: ' + data.mode + ')', 'success');
                    addLog('info', '服务启动成功 - 模式: ' + data.mode);
                    updateButtonStates(true);
                    startAutoRefresh();
                } else {
                    const errMsg = data.error || '未知错误';
                    showAlert('启动失败: ' + errMsg, 'error');
                    addLog('error', '启动失败: ' + errMsg);
                }
            } catch (err) {
                showAlert('启动失败: ' + err.message, 'error');
                addLog('error', '启动失败: ' + err.message);
            } finally {
                btn.classList.remove('loading');
            }
        }

        async function stopService() {
            const btn = document.getElementById('stopBtn');
            btn.classList.add('loading');
            addLog('info', '正在停止服务...');

            try {
                const resp = await fetch('/api/stop', { method: 'POST' });
                const data = await resp.json();

                if (data.success) {
                    showAlert('✅ 服务已停止', 'success');
                    addLog('info', '服务已成功停止');
                    updateButtonStates(false);
                    stopAutoRefresh();
                }
            } catch (err) {
                showAlert('停止失败: ' + err.message, 'error');
                addLog('error', '停止失败: ' + err.message);
            } finally {
                btn.classList.remove('loading');
            }
        }

        async function refreshState() {
            try {
                const statusResp = await fetch('/api/status');
                const status = await statusResp.json();

                updateButtonStates(status.running);
                updateStats('statMode', status.mode || '-');
                updateStats('statMode2', status.mode || '-');

                if (status.running) {
                    const stateResp = await fetch('/api/state');
                    if (stateResp.ok) {
                        const state = await stateResp.json();
                        updateStatsFromState(state);
                        lastUpdateTime = new Date();
                        updateUpdateTimes();
                        updateCardStates(true);
                    }
                } else {
                    resetStats();
                    updateCardStates(false);
                }
            } catch (err) {
                console.error('刷新失败:', err);
                addLog('warn', '状态刷新失败: ' + err.message);
            }
        }

        function updateStatsFromState(state) {
            if (state.server) {
                updateStats('statSessions', state.server.count || 0);
                updateStats('statSessions2', state.server.count || 0);
                updateStats('statConnections', state.server.currentConnections || 0);
                updateStats('statConnections2', state.server.currentConnections || 0);
                updateStats('statMessages', state.server.messages || 0);
                updateStats('statMessages2', state.server.messages || 0);
            } else if (state.device) {
                const peers = state.device.peers?.length || 0;
                updateStats('statSessions', peers);
                updateStats('statSessions2', peers);
                updateStats('statMessages', state.device.messages || 0);
                updateStats('statMessages2', state.device.messages || 0);
            }
        }

        function updateCardStates(running) {
            const cards = ['cardSessions', 'cardConnections', 'cardMessages', 'cardMode'];
            cards.forEach(cardId => {
                const card = document.getElementById(cardId);
                if (card) {
                    if (running) {
                        card.classList.remove('status-inactive');
                        card.classList.add('status-active');
                    } else {
                        card.classList.remove('status-active');
                        card.classList.add('status-inactive');
                    }
                }
            });
        }

        function updateStats(id, value) {
            const el = document.getElementById(id);
            if (el) el.textContent = value;
        }

        function resetStats() {
            ['statSessions', 'statConnections', 'statMessages', 'statSessions2', 'statConnections2', 'statMessages2'].forEach(id => {
                updateStats(id, '0');
            });
        }

        function updateButtonStates(running) {
            const startBtn = document.getElementById('startBtn');
            const stopBtn = document.getElementById('stopBtn');
            const sidebar = document.getElementById('sidebarStatus');

            if (running) {
                startBtn.disabled = true;
                stopBtn.disabled = false;
                sidebar.className = 'status-badge status-running';
                sidebar.innerHTML = '<span class="status-dot" style="background: var(--success)"></span><span>运行中</span>';
            } else {
                startBtn.disabled = false;
                stopBtn.disabled = true;
                sidebar.className = 'status-badge status-stopped';
                sidebar.innerHTML = '<span class="status-dot" style="background: var(--danger)"></span><span>已停止</span>';
            }
        }

        function showAlert(message, type) {
            const alertBox = document.getElementById('alertBox');
            const icon = type === 'success' ? '✅' : '❌';
            alertBox.innerHTML = '<div class="alert alert-' + type + '"><span>' + icon + '</span><span>' + message + '</span></div>';
            setTimeout(() => { alertBox.innerHTML = ''; }, 5000);
        }

        function startAutoRefresh() {
            if (refreshInterval) return;
            refreshInterval = setInterval(refreshState, 3000);
        }

        function stopAutoRefresh() {
            if (refreshInterval) {
                clearInterval(refreshInterval);
                refreshInterval = null;
            }
        }

        let isAdvancedMode = false;
        let peerCounter = 0;

        function toggleAdvancedMode() {
            isAdvancedMode = !isAdvancedMode;
            const simpleConfig = document.getElementById('simpleConfig');
            const advancedConfig = document.getElementById('advancedConfig');
            const toggleText = document.getElementById('modeToggleText');

            if (isAdvancedMode) {
                simpleConfig.style.display = 'none';
                advancedConfig.style.display = 'block';
                toggleText.textContent = '切换到简易模式';
                formToJSON();
            } else {
                simpleConfig.style.display = 'block';
                advancedConfig.style.display = 'none';
                toggleText.textContent = '切换到高级模式';
                jsonToForm();
            }
        }

        function toggleModeFields() {
            const mode = document.getElementById('cfgMode').value;
            const serverFields = document.getElementById('serverFields');
            const clientFields = document.getElementById('clientFields');
            const peerSection = document.getElementById('peerSection');

            if (mode === 'server') {
                serverFields.style.display = 'block';
                clientFields.style.display = 'none';
                peerSection.style.display = 'block';
            } else {
                serverFields.style.display = 'none';
                clientFields.style.display = 'block';
                peerSection.style.display = 'none';
            }
        }

        function togglePasswordVisibility(id) {
            const input = document.getElementById(id);
            input.type = input.type === 'password' ? 'text' : 'password';
        }

        function generatePSK() {
            const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*';
            let psk = '';
            for (let i = 0; i < 32; i++) {
                psk += chars.charAt(Math.floor(Math.random() * chars.length));
            }
            document.getElementById('cfgPSK').value = psk;
            showAlert('✅ 已生成32位随机密钥', 'success');
        }

        function addPeer() {
            const peersList = document.getElementById('peersList');
            const peerId = ++peerCounter;

            const peerHTML = '<div class="peer-item" id="peer-' + peerId + '">' +
                '<div class="peer-header">' +
                '<strong>节点 #' + peerId + '</strong>' +
                '<button class="btn btn-danger" onclick="removePeer(' + peerId + ')" style="padding: 6px 12px; font-size: 12px;">' +
                '<span>🗑️</span> 删除</button>' +
                '</div>' +
                '<div class="form-group">' +
                '<label class="form-label">节点名称</label>' +
                '<input type="text" class="form-input peer-name" placeholder="client1" data-peer="' + peerId + '">' +
                '</div>' +
                '<div class="form-group">' +
                '<label class="form-label">允许的 IP 段</label>' +
                '<input type="text" class="form-input peer-ips" placeholder="10.0.0.0/24" data-peer="' + peerId + '">' +
                '</div>' +
                '</div>';

            peersList.insertAdjacentHTML('beforeend', peerHTML);
        }

        function removePeer(peerId) {
            const peer = document.getElementById('peer-' + peerId);
            if (peer) {
                peer.remove();
            }
        }

        function formToJSON() {
            const config = {
                mode: document.getElementById('cfgMode').value,
                psk: document.getElementById('cfgPSK').value,
                keepalive: document.getElementById('cfgKeepalive').value,
                management: {
                    bind: document.getElementById('cfgManagement').value
                },
                logging: {
                    level: document.getElementById('cfgLogLevel').value,
                    output: "stdout"
                }
            };

            if (config.mode === 'server') {
                config.listen = document.getElementById('cfgListen').value;
                config.peers = [];

                const peerNames = document.querySelectorAll('.peer-name');
                const peerIPs = document.querySelectorAll('.peer-ips');

                for (let i = 0; i < peerNames.length; i++) {
                    if (peerNames[i].value && peerIPs[i].value) {
                        config.peers.push({
                            name: peerNames[i].value,
                            allowedIPs: peerIPs[i].value.split(',').map(ip => ip.trim())
                        });
                    }
                }

                config.tunnel = { type: "loopback" };
            } else {
                config.endpoint = document.getElementById('cfgEndpoint').value;
            }

            document.getElementById('configEditor').value = JSON.stringify(config, null, 2);
        }

        function jsonToForm() {
            try {
                const config = JSON.parse(document.getElementById('configEditor').value);

                document.getElementById('cfgMode').value = config.mode || 'server';
                document.getElementById('cfgPSK').value = config.psk || '';
                document.getElementById('cfgKeepalive').value = config.keepalive || '15s';
                document.getElementById('cfgManagement').value = config.management?.bind || '127.0.0.1:7777';
                document.getElementById('cfgLogLevel').value = config.logging?.level || 'info';

                if (config.mode === 'server') {
                    document.getElementById('cfgListen').value = config.listen || '0.0.0.0:51820';

                    document.getElementById('peersList').innerHTML = '';
                    peerCounter = 0;

                    if (config.peers && config.peers.length > 0) {
                        config.peers.forEach(peer => {
                            addPeer();
                            const peerId = peerCounter;
                            document.querySelector('input.peer-name[data-peer="' + peerId + '"]').value = peer.name || '';
                            const ips = Array.isArray(peer.allowedIPs) ? peer.allowedIPs.join(', ') : peer.allowedIPs;
                            document.querySelector('input.peer-ips[data-peer="' + peerId + '"]').value = ips || '';
                        });
                    }
                } else {
                    document.getElementById('cfgEndpoint').value = config.endpoint || '';
                }

                toggleModeFields();
            } catch (err) {
                showAlert('解析配置失败: ' + err.message, 'error');
            }
        }

        async function saveConfigForm() {
            try {
                if (isAdvancedMode) {
                    await saveConfig();
                } else {
                    formToJSON();
                    await saveConfig();
                }
            } catch (err) {
                showAlert('保存失败: ' + err.message, 'error');
            }
        }

        async function loadConfigData() {
            await loadConfig();
            jsonToForm();
        }

        window.onload = function() {
            loadConfigData();
            refreshState();
        };
    </script>
</body>
</html>
`
