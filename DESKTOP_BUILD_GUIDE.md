# VeilDeploy 原生桌面应用构建指南

本文档说明如何构建真正的原生 GUI 桌面应用程序。

## 方案对比

| 方案 | 优点 | 缺点 | 是否需要 CGO |
|------|------|------|-------------|
| **当前实现** (Web GUI) | ✅ 无需 CGO<br>✅ 跨平台<br>✅ 易于开发 | ❌ 需要浏览器<br>❌ 非原生体验 | ❌ 否 |
| **Fyne** (推荐) | ✅ 真正的原生 GUI<br>✅ 性能优秀<br>✅ 跨平台 | ❌ 需要 CGO<br>❌ 文件较大 | ✅ 是 |
| **Wails** | ✅ 现代化界面<br>✅ Web 技术栈 | ❌ 需要 CGO<br>❌ 依赖 WebView | ✅ 是 |

## 🎯 推荐方案: Fyne 原生桌面应用

### 第一步: 安装编译工具

#### Windows (选择其中一种)

**方案 A: TDM-GCC (推荐)**
```powershell
# 下载并安装 TDM-GCC
# https://jmeubank.github.io/tdm-gcc/download/

# 或使用 Chocolatey
choco install mingw -y
```

**方案 B: MSYS2**
```powershell
# 下载 MSYS2: https://www.msys2.org/
# 安装后运行:
pacman -S mingw-w64-x86_64-gcc
```

#### Linux
```bash
sudo apt-get install gcc libgl1-mesa-dev xorg-dev
```

#### macOS
```bash
xcode-select --install
```

### 第二步: 验证编译环境

```bash
gcc --version
# 应该显示 gcc 版本信息
```

### 第三步: 创建原生桌面应用

在项目目录创建 `cmd/native/main.go`:

