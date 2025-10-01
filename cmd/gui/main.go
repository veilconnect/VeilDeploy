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

type GUIServer struct {
	cfg            *config.Config
	cfgPath        string
	device         *device.Device
	registry       *sessionRegistry
	mgmt           *management.Server
	reloadTracker  *state.ReloadTracker
	logger         *logging.Logger
	running        bool
	runningMu      sync.RWMutex
	serverCancel   context.CancelFunc
	limiter        *ratelimit.ConnectionLimiter
}

type sessionRegistry struct {
	mu       sync.RWMutex
	sessions map[uint64]*sessionState
	logger   *logging.Logger
	limiter  *ratelimit.ConnectionLimiter
}

type sessionState struct {
	device *device.Device
	conn   interface{}
}

func main() {
	port := 8080
	if len(os.Args) > 1 {
		fmt.Sscanf(os.Args[1], "%d", &port)
	}

	gui := &GUIServer{
		cfgPath:       "config.json",
		reloadTracker: state.NewReloadTracker(10),
	}

	// Setup HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", gui.handleIndex)
	mux.HandleFunc("/api/config", gui.handleConfig)
	mux.HandleFunc("/api/start", gui.handleStart)
	mux.HandleFunc("/api/stop", gui.handleStop)
	mux.HandleFunc("/api/status", gui.handleStatus)
	mux.HandleFunc("/api/state", gui.handleState)
	mux.HandleFunc("/api/logs", gui.handleLogs)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	// Open browser
	url := fmt.Sprintf("http://localhost:%d", port)
	fmt.Printf("VeilDeploy GUI starting at %s\n", url)
	go openBrowser(url)

	// Start server
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("GUI server error: %v", err)
		}
	}()

	// Wait for interrupt
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	fmt.Println("\nShutting down GUI server...")
	gui.stopService()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	fmt.Println("GUI server stopped")
}

func (g *GUIServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(htmlTemplate))
}

func (g *GUIServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "GET" {
		data, err := os.ReadFile(g.cfgPath)
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

		// Validate JSON
		var testCfg config.Config
		if err := json.Unmarshal(body, &testCfg); err != nil {
			http.Error(w, fmt.Sprintf(`{"error": "Invalid JSON: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}

		// Save config
		if err := os.WriteFile(g.cfgPath, body, 0644); err != nil {
			http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}

		g.cfg = &testCfg
		w.Write([]byte(`{"success": true}`))
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (g *GUIServer) handleStart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	g.runningMu.Lock()
	if g.running {
		g.runningMu.Unlock()
		w.Write([]byte(`{"error": "Service already running"}`))
		return
	}
	g.running = true
	g.runningMu.Unlock()

	// Load config
	cfg, err := config.Load(g.cfgPath)
	if err != nil {
		g.runningMu.Lock()
		g.running = false
		g.runningMu.Unlock()
		http.Error(w, fmt.Sprintf(`{"error": "Config load failed: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	g.cfg = cfg

	// Start service in background
	ctx, cancel := context.WithCancel(context.Background())
	g.serverCancel = cancel

	go func() {
		level := logging.ParseLevel(cfg.NormalisedLevel())
		baseLogger := logging.New(level, os.Stdout)
		g.logger = baseLogger.With(map[string]interface{}{"component": "stp"})

		mode := strings.ToLower(cfg.Mode)
		if mode == "client" {
			if err := g.runClient(ctx, cfg, baseLogger); err != nil {
				g.logger.Error("client error", map[string]interface{}{"error": err.Error()})
			}
		} else if mode == "server" {
			if err := g.runServer(ctx, cfg, baseLogger); err != nil {
				g.logger.Error("server error", map[string]interface{}{"error": err.Error()})
			}
		}

		g.runningMu.Lock()
		g.running = false
		g.runningMu.Unlock()
	}()

	w.Write([]byte(`{"success": true, "mode": "` + cfg.Mode + `"}`))
}

func (g *GUIServer) handleStop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	g.stopService()
	w.Write([]byte(`{"success": true}`))
}

func (g *GUIServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	g.runningMu.RLock()
	running := g.running
	g.runningMu.RUnlock()

	mode := "none"
	if g.cfg != nil {
		mode = g.cfg.Mode
	}

	resp := map[string]interface{}{
		"running": running,
		"mode":    mode,
	}

	json.NewEncoder(w).Encode(resp)
}

func (g *GUIServer) handleState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if g.mgmt == nil {
		http.Error(w, `{"error": "Service not running"}`, http.StatusServiceUnavailable)
		return
	}

	// Proxy to management API
	resp, err := http.Get("http://" + g.cfg.Management.Bind + "/state")
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	io.Copy(w, resp.Body)
}

func (g *GUIServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// Placeholder for log streaming
	w.Write([]byte(`{"logs": []}`))
}

func (g *GUIServer) stopService() {
	g.runningMu.Lock()
	if !g.running {
		g.runningMu.Unlock()
		return
	}
	g.running = false
	g.runningMu.Unlock()

	if g.serverCancel != nil {
		g.serverCancel()
	}

	if g.mgmt != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		g.mgmt.Close(ctx)
	}

	if g.device != nil {
		g.device.Close()
	}
}

func (g *GUIServer) runClient(ctx context.Context, cfg *config.Config, baseLogger *logging.Logger) error {
	logger := baseLogger.With(map[string]interface{}{"role": "client"})
	dev, err := device.NewDevice(device.RoleClient, cfg, logger)
	if err != nil {
		return err
	}
	g.device = dev
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
			"reloads": g.reloadTracker.GetHistory(),
		}
	}, logger, management.WithMetrics(dev.Metrics), management.WithACL(cfg.ManagementPrefixes()))
	if err != nil {
		return err
	}
	g.mgmt = mgmt
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

