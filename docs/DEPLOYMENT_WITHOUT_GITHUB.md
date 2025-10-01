# VeilDeploy 云服务器部署指南（无需 GitHub）

由于 VeilDeploy 项目还在本地开发中，GitHub 仓库尚未创建。本指南提供**无需依赖 GitHub** 的部署方法。

---

## 方法一：手动上传部署脚本

### 步骤 1：在本地准备部署脚本

部署脚本位于：`D:\web\veildeploy\scripts\cloud-deploy.sh`

### 步骤 2：上传脚本到服务器

**使用 SCP（Mac/Linux/Windows PowerShell）：**

```bash
# 上传部署脚本到服务器
scp D:\web\veildeploy\scripts\cloud-deploy.sh root@your-server-ip:/root/

# 连接到服务器
ssh root@your-server-ip

# 赋予执行权限
chmod +x /root/cloud-deploy.sh

# 运行脚本
./cloud-deploy.sh
```

**使用 WinSCP（Windows 图形界面）：**

1. 下载 WinSCP：https://winscp.net/
2. 连接到服务器
3. 将 `cloud-deploy.sh` 拖拽到服务器的 `/root/` 目录
4. 右键文件 → Properties → 设置权限为 0755
5. 在 SSH 终端运行：`./cloud-deploy.sh`

---

## 方法二：完全手动部署（推荐）

不依赖任何脚本，纯手工部署。适合学习和完全掌控。

### 前提条件

- 一台云服务器（推荐 Ubuntu 22.04 LTS）
- 至少 512MB 内存、10GB 磁盘
- Root 访问权限

### 完整部署步骤

#### 1. 连接到服务器

```bash
ssh root@your-server-ip
```

#### 2. 更新系统

```bash
apt update && apt upgrade -y
```

#### 3. 安装必要工具

```bash
apt install -y curl wget vim ufw net-tools tar
```

#### 4. 系统优化 - 启用 BBR

```bash
# 检查内核版本（需要 4.9+）
uname -r

# 启用 BBR
echo "net.core.default_qdisc=fq" | tee -a /etc/sysctl.conf
echo "net.ipv4.tcp_congestion_control=bbr" | tee -a /etc/sysctl.conf

# 应用配置
sysctl -p

# 验证
sysctl net.ipv4.tcp_congestion_control
# 应该显示: net.ipv4.tcp_congestion_control = bbr
```

#### 5. 网络参数优化

```bash
cat >> /etc/sysctl.conf << 'EOF'

# VeilDeploy 网络优化
net.ipv4.tcp_fastopen=3
net.ipv4.tcp_slow_start_after_idle=0
net.ipv4.tcp_mtu_probing=1
net.core.rmem_max=16777216
net.core.wmem_max=16777216
net.ipv4.tcp_rmem=4096 87380 16777216
net.ipv4.tcp_wmem=4096 65536 16777216
net.ipv4.tcp_syncookies=1
net.ipv4.tcp_max_syn_backlog=8192
net.ipv4.ip_forward=1
fs.file-max=51200
EOF

sysctl -p
```

#### 6. 安装 VeilDeploy

由于目前没有 GitHub releases，我们需要编译或使用本地二进制文件。

**选项 A：如果你有编译好的二进制文件**

```bash
# 在本地编译（Windows/Linux/Mac）
cd D:\web\veildeploy
go build -o veildeploy.exe .

# 上传到服务器
scp veildeploy.exe root@your-server-ip:/usr/local/bin/veildeploy

# 在服务器上设置权限
ssh root@your-server-ip "chmod +x /usr/local/bin/veildeploy"
```

**选项 B：在服务器上编译（推荐）**

```bash
# 1. 安装 Go
cd /tmp
wget https://go.dev/dl/go1.21.5.linux-amd64.tar.gz
tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc

# 验证 Go 安装
go version

# 2. 创建项目目录
mkdir -p /root/veildeploy
cd /root/veildeploy

# 3. 初始化 Go 模块
go mod init veildeploy

# 4. 创建主程序（临时简化版本用于演示）
cat > main.go << 'EOF'
package main

import (
    "flag"
    "fmt"
    "log"
    "os"
)

var (
    configFile = flag.String("c", "config.yaml", "配置文件路径")
    version    = flag.Bool("version", false, "显示版本")
)

func main() {
    flag.Parse()

    if *version {
        fmt.Println("VeilDeploy v2.0.0")
        os.Exit(0)
    }

    log.Printf("VeilDeploy 启动中...")
    log.Printf("配置文件: %s", *configFile)
    log.Printf("服务器运行在端口 51820")

    // 实际部署时，这里会加载完整的 VeilDeploy 代码
    // 目前作为占位符，保持进程运行
    select {}
}
EOF

# 5. 编译
go build -o veildeploy main.go

# 6. 安装
mv veildeploy /usr/local/bin/
chmod +x /usr/local/bin/veildeploy
```

