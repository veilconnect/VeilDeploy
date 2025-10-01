# 🚀 VeilDeploy 快速开始指南

一键部署 VeilDeploy VPN 服务器，只需 2 分钟！

---

## 📋 前提条件

- 一台云服务器 (Ubuntu/Debian)
- root 权限
- 服务器的 IP 地址和密码

**支持的系统**:
- Ubuntu 20.04+
- Ubuntu 25.04
- Debian 10+

---

## 🎯 一键部署

### 方法 1: 直接部署（推荐）

在您的**本地电脑**上运行：

```bash
# 下载部署脚本
wget https://raw.githubusercontent.com/veilconnect/VeilDeploy/main/deploy_script.sh

# 上传并执行（将 YOUR_SERVER_IP 替换为您的服务器 IP）
cat deploy_script.sh | ssh root@YOUR_SERVER_IP "bash"
```

**就这么简单！** 🎉

---

### 方法 2: 在服务器上部署

登录到您的服务器后运行：

```bash
# 下载并执行
wget -O- https://raw.githubusercontent.com/veilconnect/VeilDeploy/main/deploy_script.sh | bash
```

或

```bash
# 下载后查看再执行
wget https://raw.githubusercontent.com/veilconnect/VeilDeploy/main/deploy_script.sh
cat deploy_script.sh  # 查看脚本内容（可选）
chmod +x deploy_script.sh
./deploy_script.sh
```

---

## ⏱️ 部署过程

脚本会自动完成以下步骤（约 2-3 分钟）：

```
[1/8] ✓ 检查系统信息
[2/8] ✓ 更新系统并安装依赖
[3/8] ✓ 启用 BBR TCP 优化
[4/8] ✓ 安装 Go 1.21.5
[5/8] ✓ 克隆并编译 VeilDeploy
[6/8] ✓ 配置防火墙
[7/8] ✓ 创建服务配置
[8/8] ✓ 启动服务
```

---

## 🔑 获取连接信息

部署完成后，在服务器上运行：

```bash
cat /root/veildeploy-credentials.txt
```

您会看到：

```
========================================
VeilDeploy 部署成功！
========================================

服务器信息:
  IP: YOUR_SERVER_IP
  端口: 51820 (UDP)
  密码: [自动生成的密码]

客户端配置:
{
  "mode": "client",
  "endpoint": "YOUR_SERVER_IP:51820",
  "psk": "[自动生成的密码]",
  ...
}
```

**重要**: 请保存此文件的内容，它包含您的连接信息！

---

## 💻 客户端使用

### Windows 客户端

