# VeilDeploy 用户使用指南

## 🎯 VeilDeploy 是什么？

VeilDeploy 是一个**安全加密隧道软件**，类似于 VPN，提供：
- 🔐 端到端加密通信
- 🌐 点对点网络隧道
- 🚀 高性能数据传输
- 📊 实时监控和管理

## 📦 三种使用方式

### 方式1️⃣: 命令行模式（高级用户）

适合服务器部署和自动化场景。

#### 服务器端
```bash
# 启动服务器
./veildeploy.exe -config config.json

# 或指定模式
./veildeploy.exe -config config.json -mode server
```

#### 客户端
```bash
# 启动客户端
./veildeploy.exe -config client-config.json -mode client
```

---

### 方式2️⃣: 桌面 GUI（推荐普通用户）

提供可视化管理界面，简单易用。

```bash
# 启动 GUI
./veildeploy-gui.exe

# 自动打开浏览器访问 http://localhost:8080
```

**GUI 功能**:
- ✅ 可视化配置编辑
- ✅ 一键启动/停止服务
- ✅ 实时状态监控
- ✅ 图形化统计面板

![GUI Screenshot](gui-preview.png)

---

### 方式3️⃣: 管理 API（开发者）

通过 HTTP API 管理和监控服务。

```bash
# 查看状态
curl http://127.0.0.1:7777/state

# 查看指标
curl http://127.0.0.1:7777/metrics
```

---

## 🚀 快速开始

### 场景1: 本地测试（最简单）

**步骤1**: 启动服务器
```bash
./veildeploy.exe -config config.json
```

**步骤2**: 新开终端启动客户端
```bash
./veildeploy.exe -config client-config.json -mode client
```

**步骤3**: 查看连接状态
```bash
# 服务器状态
curl http://127.0.0.1:7777/state

# 客户端状态
curl http://127.0.0.1:7778/state
```

✅ **成功标志**: 看到 "handshake complete" 和相同的 sessionId

---

### 场景2: 远程部署

#### 服务器配置 (config.json)
```json
{
  "mode": "server",
  "listen": "0.0.0.0:51820",  // 监听所有网卡
  "psk": "your-secure-random-32-byte-psk-here",
  "peers": [
    {
      "name": "client1",
      "allowedIPs": ["10.0.0.0/24"]
    }
  ],
  "tunnel": {
    "type": "loopback"  // 或 "tun" 用于真实网络
  }
}
```

#### 客户端配置 (client-config.json)
```json
{
  "mode": "client",
  "endpoint": "your-server-ip:51820",  // 服务器公网 IP
  "psk": "your-secure-random-32-byte-psk-here",  // 与服务器相同
  "peers": [
    {
      "name": "server",
      "allowedIPs": ["10.0.0.0/24"]
    }
  ],
  "tunnel": {
    "type": "loopback"
  }
}
```

---

## 📖 配置文件详解

### 基本配置

| 字段 | 说明 | 示例 |
|------|------|------|
| `mode` | 运行模式 | `"server"` 或 `"client"` |
| `listen` | 监听地址（服务器） | `"0.0.0.0:51820"` |
| `endpoint` | 服务器地址（客户端） | `"server.com:51820"` |
| `psk` | 预共享密钥（必须相同）| 至少16字符 |

### Tunnel 配置

| 类型 | 说明 | 使用场景 |
|------|------|----------|
| `loopback` | 回环测试 | 开发和测试 |
| `udp-bridge` | UDP桥接 | 应用层隧道 |
| `tun` | TUN设备 | 真实VPN网络 |

### TUN 模式配置（高级）

```json
{
  "tunnel": {
    "type": "tun",
    "name": "stp0",           // 接口名称
    "mtu": 1420,              // MTU大小
    "address": "10.0.0.1/24", // 本地IP
    "autoConfigure": true,    // 自动配置
    "routes": [               // 额外路由
      "192.168.0.0/16"
    ]
  }
}
```

---

## 📊 实时监控

### 服务器状态

当前连接：
```json
{
  "server": {
    "sessions": 1,           // 活跃会话数
    "currentConnections": 1, // 当前连接数
    "maxConnections": 1000,  // 最大连接数
    "availableTokens": 9     // 可用令牌
  }
}
```

### 客户端状态

```json
{
  "device": {
    "role": "client",
    "sessionId": "a8cd9712...",  // 会话ID
    "messages": 15,              // 消息计数
    "peers": [{
      "name": "server",
      "endpoint": "127.0.0.1:51820",
      "lastHandshake": "2025-09-30T22:19:23Z"
    }]
  }
}
```

---

## 🔧 常用操作

### 查看日志

服务运行时会输出结构化日志：

```json
{"level":"info","message":"handshake complete","sessionId":"a8cd9712..."}
{"level":"info","message":"session added","session":1}
```

日志级别：`error` < `warn` < `info` < `debug`

### 热重载配置

修改 `config.json` 后，5秒内自动应用（无需重启）：