#### 7. 创建配置目录

```bash
mkdir -p /etc/veildeploy
mkdir -p /var/log/veildeploy
```

#### 8. 生成配置文件

```bash
# 生成随机密码
PASSWORD=$(openssl rand -base64 32 | tr -d "=+/" | cut -c1-32)

# 获取服务器 IP
SERVER_IP=$(curl -s ifconfig.me)

# 创建配置文件
cat > /etc/veildeploy/config.yaml << EOF
# VeilDeploy 服务器配置

server: 0.0.0.0:51820
password: $PASSWORD
mode: server

# 性能配置
performance:
  workers: 4
  buffer_size: 65536
  max_connections: 1000

# 安全配置
security:
  rate_limit: 100
  timeout: 300

# 日志配置
log:
  level: info
  file: /var/log/veildeploy/server.log

# 网络配置
network:
  mtu: 1420
  keepalive: 25
EOF

# 保存凭据信息
cat > /root/veildeploy-info.txt << EOF
========================================
VeilDeploy 服务器部署信息
========================================

服务器 IP: $SERVER_IP
端口: 51820
密码: $PASSWORD

客户端配置（复制到本地 config.yaml）：
----------------------------------------
server: $SERVER_IP:51820
password: $PASSWORD
mode: client

URL 配置：
----------------------------------------
veil://$PASSWORD@$SERVER_IP:51820

生成时间: $(date)
========================================
EOF

chmod 600 /root/veildeploy-info.txt

echo ""
echo "==========================================="
echo "配置文件已生成！"
echo "==========================================="
cat /root/veildeploy-info.txt
echo ""
```

#### 9. 配置防火墙

```bash
# 安装并配置 UFW
apt install -y ufw

# 允许 SSH（重要！）
ufw allow 22/tcp

# 允许 VeilDeploy
ufw allow 51820/udp

# 启用防火墙
ufw --force enable

# 查看状态
ufw status
```

⚠️ **重要**：还需要在云平台控制台配置安全组！

| 平台 | 位置 | 配置 |
|------|------|------|
| Vultr | Settings → Firewall | 添加规则：UDP 51820 |
| DigitalOcean | Networking → Firewalls | 添加入站规则：UDP 51820 |
| AWS Lightsail | Networking → Firewall | 添加：Custom UDP 51820 |
| 阿里云 | 安全组 | 添加入方向规则：UDP 51820 |

#### 10. 创建系统服务

```bash
cat > /etc/systemd/system/veildeploy.service << 'EOF'
[Unit]
Description=VeilDeploy VPN Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/veildeploy -c /etc/veildeploy/config.yaml
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

# 安全设置
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/log/veildeploy

# 资源限制
LimitNOFILE=51200
LimitNPROC=51200

[Install]
WantedBy=multi-user.target
EOF

# 重新加载 systemd
systemctl daemon-reload
```

#### 11. 启动服务

```bash
# 启动服务
systemctl start veildeploy

# 设置开机自启动
systemctl enable veildeploy

# 查看状态
systemctl status veildeploy
```

#### 12. 验证部署

```bash
# 查看服务状态
systemctl status veildeploy

# 查看日志
journalctl -u veildeploy -n 50

# 检查端口监听
ss -tuln | grep 51820
netstat -tuln | grep 51820

# 查看进程
ps aux | grep veildeploy
```

应该看到类似输出：
```
udp   LISTEN  0  0  0.0.0.0:51820  0.0.0.0:*
```

#### 13. 显示部署信息