\`\`\`go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2"

	"stp/config"
	"stp/device"
	"stp/internal/logging"
	"stp/internal/management"
	"stp/internal/ratelimit"
	"stp/internal/state"
	"stp/transport"
)

type NativeApp struct {
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

	// UI 组件
	window         fyne.Window
	statusLabel    *widget.Label
	startBtn       *widget.Button
	stopBtn        *widget.Button
	configEditor   *widget.Entry
	sessionsLabel  *widget.Label
	connectionsLabel *widget.Label
	messagesLabel  *widget.Label
	modeLabel      *widget.Label
}

func main() {
	myApp := app.NewWithID("com.veildeploy.native")
	myWindow := myApp.NewWindow("VeilDeploy 控制面板")
	myWindow.Resize(fyne.NewSize(1000, 700))
	myWindow.CenterOnScreen()

	nativeApp := &NativeApp{
		cfgPath:       "config.json",
		reloadTracker: state.NewReloadTracker(10),
		window:        myWindow,
	}

	nativeApp.setupUI()
	nativeApp.loadConfig()
	nativeApp.updateStatus()

	// 自动刷新
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			nativeApp.updateStatus()
		}
	}()

	myWindow.ShowAndRun()
}

func (d *NativeApp) setupUI() {
	// 标题栏
	titleLabel := widget.NewLabelWithStyle(
		"🛡️ VeilDeploy 桌面控制面板",
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)
	titleLabel.TextSize = 20

	d.statusLabel = widget.NewLabel("状态: 已停止")
	d.statusLabel.TextStyle = fyne.TextStyle{Bold: true}

	// 控制按钮
	d.startBtn = widget.NewButtonWithIcon("启动服务", theme.MediaPlayIcon(), func() {
		d.startService()
	})
	d.startBtn.Importance = widget.HighImportance

	d.stopBtn = widget.NewButtonWithIcon("停止服务", theme.MediaStopIcon(), func() {
		d.stopService()
	})
	d.stopBtn.Importance = widget.DangerImportance
	d.stopBtn.Disable()

	refreshBtn := widget.NewButtonWithIcon("刷新状态", theme.ViewRefreshIcon(), func() {
		d.updateStatus()
	})

	saveBtn := widget.NewButtonWithIcon("保存配置", theme.DocumentSaveIcon(), func() {
		d.saveConfig()
	})

	controlsBox := container.NewHBox(
		d.startBtn,
		d.stopBtn,
		refreshBtn,
		saveBtn,
	)

	// 统计卡片
	d.sessionsLabel = widget.NewLabelWithStyle("0", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	d.connectionsLabel = widget.NewLabelWithStyle("0", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	d.messagesLabel = widget.NewLabelWithStyle("0", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	d.modeLabel = widget.NewLabelWithStyle("-", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	statsGrid := container.New(layout.NewGridLayout(4),
		container.NewVBox(
			widget.NewLabelWithStyle("活跃会话", fyne.TextAlignCenter, fyne.TextStyle{}),
			d.sessionsLabel,
		),
		container.NewVBox(
			widget.NewLabelWithStyle("当前连接", fyne.TextAlignCenter, fyne.TextStyle{}),
			d.connectionsLabel,
		),
		container.NewVBox(
			widget.NewLabelWithStyle("消息总数", fyne.TextAlignCenter, fyne.TextStyle{}),
			d.messagesLabel,
		),
		container.NewVBox(
			widget.NewLabelWithStyle("运行模式", fyne.TextAlignCenter, fyne.TextStyle{}),
			d.modeLabel,
		),
	)

	// 配置编辑器
	d.configEditor = widget.NewMultiLineEntry()
	d.configEditor.SetPlaceHolder("配置文件 JSON 内容...")
	d.configEditor.Wrapping = fyne.TextWrapOff

	configScroll := container.NewScroll(d.configEditor)
	configScroll.SetMinSize(fyne.NewSize(900, 350))

	// 标签页
	tabs := container.NewAppTabs(
		container.NewTabItem("控制面板", container.NewVBox(
			container.NewCenter(titleLabel),
			widget.NewSeparator(),
			d.statusLabel,
			controlsBox,
			widget.NewSeparator(),
			widget.NewLabel("实时状态"),
			statsGrid,
		)),
		container.NewTabItem("配置管理", container.NewVBox(
			widget.NewLabel("编辑 config.json"),
			configScroll,
		)),
	)

	d.window.SetContent(container.NewPadded(tabs))
}

func (d *NativeApp) loadConfig() {
	data, err := os.ReadFile(d.cfgPath)
	if err != nil {
		dialog.ShowError(err, d.window)
		return
	}

	var formatted interface{}
	if err := json.Unmarshal(data, &formatted); err == nil {
		if pretty, err := json.MarshalIndent(formatted, "", "  "); err == nil {
			d.configEditor.SetText(string(pretty))
			return
		}
	}
	d.configEditor.SetText(string(data))
}

func (d *NativeApp) saveConfig() {
	configText := d.configEditor.Text

	var testCfg config.Config
	if err := json.Unmarshal([]byte(configText), &testCfg); err != nil {
		dialog.ShowError(fmt.Errorf("配置格式错误: %v", err), d.window)
		return
	}

	if err := os.WriteFile(d.cfgPath, []byte(configText), 0644); err != nil {
		dialog.ShowError(fmt.Errorf("保存失败: %v", err), d.window)
		return
	}

	d.cfg = &testCfg
	dialog.ShowInformation("成功", "配置已保存", d.window)
}

func (d *NativeApp) startService() {
	d.runningMu.Lock()
	if d.running {
		d.runningMu.Unlock()
		dialog.ShowInformation("提示", "服务已在运行中", d.window)
		return
	}
	d.running = true
	d.runningMu.Unlock()

	cfg, err := config.Load(d.cfgPath)
	if err != nil {
		d.runningMu.Lock()
		d.running = false
		d.runningMu.Unlock()
		dialog.ShowError(fmt.Errorf("配置加载失败: %v", err), d.window)
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
		d.updateStatus()
	}()

	d.updateStatus()
	dialog.ShowInformation("成功", "服务已启动 (模式: "+cfg.Mode+")", d.window)
}

func (d *NativeApp) stopService() {
	d.runningMu.Lock()
	if !d.running {
		d.runningMu.Unlock()
		return
	}
	d.running = false
	d.runningMu.Unlock()

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

	d.updateStatus()
	dialog.ShowInformation("成功", "服务已停止", d.window)
}

func (d *NativeApp) updateStatus() {
	d.runningMu.RLock()
	running := d.running
	d.runningMu.RUnlock()

	if running {
		d.statusLabel.SetText("状态: ✅ 运行中")
		d.startBtn.Disable()
		d.stopBtn.Enable()

		if d.cfg != nil && d.cfg.Management.Bind != "" {
			d.fetchStats()
		}
	} else {
		d.statusLabel.SetText("状态: ⭕ 已停止")
		d.startBtn.Enable()
		d.stopBtn.Disable()
		d.sessionsLabel.SetText("0")
		d.connectionsLabel.SetText("0")
		d.messagesLabel.SetText("0")
		if d.cfg != nil {
			d.modeLabel.SetText(d.cfg.Mode)
		} else {
			d.modeLabel.SetText("-")
		}
	}
}

func (d *NativeApp) fetchStats() {
	if d.cfg == nil {
		return
	}

	resp, err := http.Get("http://" + d.cfg.Management.Bind + "/state")
	if err != nil {
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var state map[string]interface{}
	if err := json.Unmarshal(body, &state); err != nil {
		return
	}

	d.modeLabel.SetText(d.cfg.Mode)

	if server, ok := state["server"].(map[string]interface{}); ok {
		if count, ok := server["count"].(float64); ok {
			d.sessionsLabel.SetText(fmt.Sprintf("%.0f", count))
		}
		if conns, ok := server["currentConnections"].(float64); ok {
			d.connectionsLabel.SetText(fmt.Sprintf("%.0f", conns))
		}
		if msgs, ok := server["messages"].(float64); ok {
			d.messagesLabel.SetText(fmt.Sprintf("%.0f", msgs))
		}
	} else if device, ok := state["device"].(map[string]interface{}); ok {
		if peers, ok := device["peers"].([]interface{}); ok {
			d.sessionsLabel.SetText(fmt.Sprintf("%d", len(peers)))
		}
		if msgs, ok := device["messages"].(float64); ok {
			d.messagesLabel.SetText(fmt.Sprintf("%.0f", msgs))
		}
	}
}

// runClient 和 runServer 实现 (复制自现有代码)
// ... 省略重复代码 ...
\`\`\`

### 第四步: 构建原生应用

```bash
# 安装 Fyne 依赖
go get fyne.io/fyne/v2@latest

