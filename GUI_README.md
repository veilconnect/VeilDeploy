# VeilDeploy 桌面版 GUI 使用指南

## 🎉 GUI 应用已创建完成！

VeilDeploy 现在提供一个现代化的桌面 GUI 控制面板，让您可以轻松管理 VPN 隧道服务。

## ✨ 功能特性

### 🎨 现代化界面
- **渐变紫色主题** - 美观的视觉设计
- **响应式布局** - 自适应不同屏幕尺寸
- **实时状态更新** - 自动刷新服务状态

### ⚡ 核心功能

1. **服务控制**
   - 一键启动/停止服务
   - 实时状态指示器
   - 自动检测运行状态

2. **配置管理**
   - 内置 JSON 配置编辑器
   - 语法高亮显示
   - 实时配置验证
   - 一键保存配置

3. **实时监控**
   - 活跃会话数
   - 当前连接数
   - 消息总数
   - 运行模式显示

4. **智能刷新**
   - 3 秒自动刷新状态
   - 手动刷新按钮
   - 启动时自动加载配置

## 🚀 启动 GUI

### Windows

```bash
# 双击运行
veildeploy-gui.exe

# 或命令行启动
./veildeploy-gui.exe

# 自定义端口（默认 8080）
./veildeploy-gui.exe 9000
```

### Linux/macOS

```bash
# 编译
go build -o veildeploy-gui ./cmd/gui

# 运行
./veildeploy-gui

# 自定义端口
./veildeploy-gui 9000
```

## 📖 使用说明

### 1. 启动 GUI

运行 `veildeploy-gui.exe` 后，GUI 会：
- 自动启动 HTTP 服务器（默认端口 8080）
- 自动打开浏览器访问 `http://localhost:8080`
- 加载当前目录的 `config.json` 配置文件

### 2. 编辑配置

在 **配置文件** 面板：
1. 直接编辑 JSON 配置
2. 点击 **保存配置** 按钮
3. 系统会自动验证 JSON 格式

示例配置：
```json
{
  "mode": "server",
  "listen": "0.0.0.0:51820",
  "psk": "your-secure-random-32-byte-psk-here",
  "keepalive": "15s",
  "peers": [
    {
      "name": "client1",
      "allowedIPs": ["10.0.0.0/24"]
    }
  ],
  "tunnel": {
    "type": "loopback"
  },
  "management": {
    "bind": "127.0.0.1:7777"
  },
  "logging": {
    "level": "info",
    "output": "stdout"
  }
}
```

### 3. 启动服务

1. 点击 **启动服务** 按钮
2. 查看成功提示消息
3. 状态指示器变为 **绿色"运行中"**
4. 自动开始实时监控

### 4. 监控状态

启动后可以查看：
- **活跃会话** - 当前连接的客户端数量
- **当前连接** - 活跃连接数/最大连接数
- **消息总数** - 已处理的消息数量
- **运行模式** - Server/Client

### 5. 停止服务

点击 **停止服务** 按钮即可安全关闭服务。

## 🎯 界面预览

### 头部
```
🛡️ VeilDeploy 控制面板          [运行中]
```

### 服务控制
```
⚡ 服务控制
[启动服务] [停止服务] [刷新状态] [保存配置]
```

### 实时状态
```
📊 实时状态
┌────────────┬────────────┬────────────┬────────────┐
│ 活跃会话    │ 当前连接    │ 消息总数    │ 运行模式    │
│     2      │   2/1000   │   15,234   │  Server    │
└────────────┴────────────┴────────────┴────────────┘
```

### 配置编辑器
```
⚙️ 配置文件 (config.json)
┌─────────────────────────────────────┐
│ {                                   │
│   "mode": "server",                 │
│   "listen": "0.0.0.0:51820",        │
│   ...                               │
│ }                                   │
└─────────────────────────────────────┘
```

## 🔧 高级功能

### API 端点

GUI 服务器提供以下 REST API：

| 端点 | 方法 | 说明 |
|------|------|------|
| `/` | GET | 主界面 HTML |
| `/api/config` | GET | 获取配置 |
| `/api/config` | POST | 保存配置 |
| `/api/start` | POST | 启动服务 |
| `/api/stop` | POST | 停止服务 |
| `/api/status` | GET | 获取运行状态 |
| `/api/state` | GET | 获取详细状态（代理到管理 API）|

### 自定义端口

```bash
# 使用 9000 端口
./veildeploy-gui.exe 9000

# 然后访问 http://localhost:9000
```

### 配置文件路径

默认使用当前目录的 `config.json`。可以修改源代码中的 `cfgPath` 变量自定义路径。

## 🛠️ 技术栈

- **后端**: Go (net/http)
- **前端**: HTML5 + CSS3 + Vanilla JavaScript
- **设计**: 渐变紫色主题，现代扁平化设计
- **响应式**: 支持桌面和移动设备

## 📱 跨平台支持

GUI 应用支持：
- ✅ Windows 10/11
- ✅ macOS 10.15+
- ✅ Linux (Ubuntu, Debian, Fedora, etc.)

浏览器要求：
- ✅ Chrome/Edge 90+
- ✅ Firefox 88+
- ✅ Safari 14+

## 🎨 界面配色

主题色彩：
- 主色调：`#667eea` (靛蓝色)
- 辅助色：`#764ba2` (紫色)
- 成功色：`#10b981` (绿色)
- 危险色：`#ef4444` (红色)
- 次要色：`#6b7280` (灰色)

## 🔒 安全说明

1. **本地访问** - GUI 默认只监听 `localhost`
2. **配置安全** - PSK 在配置文件中以明文存储，请保护好配置文件
3. **管理接口** - 通过 ACL 控制管理 API 访问
4. **HTTPS** - 如需公网访问，建议使用 Nginx 反向代理 + SSL

## 📊 性能监控

GUI 每 3 秒自动刷新以下指标：
- 会话统计
- 连接统计
- 消息计数
- 服务状态

## 🐛 故障排除

### 端口被占用

```
Error: listen tcp :8080: bind: address already in use
```

**解决方案**：使用其他端口
```bash
./veildeploy-gui.exe 9000
```

### 配置文件不存在

```
Error: Config load failed: open config.json: no such file or directory
```

**解决方案**：创建 `config.json` 或在 GUI 中编辑并保存

### 浏览器未自动打开

手动访问 `http://localhost:8080`

### 服务启动失败

检查：
1. 配置文件格式是否正确
2. PSK 是否符合要求（至少 16 字符，不能是默认值）
3. 端口是否被占用
4. Peer 配置是否完整

## 📞 帮助和支持

- **项目文档**: 查看 `OPTIMIZATIONS.md`, `CHANGELOG.md`, `TASKS_COMPLETED.md`
- **配置示例**: `config.example.json`
- **命令行版本**: 使用 `./veildeploy.exe` 获得更多控制选项

## 🎯 快速开始

1. 下载或编译 `veildeploy-gui.exe`
2. 双击运行（或命令行 `./veildeploy-gui.exe`）
3. 浏览器自动打开 GUI 界面
4. 编辑配置（如需）
5. 点击 **启动服务**
6. 开始使用！

---

**享受您的 VeilDeploy 桌面体验！** 🚀