func (g *GUIServer) runServer(ctx context.Context, cfg *config.Config, baseLogger *logging.Logger) error {
	logger := baseLogger.With(map[string]interface{}{"role": "server"})

	network, address := parseEndpoint(cfg.Listen)
	listener, err := transport.Listen(network, address)
	if err != nil {
		return err
	}
	defer listener.Close()

	g.limiter = ratelimit.NewConnectionLimiter(
		cfg.EffectiveMaxConnections(),
		cfg.EffectiveConnectionRate(),
		cfg.EffectiveConnectionBurst(),
	)

	g.registry = &sessionRegistry{
		logger:  logger,
		limiter: g.limiter,
	}

	mgmt, err := management.New(cfg.Management.Bind, func() interface{} {
		return map[string]interface{}{
			"server":  map[string]interface{}{"sessions": 0},
			"reloads": g.reloadTracker.GetHistory(),
		}
	}, logger, management.WithACL(cfg.ManagementPrefixes()))
	if err != nil {
		return err
	}
	g.mgmt = mgmt
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

func openBrowser(url string) {
	time.Sleep(500 * time.Millisecond)
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

const htmlTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>VeilDeploy 控制面板</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            padding: 20px;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
        }
        .header {
            background: white;
            padding: 20px 30px;
            border-radius: 10px;
            box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
            margin-bottom: 20px;
            display: flex;
            justify-content: space-between;
            align-items: center;
        }
        .header h1 {
            color: #667eea;
            font-size: 28px;
        }
        .status-badge {
            padding: 8px 20px;
            border-radius: 20px;
            font-weight: bold;
            font-size: 14px;
        }
        .status-running {
            background: #10b981;
            color: white;
        }
        .status-stopped {
            background: #ef4444;
            color: white;
        }
        .panel {
            background: white;
            border-radius: 10px;
            box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
            padding: 25px;
            margin-bottom: 20px;
        }
        .panel h2 {
            color: #333;
            margin-bottom: 15px;
            font-size: 20px;
            border-bottom: 2px solid #667eea;
            padding-bottom: 10px;
        }
        .controls {
            display: flex;
            gap: 10px;
            margin-bottom: 20px;
        }
        .btn {
            padding: 12px 24px;
            border: none;
            border-radius: 6px;
            font-size: 16px;
            font-weight: bold;
            cursor: pointer;
            transition: all 0.3s;
        }
        .btn:hover {
            transform: translateY(-2px);
            box-shadow: 0 4px 12px rgba(0, 0, 0, 0.2);
        }
        .btn-primary {
            background: #10b981;
            color: white;
        }
        .btn-danger {
            background: #ef4444;
            color: white;
        }
        .btn-secondary {
            background: #6b7280;
            color: white;
        }
        .btn:disabled {
            opacity: 0.5;
            cursor: not-allowed;
        }
        textarea {
            width: 100%;
            min-height: 400px;
            padding: 15px;
            border: 2px solid #e5e7eb;
            border-radius: 6px;
            font-family: 'Courier New', monospace;
            font-size: 14px;
            resize: vertical;
        }
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 15px;
            margin-top: 15px;
        }
        .stat-card {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 20px;
            border-radius: 8px;
            text-align: center;
        }
        .stat-value {
            font-size: 32px;
            font-weight: bold;
            margin-bottom: 5px;
        }
        .stat-label {
            font-size: 14px;
            opacity: 0.9;
        }
        .log-output {
            background: #1e293b;
            color: #10b981;
            padding: 15px;
            border-radius: 6px;
            font-family: 'Courier New', monospace;
            font-size: 13px;
            max-height: 300px;
            overflow-y: auto;
        }
        .alert {
            padding: 15px;
            border-radius: 6px;
            margin-bottom: 15px;
        }
        .alert-success {
            background: #d1fae5;
            color: #065f46;
            border-left: 4px solid #10b981;
        }
        .alert-error {
            background: #fee2e2;
            color: #991b1b;
            border-left: 4px solid #ef4444;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🛡️ VeilDeploy 控制面板</h1>
            <span class="status-badge status-stopped" id="statusBadge">已停止</span>
        </div>

        <div class="panel">
            <h2>⚡ 服务控制</h2>
            <div class="controls">
                <button class="btn btn-primary" id="startBtn" onclick="startService()">启动服务</button>
                <button class="btn btn-danger" id="stopBtn" onclick="stopService()" disabled>停止服务</button>
                <button class="btn btn-secondary" onclick="refreshState()">刷新状态</button>
                <button class="btn btn-secondary" onclick="saveConfig()">保存配置</button>
            </div>
            <div id="alertBox"></div>
        </div>

        <div class="panel">
            <h2>📊 实时状态</h2>
            <div class="stats-grid" id="statsGrid">
                <div class="stat-card">
                    <div class="stat-value" id="statSessions">0</div>
                    <div class="stat-label">活跃会话</div>
                </div>
                <div class="stat-card">
                    <div class="stat-value" id="statConnections">0</div>
                    <div class="stat-label">当前连接</div>
                </div>
                <div class="stat-card">
                    <div class="stat-value" id="statMessages">0</div>
                    <div class="stat-label">消息总数</div>
                </div>
                <div class="stat-card">
                    <div class="stat-value" id="statMode">-</div>
                    <div class="stat-label">运行模式</div>
                </div>
            </div>
        </div>

        <div class="panel">
            <h2>⚙️ 配置文件 (config.json)</h2>
            <textarea id="configEditor" spellcheck="false"></textarea>
        </div>
    </div>

    <script>
        let refreshInterval = null;

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
                    showAlert('配置保存成功', 'success');
                } else {
                    const err = await resp.json();
                    showAlert('保存失败: ' + err.error, 'error');
                }
            } catch (err) {
                showAlert('配置格式错误: ' + err.message, 'error');
            }
        }

        async function startService() {
            try {
                const resp = await fetch('/api/start', { method: 'POST' });
                const data = await resp.json();

                if (data.success) {
                    showAlert('服务启动成功 (模式: ' + data.mode + ')', 'success');
                    updateButtonStates(true);
                    startAutoRefresh();
                } else {
                    showAlert('启动失败: ' + (data.error || '未知错误'), 'error');
                }
            } catch (err) {
                showAlert('启动失败: ' + err.message, 'error');
            }
        }

        async function stopService() {
            try {
                const resp = await fetch('/api/stop', { method: 'POST' });
                const data = await resp.json();

                if (data.success) {
                    showAlert('服务已停止', 'success');
                    updateButtonStates(false);
                    stopAutoRefresh();
                }
            } catch (err) {
                showAlert('停止失败: ' + err.message, 'error');
            }
        }

        async function refreshState() {
            try {
                const statusResp = await fetch('/api/status');
                const status = await statusResp.json();

                updateButtonStates(status.running);
                document.getElementById('statMode').textContent = status.mode || '-';

                if (status.running) {
                    const stateResp = await fetch('/api/state');
                    if (stateResp.ok) {
                        const state = await stateResp.json();
                        updateStats(state);
                    }
                }
            } catch (err) {
                console.error('刷新状态失败:', err);
            }
        }

        function updateStats(state) {
            if (state.server) {
                document.getElementById('statSessions').textContent = state.server.count || 0;
                document.getElementById('statConnections').textContent = state.server.currentConnections || 0;
                document.getElementById('statMessages').textContent = state.server.messages || 0;
            } else if (state.device) {
                document.getElementById('statSessions').textContent = state.device.peers?.length || 0;
                document.getElementById('statMessages').textContent = state.device.messages || 0;
            }
        }

        function updateButtonStates(running) {
            const badge = document.getElementById('statusBadge');
            const startBtn = document.getElementById('startBtn');
            const stopBtn = document.getElementById('stopBtn');

            if (running) {
                badge.textContent = '运行中';
                badge.className = 'status-badge status-running';
                startBtn.disabled = true;
                stopBtn.disabled = false;
            } else {
                badge.textContent = '已停止';
                badge.className = 'status-badge status-stopped';
                startBtn.disabled = false;
                stopBtn.disabled = true;
            }
        }

        function showAlert(message, type) {
            const alertBox = document.getElementById('alertBox');
            alertBox.innerHTML = '<div class="alert alert-' + type + '">' + message + '</div>';
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

        // 初始化
        window.onload = function() {
            loadConfig();
            refreshState();
        };
    </script>
</body>
</html>
`