支持热重载的参数：
- ✅ `management.acl` - 管理ACL
- ✅ `logging.level` - 日志级别
- ✅ `peers` - Peer配置
- ✅ `maxConnections` - 连接限制
- ✅ `connectionRate` - 速率限制

### 停止服务

```bash
# Ctrl+C 或发送 SIGTERM
kill <pid>
```

服务会优雅关闭，等待现有会话结束。

---

## 🔒 安全建议

### 1. 使用强PSK

❌ **不要使用**:
- 默认值
- 短密码（< 16字符）
- 简单密码

✅ **推荐**:
```bash
# 生成安全的PSK
openssl rand -base64 32

# 或使用
head /dev/urandom | tr -dc A-Za-z0-9 | head -c 32
```

### 2. 限制管理接口

```json
{
  "management": {
    "bind": "127.0.0.1:7777",  // 只监听本地
    "acl": ["127.0.0.0/8"]     // 只允许本地访问
  }
}
```

### 3. 启用防火墙

```bash
# 只开放必要端口
ufw allow 51820/udp
ufw deny 7777
```

---

## 🐛 故障排除

### 问题1: 连接失败

**症状**: 客户端无法连接服务器

**检查**:
```bash
# 1. 服务器是否运行
curl http://server-ip:7777/state

# 2. 防火墙是否开放
telnet server-ip 51820

# 3. PSK是否匹配
grep psk config.json client-config.json
```

### 问题2: 端口被占用

```
Error: listen udp :51820: bind: address already in use
```

**解决**:
```bash
# 查找占用进程
netstat -ano | findstr :51820

# 停止进程或更换端口
```

### 问题3: 握手失败

```
Error: handshake failed: authentication failed
```

**原因**: PSK不匹配

**解决**: 确保服务器和客户端使用相同的PSK

---

## 📈 性能优化

### 调整MTU

```json
{
  "tunnel": {
    "mtu": 1420  // 默认值，根据网络调整
  }
}
```

网络环境建议：
- 局域网: 1500
- 互联网: 1420
- 移动网络: 1280

### 连接限制

```json
{
  "maxConnections": 1000,    // 最大连接数
  "connectionRate": 100,     // 每分钟新连接数
  "connectionBurst": 10      // 突发连接数
}
```

### Keepalive 间隔

```json
{
  "keepalive": "15s"  // 保活间隔，建议5-30秒
}
```

---

## 🎓 高级功能

### 多Peer配置

```json
{
  "peers": [
    {"name": "client1", "allowedIPs": ["10.0.1.0/24"]},
    {"name": "client2", "allowedIPs": ["10.0.2.0/24"]},
    {"name": "client3", "allowedIPs": ["10.0.3.0/24"]}
  ]
}
```

### Rekey配置

```json
{
  "rekeyInterval": "30m",  // 30分钟重新密钥
  "rekeyBudget": 16000     // 或16000条消息后
}
```

### 日志输出

```json
{
  "logging": {
    "level": "info",        // error/warn/info/debug
    "output": "stdout"      // stdout 或文件路径
  }
}
```

---

## 📞 帮助和支持

### 命令行帮助

```bash
./veildeploy.exe -h
```

### 配置验证

```bash
# 测试配置文件
./veildeploy.exe -config config.json -mode server &
sleep 2
curl http://127.0.0.1:7777/state
kill $!
```

### 查看版本

```bash
./veildeploy.exe -version
```

---

## 📚 示例场景

### 场景A: 简单点对点

**用途**: 两台电脑之间加密通信

**服务器** (电脑A):
```bash
./veildeploy.exe -config config.json
```

**客户端** (电脑B):
```bash
./veildeploy.exe -config client-config.json -mode client
```

### 场景B: 多客户端访问

**服务器** (中心服务器):
```json
{
  "mode": "server",
  "listen": "0.0.0.0:51820",
  "peers": [
    {"name": "laptop", "allowedIPs": ["10.0.0.10/32"]},
    {"name": "phone", "allowedIPs": ["10.0.0.20/32"]},
    {"name": "tablet", "allowedIPs": ["10.0.0.30/32"]}
  ]
}
```

每个客户端使用自己的配置文件连接。

### 场景C: TUN模式真实VPN

**服务器**:
```json
{
  "tunnel": {
    "type": "tun",
    "name": "stp0",
    "address": "10.0.0.1/24",
    "autoConfigure": true
  }
}
```

**客户端**:
```json
{
  "tunnel": {
    "type": "tun",
    "name": "stp0",
    "address": "10.0.0.2/24",
    "autoConfigure": true,
    "routes": ["192.168.0.0/16"]  // 路由内网流量
  }
}
```

---

## 🎉 总结

VeilDeploy 提供三种使用方式：

1. **命令行** - 服务器部署，自动化
2. **GUI** - 简单易用，可视化管理
3. **API** - 编程集成，监控告警

选择适合您的方式，开始使用安全加密隧道！

**关键要点**:
- ✅ PSK必须匹配
- ✅ 防火墙开放端口
- ✅ 查看日志排查问题
- ✅ 使用管理API监控

**需要帮助？** 查看文档或提交 Issue。