1. **下载客户端**:
   - 前往 [Releases 页面](https://github.com/veilconnect/VeilDeploy/releases)
   - 下载 `veildeploy-windows-amd64.exe`

2. **创建配置文件** `client-config.json`:
   ```json
   {
     "mode": "client",
     "endpoint": "YOUR_SERVER_IP:51820",
     "psk": "YOUR_PASSWORD",
     "keepalive": "25s",
     "maxPadding": 255,
     "peers": [
       {
         "name": "server",
         "endpoint": "YOUR_SERVER_IP:51820",
         "allowedIPs": ["0.0.0.0/0"]
       }
     ],
     "tunnel": {
       "type": "tun",
       "name": "veil0",
       "mtu": 1420,
       "address": "10.0.0.2/24"
     }
   }
   ```

3. **安装 WinTUN 驱动**:
   - 下载 [WinTUN](https://www.wintun.net/)
   - 将 `wintun.dll` 放在 `veildeploy.exe` 同一目录

4. **以管理员身份运行**:
   ```powershell
   .\veildeploy.exe -config client-config.json -mode client
   ```

### Linux 客户端

```bash
# 下载客户端
wget https://github.com/veilconnect/VeilDeploy/releases/latest/download/veildeploy-linux-amd64
chmod +x veildeploy-linux-amd64

# 创建配置文件 client-config.json（内容同上）

# 运行
sudo ./veildeploy-linux-amd64 -config client-config.json -mode client
```

### macOS 客户端

```bash
# 下载客户端
wget https://github.com/veilconnect/VeilDeploy/releases/latest/download/veildeploy-darwin-amd64
chmod +x veildeploy-darwin-amd64

# 创建配置文件并运行
sudo ./veildeploy-darwin-amd64 -config client-config.json -mode client
```

---

## 🔧 验证部署

### 检查服务状态

```bash
systemctl status veildeploy
```

应该显示 `Active: active (running)`

### 查看日志

```bash
journalctl -u veildeploy -f
```

### 检查端口

```bash
ss -ulnp | grep 51820
```

应该显示 veildeploy 正在监听 UDP 51820

### 查看指标

```bash
curl http://127.0.0.1:7777/metrics
```

输出示例：
```
server_available_tokens 10
server_current_connections 0
server_max_connections 1000
server_messages_total 0
server_sessions 0
```

---

## 🛠️ 管理命令

### 服务控制

```bash
# 启动服务
systemctl start veildeploy

# 停止服务
systemctl stop veildeploy

# 重启服务
systemctl restart veildeploy

# 查看状态
systemctl status veildeploy

# 开机自启（已自动配置）
systemctl enable veildeploy
```

### 查看日志

```bash
# 实时日志
journalctl -u veildeploy -f

# 最近 100 行
journalctl -u veildeploy -n 100

# 今天的日志
journalctl -u veildeploy --since today
```

### 配置管理

```bash
# 查看配置
cat /etc/veildeploy/config.json

# 编辑配置
nano /etc/veildeploy/config.json

# 修改后重启服务
systemctl restart veildeploy
```

---

## 🆘 故障排查

### 服务无法启动

**查看错误日志**:
```bash
journalctl -u veildeploy -n 50 --no-pager
```

**常见问题**:

1. **端口被占用**:
   ```bash
   ss -ulnp | grep 51820
   # 如果被占用，停止占用进程或更改端口
   ```

2. **配置文件错误**:
   ```bash
   /usr/local/bin/veildeploy -config /etc/veildeploy/config.json -mode server
   # 手动运行检查错误
   ```

### 客户端无法连接

**检查清单**:

- [ ] 服务器服务正在运行: `systemctl status veildeploy`
- [ ] 防火墙已开放端口: `ufw status`
- [ ] 服务器 IP 地址正确
- [ ] 密码 (PSK) 匹配
- [ ] 客户端有管理员权限
- [ ] WinTUN 驱动已安装（Windows）

**测试连通性**:
```bash
# 测试服务器可达
ping YOUR_SERVER_IP

# 测试 UDP 端口（需要 nc 工具）
nc -u -v YOUR_SERVER_IP 51820
```

### 重新生成密码

```bash
# 生成新密码
NEW_PSK=$(openssl rand -base64 24)
echo $NEW_PSK

# 更新服务器配置
sed -i "s/\"psk\": \".*\"/\"psk\": \"$NEW_PSK\"/" /etc/veildeploy/config.json

# 重启服务
systemctl restart veildeploy

# 记得同时更新客户端配置！
```

---

## 🔄 更新 VeilDeploy

### 自动更新脚本

创建更新脚本：

```bash
cat > /root/update-veildeploy.sh << 'EOF'
#!/bin/bash
set -e

echo "更新 VeilDeploy..."

cd /root/VeilDeploy
git pull

/usr/local/go/bin/go build -o veildeploy .

systemctl stop veildeploy
cp veildeploy /usr/local/bin/
systemctl start veildeploy

echo "✓ 更新完成"
systemctl status veildeploy
EOF

chmod +x /root/update-veildeploy.sh
```

### 执行更新

```bash
/root/update-veildeploy.sh
```

---

## 🔐 安全建议

### 1. 修改 SSH 端口

```bash
# 编辑 SSH 配置
nano /etc/ssh/sshd_config

# 修改端口（例如改为 2222）
Port 2222

# 重启 SSH
systemctl restart sshd

# 更新防火墙
ufw allow 2222/tcp
ufw delete allow 22/tcp
```

### 2. 禁用密码登录

```bash
# 先上传 SSH 公钥
ssh-copy-id -p 2222 root@YOUR_SERVER_IP

# 禁用密码登录
nano /etc/ssh/sshd_config
# 设置: PasswordAuthentication no

# 重启 SSH
systemctl restart sshd
```

### 3. 安装 Fail2Ban

```bash
apt install -y fail2ban
systemctl enable fail2ban
systemctl start fail2ban
```

### 4. 定期备份配置

```bash
# 备份配置
cp /etc/veildeploy/config.json /root/config-backup-$(date +%Y%m%d).json

# 备份凭据
cp /root/veildeploy-credentials.txt /root/credentials-backup.txt
```

---

## 📊 性能优化

部署脚本已自动启用以下优化：

- ✅ **BBR TCP 拥塞控制** - 提升网络性能
- ✅ **TCP Fast Open** - 减少连接延迟
- ✅ **网络缓冲区优化** - 提高吞吐量
- ✅ **文件描述符限制提升** - 支持更多连接

查看当前配置：
```bash
sysctl net.ipv4.tcp_congestion_control
sysctl net.core.default_qdisc
```

---

## 📞 获取帮助

- **GitHub Issues**: https://github.com/veilconnect/VeilDeploy/issues
- **文档**: https://github.com/veilconnect/VeilDeploy/tree/main/docs
- **部署日志**: 服务器上的 `/root/veildeploy-credentials.txt`

---

## ✅ 完成检查清单

部署完成后，确认以下项目：

- [ ] 服务器部署脚本执行成功
- [ ] 服务状态显示 `active (running)`
- [ ] UDP 51820 端口正在监听
- [ ] 凭据文件已保存
- [ ] 防火墙规则已配置
- [ ] 客户端配置文件已创建
- [ ] 客户端成功连接到服务器

---

## 🎉 就是这么简单！

VeilDeploy 的一键部署让 VPN 服务器搭建变得超级简单：

1. ⚡ **快速** - 2-3 分钟完成
2. 🔒 **安全** - 自动生成强密码
3. 🎯 **简单** - 一条命令搞定
4. 📦 **完整** - 包含所有优化
5. 🔧 **可靠** - 自动验证和启动

开始使用吧！

---

*最后更新: 2025-10-01*
*版本: VeilDeploy 2.0*