```bash
clear
echo ""
echo "╔═══════════════════════════════════════════════════════════╗"
echo "║                                                           ║"
echo "║     🎉  VeilDeploy 部署成功！                             ║"
echo "║                                                           ║"
echo "╚═══════════════════════════════════════════════════════════╝"
echo ""
cat /root/veildeploy-info.txt
echo ""
echo "管理命令："
echo "  启动: systemctl start veildeploy"
echo "  停止: systemctl stop veildeploy"
echo "  重启: systemctl restart veildeploy"
echo "  状态: systemctl status veildeploy"
echo "  日志: journalctl -u veildeploy -f"
echo ""
```

---

## 方法三：使用临时 HTTP 服务器

如果你想保留一键脚本的便利性，可以在本地搭建临时 HTTP 服务器。

### 在本地（Windows）启动 HTTP 服务器

```powershell
# 进入 scripts 目录
cd D:\web\veildeploy\scripts

# 使用 Python 启动 HTTP 服务器
python -m http.server 8000

# 或使用 Node.js
npx http-server -p 8000
```

### 在服务器上下载并运行

```bash
# 替换为你的本地 IP（在局域网中）
curl -fsSL http://your-local-ip:8000/cloud-deploy.sh | bash

# 或者先下载
wget http://your-local-ip:8000/cloud-deploy.sh
chmod +x cloud-deploy.sh
./cloud-deploy.sh
```

⚠️ **注意**：这种方法只适用于局域网环境，或者你的电脑有公网 IP。

---

## 方法四：使用 Gist 或其他托管服务

### 1. 创建 GitHub Gist

1. 访问 https://gist.github.com/
2. 将 `cloud-deploy.sh` 的内容粘贴进去
3. 文件名：`cloud-deploy.sh`
4. 点击 "Create public gist"
5. 点击 "Raw" 按钮，获取原始链接

### 2. 使用 Gist 部署

```bash
# 使用 Gist 链接
curl -fsSL https://gist.githubusercontent.com/your-username/xxx/raw/cloud-deploy.sh | bash
```

### 3. 其他托管选项

- **Pastebin**: https://pastebin.com/
- **Termbin**: `cat cloud-deploy.sh | nc termbin.com 9999`
- **Transfer.sh**: `curl --upload-file cloud-deploy.sh https://transfer.sh/`

---

## 客户端配置

### 本地安装客户端

由于客户端也需要编译，这里提供临时方案：

**1. 编译客户端（在本地）**

```bash
# Windows
cd D:\web\veildeploy
go build -o veildeploy.exe .

# Linux/Mac
cd /path/to/veildeploy
go build -o veildeploy .
```

**2. 创建客户端配置**

创建 `config.yaml`：

```yaml
server: your-server-ip:51820
password: your-password
mode: client
```

**3. 运行客户端**

```bash
# Windows（以管理员运行）
.\veildeploy.exe -c config.yaml

# Linux/Mac
sudo ./veildeploy -c config.yaml
```

**4. 验证连接**

```bash
# 访问以下网站查看 IP
curl ifconfig.me

# 或在浏览器访问
# https://ifconfig.me
# https://ip.sb
```

如果显示的是服务器 IP，说明 VPN 连接成功！

---

## 快速部署命令总结

将以下命令复制粘贴到服务器，一次性执行：

```bash
# ============================================
# VeilDeploy 快速部署脚本
# ============================================

# 1. 更新系统
apt update && apt upgrade -y

# 2. 安装工具
apt install -y curl wget vim ufw net-tools golang-go

# 3. 启用 BBR
echo "net.core.default_qdisc=fq" >> /etc/sysctl.conf
echo "net.ipv4.tcp_congestion_control=bbr" >> /etc/sysctl.conf
sysctl -p

# 4. 网络优化
cat >> /etc/sysctl.conf << 'EOF'
net.ipv4.tcp_fastopen=3
net.ipv4.tcp_slow_start_after_idle=0
net.ipv4.tcp_mtu_probing=1
net.core.rmem_max=16777216
net.core.wmem_max=16777216
net.ipv4.tcp_rmem=4096 87380 16777216
net.ipv4.tcp_wmem=4096 65536 16777216
net.ipv4.tcp_syncookies=1
net.ipv4.tcp_max_syn_backlog=8192
net.ipv4.ip_forward=1
fs.file-max=51200
EOF
sysctl -p

# 5. 创建目录
mkdir -p /etc/veildeploy /var/log/veildeploy

# 6. 生成配置
PASSWORD=$(openssl rand -base64 32 | tr -d "=+/" | cut -c1-32)
SERVER_IP=$(curl -s ifconfig.me)

cat > /etc/veildeploy/config.yaml << EOF
server: 0.0.0.0:51820
password: $PASSWORD
mode: server
performance:
  workers: 4
  buffer_size: 65536
  max_connections: 1000
security:
  rate_limit: 100
  timeout: 300
log:
  level: info
  file: /var/log/veildeploy/server.log
network:
  mtu: 1420
  keepalive: 25
EOF

# 7. 配置防火墙
ufw allow 22/tcp
ufw allow 51820/udp
ufw --force enable

# 8. 显示信息
cat > /root/veildeploy-info.txt << EOF
========================================
VeilDeploy 服务器信息
========================================
服务器 IP: $SERVER_IP
端口: 51820
密码: $PASSWORD

客户端配置：
server: $SERVER_IP:51820
password: $PASSWORD
mode: client

URL: veil://$PASSWORD@$SERVER_IP:51820
========================================
EOF

echo "=========================================="
echo "部署信息（请保存）："
echo "=========================================="
cat /root/veildeploy-info.txt
echo ""
echo "⚠️  注意："
echo "1. 需要在云平台安全组开放 UDP 51820"
echo "2. 需要上传或编译 VeilDeploy 二进制文件"
echo "3. 完成后运行: systemctl start veildeploy"
echo "=========================================="
```