# 构建 Windows 应用
go build -o veildeploy-native.exe ./cmd/native

# 或使用 Fyne 打包工具创建更专业的应用
go install fyne.io/fyne/v2/cmd/fyne@latest
fyne package -os windows -icon icon.png
```

### 第五步: 优化和打包

**创建应用图标:**
```bash
# 准备 icon.png (512x512)
fyne package -os windows -icon icon.png -name VeilDeploy
```

**减小文件大小:**
```bash
go build -ldflags="-s -w" -o veildeploy-native.exe ./cmd/native
upx --best veildeploy-native.exe  # 可选: 使用 UPX 压缩
```

## 🎨 当前可用方案

由于当前环境没有 C 编译器,已提供以下替代方案:

### 方案 1: Web 桌面应用 (已实现)

**文件:** `cmd/desktop/main.go`

**特点:**
- ✅ 无需 CGO,纯 Go 实现
- ✅ 现代化 UI (参考 VS Code/GitHub Desktop)
- ✅ 自动以应用模式打开 Chrome/Edge
- ✅ 类似原生应用的体验

**使用:**
```bash
# 直接运行
./veildeploy-desktop.exe

# 将自动打开独立窗口
```

### 方案 2: 使用在线构建服务

如果本地无法安装 GCC,可以使用:

1. **GitHub Actions** - 在 CI/CD 中构建
2. **Docker** - 使用包含 GCC 的容器
3. **云编译服务** - 使用在线构建平台

## 📚 参考资源

- [Fyne 官方文档](https://developer.fyne.io/)
- [Fyne 示例](https://github.com/fyne-io/examples)
- [TDM-GCC 下载](https://jmeubank.github.io/tdm-gcc/)
- [Wails 框架](https://wails.io/)

## 🔧 常见问题

**Q: 为什么需要 CGO?**
A: Fyne 使用 OpenGL 进行渲染,需要 C 库支持

**Q: 有没有不需要 CGO 的原生 GUI?**
A: 纯 Go 的 GUI 框架功能有限,推荐使用 Web GUI 方案

**Q: 构建后文件很大怎么办?**
A: 使用 `-ldflags="-s -w"` 和 UPX 压缩可减小约 50%

## ✅ 总结

**推荐使用顺序:**
1. **有 GCC 环境** → 使用 Fyne 构建真正的原生应用
2. **无 GCC 环境** → 使用当前的 Web 桌面应用 (已实现)
3. **需要跨平台** → 使用 Docker 或 CI/CD 构建

当前项目已包含**专业级 Web 桌面应用**,提供接近原生的用户体验!