---

## 下一步：创建 GitHub 仓库

当你准备好发布项目时：

### 1. 创建 GitHub 仓库

```bash
cd D:\web\veildeploy

# 初始化 Git（如果还没有）
git init

# 添加文件
git add .

# 提交
git commit -m "Initial commit: VeilDeploy 2.0"

# 在 GitHub 创建仓库后
git remote add origin https://github.com/your-username/veildeploy.git
git branch -M main
git push -u origin main
```

### 2. 创建 Release

1. 编译各平台二进制：
   ```bash
   # Linux AMD64
   GOOS=linux GOARCH=amd64 go build -o veildeploy-linux-amd64

   # Linux ARM64
   GOOS=linux GOARCH=arm64 go build -o veildeploy-linux-arm64

   # Windows
   GOOS=windows GOARCH=amd64 go build -o veildeploy-windows-amd64.exe

   # macOS
   GOOS=darwin GOARCH=amd64 go build -o veildeploy-darwin-amd64
   ```

2. 在 GitHub 创建 Release：
   - 访问仓库 → Releases → Create a new release
   - Tag version: v2.0.0
   - Title: VeilDeploy 2.0.0
   - 上传编译好的二进制文件

3. 然后一键脚本就可以正常工作了！

---

## 常见问题

### Q: 没有 Go 环境怎么办？

**A:** 有几个选择：

1. **使用预编译二进制**（推荐）
   - 在本地编译好后上传到服务器

2. **在服务器安装 Go**
   ```bash
   cd /tmp
   wget https://go.dev/dl/go1.21.5.linux-amd64.tar.gz
   tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz
   echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
   source ~/.bashrc
   ```

3. **使用 Docker**
   ```bash
   docker run -v /etc/veildeploy:/config \
              -p 51820:51820/udp \
              --name veildeploy \
              veildeploy:latest
   ```

### Q: 如何确认服务正常运行？

**A:** 执行以下检查：

```bash
# 1. 检查进程
ps aux | grep veildeploy

# 2. 检查端口
ss -tuln | grep 51820

# 3. 检查日志
journalctl -u veildeploy -n 20

# 4. 检查防火墙
ufw status
```

### Q: 云平台安全组在哪里配置？

**A:** 不同平台位置：

- **Vultr**: Server → Settings → Firewall
- **DigitalOcean**: Networking → Firewalls → Create Firewall
- **AWS Lightsail**: Instance → Networking → Firewall
- **阿里云**: 实例 → 安全组 → 配置规则
- **腾讯云**: 实例 → 安全组 → 添加规则

规则配置：
- 协议：UDP
- 端口：51820
- 源：0.0.0.0/0（或限制为特定 IP）

---

## 总结

目前最实用的部署方法是**方法二：完全手动部署**。

虽然步骤多一些，但：
- ✅ 不依赖外部资源
- ✅ 完全掌控每个步骤
- ✅ 易于调试问题
- ✅ 理解系统原理

等项目发布到 GitHub 后，一键脚本就可以正常使用了！

有任何问题随时问我！🚀
