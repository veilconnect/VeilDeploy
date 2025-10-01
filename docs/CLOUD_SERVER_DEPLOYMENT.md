# VeilDeploy 云服务器部署完整教程

本教程将详细指导你如何在云服务器上部署 VeilDeploy VPN 节点，从零开始，无需任何技术背景。

---

## 目录

- [方法一：一键部署（推荐）](#方法一一键部署推荐)
- [方法二：手动部署（学习用）](#方法二手动部署学习用)
- [云平台选择指南](#云平台选择指南)
- [平台详细教程](#平台详细教程)
- [安全加固](#安全加固)
- [性能优化](#性能优化)
- [常见问题](#常见问题)

---

## 方法一：一键部署（推荐）

### 步骤 1：购买云服务器

推荐配置（适合个人使用）：
- **CPU**: 1 核或 2 核
- **内存**: 1GB
- **存储**: 20GB SSD
- **带宽**: 1-5 Mbps
- **操作系统**: Ubuntu 22.04 LTS

### 步骤 2：连接到服务器

获取服务器 IP 地址后，使用 SSH 连接：

**Windows 用户：**
```bash
# 使用 PowerShell 或 CMD
ssh root@your-server-ip
```

**Mac/Linux 用户：**
```bash
ssh root@your-server-ip
```

输入密码后即可登录。

### 步骤 3：运行一键部署脚本

登录服务器后，复制并运行以下命令：

```bash
curl -fsSL https://raw.githubusercontent.com/veildeploy/veildeploy/main/scripts/cloud-deploy.sh | bash
```

或者如果上述链接无法访问：

```bash
# 1. 下载脚本
wget https://raw.githubusercontent.com/veildeploy/veildeploy/main/scripts/cloud-deploy.sh

# 2. 赋予执行权限
chmod +x cloud-deploy.sh

# 3. 运行脚本
./cloud-deploy.sh
```

### 步骤 4：等待安装完成

脚本会自动完成以下任务（需要 3-5 分钟）：

- ✅ 更新系统
- ✅ 优化网络性能（启用 BBR）
- ✅ 安装 VeilDeploy
- ✅ 生成安全配置
- ✅ 配置防火墙
- ✅ 创建系统服务
- ✅ 启动服务
- ✅ 验证部署

### 步骤 5：获取连接信息

安装完成后，屏幕会显示：

```
=================================
VeilDeploy 服务器信息
=================================

服务器地址: 123.45.67.89:51820
密码: Abc123xyz789...
生成时间: 2025-10-01 10:00:00

客户端配置:
---------------------------------
server: 123.45.67.89:51820
password: Abc123xyz789...
mode: client

URL 配置:
---------------------------------
veil://Abc123xyz789...@123.45.67.89:51820
```

**请复制保存这些信息！**

凭据信息也保存在服务器的 `/root/veildeploy-credentials.txt` 文件中。

### 步骤 6：在本地安装客户端

**Windows:**
```powershell
# 以管理员身份运行 PowerShell
iwr -useb https://get.veildeploy.com/install.ps1 | iex
```

**Mac/Linux:**
```bash
curl -fsSL https://get.veildeploy.com | bash
```

### 步骤 7：配置客户端

创建配置文件 `config.yaml`：

```yaml
server: 123.45.67.89:51820
password: Abc123xyz789...
mode: client
```

替换为你自己的服务器 IP 和密码。

### 步骤 8：启动客户端

```bash
# Mac/Linux
sudo veildeploy -c config.yaml

# Windows（以管理员运行）
veildeploy.exe -c config.yaml
```

### 步骤 9：验证连接

打开浏览器访问：https://ifconfig.me

如果显示的是你的服务器 IP，说明 VPN 已成功连接！

---

## 方法二：手动部署（学习用）

如果你想理解部署过程的每一步，可以按照以下手动步骤操作。

### 1. 准备工作

**1.1 连接到服务器**

```bash
ssh root@your-server-ip
```

**1.2 更新系统**

```bash
# Ubuntu/Debian
apt update && apt upgrade -y

# CentOS/RHEL
yum update -y
```

**1.3 安装必要工具**

```bash
# Ubuntu/Debian
apt install -y curl wget vim ufw net-tools

# CentOS/RHEL
yum install -y curl wget vim firewalld net-tools
```

### 2. 系统优化

**2.1 启用 BBR（Google TCP 拥塞控制算法）**

BBR 可以显著提升网络性能，特别是在高延迟或丢包网络中。

```bash
# 检查内核版本（需要 4.9+）
uname -r

# 启用 BBR
echo "net.core.default_qdisc=fq" >> /etc/sysctl.conf
echo "net.ipv4.tcp_congestion_control=bbr" >> /etc/sysctl.conf

# 应用配置
sysctl -p

# 验证 BBR 是否启用
sysctl net.ipv4.tcp_congestion_control
# 应该显示: net.ipv4.tcp_congestion_control = bbr
```

**2.2 网络参数优化**

```bash
cat >> /etc/sysctl.conf << EOF

# VeilDeploy 网络优化
net.ipv4.tcp_fastopen=3
net.ipv4.tcp_slow_start_after_idle=0
net.ipv4.tcp_mtu_probing=1

# 缓冲区大小
net.core.rmem_max=16777216
net.core.wmem_max=16777216
net.ipv4.tcp_rmem=4096 87380 16777216
net.ipv4.tcp_wmem=4096 65536 16777216

# 安全设置
net.ipv4.tcp_syncookies=1
net.ipv4.tcp_max_syn_backlog=8192

# 启用 IP 转发
net.ipv4.ip_forward=1

# 文件描述符限制
fs.file-max=51200
EOF

# 应用配置
sysctl -p
```

**2.3 增加文件描述符限制**

```bash
cat >> /etc/security/limits.conf << EOF
* soft nofile 51200
* hard nofile 51200
EOF
```

### 3. 安装 VeilDeploy

**3.1 下载二进制文件**

```bash
# 进入临时目录
cd /tmp

# 检测系统架构
ARCH=$(uname -m)
case $ARCH in
    x86_64) ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
esac

# 下载最新版本
wget https://github.com/veildeploy/veildeploy/releases/latest/download/veildeploy-linux-${ARCH}.tar.gz

# 解压
tar -xzf veildeploy-linux-${ARCH}.tar.gz

# 移动到系统路径
mv veildeploy /usr/local/bin/
chmod +x /usr/local/bin/veildeploy

# 验证安装
veildeploy --version
```

**3.2 创建必要目录**

```bash
mkdir -p /etc/veildeploy
mkdir -p /var/log/veildeploy
```

### 4. 配置 VeilDeploy

**4.1 生成安全密码**

```bash
# 生成 32 字符随机密码
PASSWORD=$(openssl rand -base64 32 | tr -d "=+/" | cut -c1-32)
echo "生成的密码: $PASSWORD"

# 保存密码（重要！）
echo "$PASSWORD" > /root/veildeploy-password.txt
chmod 600 /root/veildeploy-password.txt
```

**4.2 获取服务器 IP**

```bash
SERVER_IP=$(curl -s ifconfig.me)
echo "服务器 IP: $SERVER_IP"
```

**4.3 创建配置文件**

```bash
cat > /etc/veildeploy/config.yaml << EOF
# VeilDeploy 服务器配置

server: 0.0.0.0:51820
password: $PASSWORD
mode: server

# 性能配置
performance:
  workers: 4                    # 工作线程数（建议 = CPU 核心数）
  buffer_size: 65536            # 缓冲区大小
  max_connections: 1000         # 最大连接数

# 安全配置
security:
  rate_limit: 100               # 每秒速率限制
  timeout: 300                  # 连接超时（秒）

# 日志配置
log:
  level: info                   # 日志级别: debug/info/warn/error
  file: /var/log/veildeploy/server.log

# 网络配置
network:
  mtu: 1420                     # MTU 大小
  keepalive: 25                 # 保持活动间隔（秒）
EOF
```

**4.4 保存客户端配置信息**

```bash
cat > /root/veildeploy-client-config.yaml << EOF
# 客户端配置
server: $SERVER_IP:51820
password: $PASSWORD
mode: client
EOF

echo ""
echo "========================================"
echo "客户端配置信息已保存到:"
echo "/root/veildeploy-client-config.yaml"
echo "========================================"
echo ""
cat /root/veildeploy-client-config.yaml
echo ""
```

### 5. 配置防火墙

**5.1 UFW（Ubuntu/Debian）**

```bash
# 检查 UFW 是否安装
if command -v ufw >/dev/null 2>&1; then
    # 允许 SSH（重要！否则会锁死）
    ufw allow 22/tcp

    # 允许 VeilDeploy
    ufw allow 51820/udp

    # 启用防火墙
    ufw --force enable

    # 查看状态
    ufw status
fi
```

**5.2 firewalld（CentOS/RHEL）**

```bash
# 启动 firewalld
systemctl start firewalld
systemctl enable firewalld

# 允许 VeilDeploy
firewall-cmd --permanent --add-port=51820/udp
firewall-cmd --reload

# 查看状态
firewall-cmd --list-all
```

**5.3 云平台安全组**

⚠️ **重要**：还需要在云平台控制台配置安全组规则！

允许入站规则：
- **类型**: UDP
- **端口**: 51820
- **源**: 0.0.0.0/0（所有 IP）

### 6. 创建系统服务

**6.1 创建 systemd 服务文件**

```bash
cat > /etc/systemd/system/veildeploy.service << EOF
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
```

**6.2 启动服务**

```bash
# 重新加载 systemd
systemctl daemon-reload

# 启动服务
systemctl start veildeploy

# 设置开机自启动
systemctl enable veildeploy

# 查看服务状态
systemctl status veildeploy
```

### 7. 验证部署

**7.1 检查服务状态**

```bash
# 查看服务状态
systemctl status veildeploy

# 查看实时日志
journalctl -u veildeploy -f
```

**7.2 检查端口监听**

```bash
# 使用 netstat
netstat -tuln | grep 51820

# 或使用 ss
ss -tuln | grep 51820
```

应该看到类似输出：
```
udp   LISTEN  0  0  0.0.0.0:51820  0.0.0.0:*
```

**7.3 检查进程**

```bash
ps aux | grep veildeploy
```

### 8. 测试连接

**8.1 在本地安装客户端**

参考前面"方法一"中的步骤 6-9。

**8.2 常用管理命令**

```bash
# 启动服务
systemctl start veildeploy

# 停止服务
systemctl stop veildeploy

# 重启服务
systemctl restart veildeploy

# 查看状态
systemctl status veildeploy

# 查看日志
journalctl -u veildeploy -n 100        # 最近 100 行
journalctl -u veildeploy -f            # 实时跟踪
journalctl -u veildeploy --since today # 今天的日志
```

---

## 云平台选择指南

### 推荐平台对比

| 平台 | 价格 | 带宽/流量 | 优点 | 缺点 | 推荐度 |
|------|------|-----------|------|------|--------|
| **Vultr** | $5/月 | 1TB 流量 | 便宜，全球节点多 | 偶尔丢包 | ⭐⭐⭐⭐⭐ |
| **DigitalOcean** | $6/月 | 1TB 流量 | 稳定，文档好 | 略贵 | ⭐⭐⭐⭐⭐ |
| **AWS Lightsail** | $5/月 | 2TB 流量 | 流量多，整合好 | 配置复杂 | ⭐⭐⭐⭐ |
| **Google Cloud** | $10/月 | 1TB 流量 | 性能强 | 较贵 | ⭐⭐⭐⭐ |
| **阿里云** | ¥30/月 | 1-5 Mbps | 国内访问快 | 需备案 | ⭐⭐⭐ |
| **腾讯云** | ¥30/月 | 1-5 Mbps | 国内访问快 | 需备案 | ⭐⭐⭐ |

### 选择建议

**个人使用（1-3 人）：**
- 推荐：Vultr 或 DigitalOcean
- 配置：1 vCPU, 1GB RAM, 25GB SSD
- 价格：$5-6/月

**家庭使用（3-5 人）：**
- 推荐：DigitalOcean 或 AWS Lightsail
- 配置：2 vCPU, 2GB RAM, 50GB SSD
- 价格：$12-15/月

**小团队使用（5-20 人）：**
- 推荐：AWS 或 Google Cloud
- 配置：4 vCPU, 8GB RAM, 100GB SSD
- 价格：$40-60/月

**地区选择：**
- 访问美国网站：选美国西海岸（洛杉矶/旧金山）
- 访问欧洲网站：选英国/德国
- 亚洲用户：选日本/新加坡/香港
- 追求速度：选择离你最近的地区

---

## 平台详细教程

### Vultr 部署教程（推荐新手）

#### 步骤 1：注册账号

1. 访问 https://www.vultr.com
2. 点击右上角 "Sign Up"
3. 填写邮箱和密码
4. 验证邮箱

#### 步骤 2：充值

1. 登录后点击 "Billing"
2. 选择支付方式（支持信用卡、PayPal、支付宝）
3. 充值 $10（建议）

#### 步骤 3：创建服务器

1. 点击左侧 "Products"
2. 点击蓝色按钮 "Deploy New Server"
3. 配置选择：

**Choose Server:**
- 选择 "Cloud Compute" → "Regular Performance"

**Server Location:**
- 推荐选择：
  - 亚洲用户：Tokyo, Japan（东京）或 Singapore（新加坡）
  - 美国用户：Los Angeles（洛杉矶）
  - 欧洲用户：London（伦敦）

**Server Image:**
- 选择 "Ubuntu 22.04 LTS x64"

**Server Size:**
- 选择 "$5/mo" 套餐（1 vCPU, 1024 MB RAM, 25 GB SSD, 1 TB Bandwidth）

**Additional Features:**
- 可选：勾选 "Enable Auto Backups"（$1/月，建议勾选）
- 可选：勾选 "Enable IPv6"（免费）

**Server Hostname & Label:**
- 填写一个好记的名字，如 "veildeploy-server"

4. 点击 "Deploy Now"

#### 步骤 4：等待服务器创建

等待 1-2 分钟，状态变为 "Running"

#### 步骤 5：获取连接信息

1. 点击服务器名称进入详情页
2. 记录以下信息：
   - **IP Address**: 服务器 IP（如 123.45.67.89）
   - **Username**: root
   - **Password**: 点击眼睛图标查看

#### 步骤 6：配置防火墙

1. 在服务器详情页点击 "Settings"
2. 点击 "Firewall"
3. 点击 "Add Firewall Group"
4. 添加规则：
   - **Rule 1**: Protocol=SSH, Port=22, Source=Anywhere
   - **Rule 2**: Protocol=UDP, Port=51820, Source=Anywhere
5. 点击 "Link Firewall Group"

#### 步骤 7：连接并部署

```bash
# 连接到服务器
ssh root@your-server-ip

# 运行一键部署脚本
curl -fsSL https://raw.githubusercontent.com/veildeploy/veildeploy/main/scripts/cloud-deploy.sh | bash
```

完成！

### DigitalOcean 部署教程

#### 步骤 1：注册账号

1. 访问 https://www.digitalocean.com
2. 点击 "Sign Up"
3. 使用 GitHub/Google 账号快速注册
4. 新用户可获得 $200 免费额度（60 天有效）

#### 步骤 2：创建 Droplet

1. 点击顶部 "Create" → "Droplets"
2. 配置选择：

**Choose an image:**
- 选择 "Ubuntu 22.04 (LTS) x64"

**Choose a plan:**
- 选择 "Basic"
- CPU options: "Regular"
- 选择 "$6/mo" 套餐（1 GB RAM, 1 vCPU, 25 GB SSD, 1000 GB transfer）

**Choose a datacenter region:**
- 推荐：
  - 亚洲：Singapore（新加坡）
  - 美国：San Francisco（旧金山）或 New York（纽约）
  - 欧洲：London（伦敦）或 Frankfurt（法兰克福）

**Authentication:**
- 选择 "Password" 或 "SSH keys"（推荐 SSH keys 更安全）

**Hostname:**
- 填写：veildeploy-server

3. 点击 "Create Droplet"

#### 步骤 3：配置防火墙

1. 左侧菜单点击 "Networking"
2. 选择 "Firewalls" 标签
3. 点击 "Create Firewall"
4. 添加规则：

**Inbound Rules:**
- SSH: TCP, Port 22, All IPv4/IPv6
- Custom: UDP, Port 51820, All IPv4/IPv6

**Outbound Rules:**
- All TCP, All UDP, All ICMP（保持默认）

5. 在 "Apply to Droplets" 选择你的服务器
6. 点击 "Create Firewall"

#### 步骤 4：连接并部署

```bash
# 连接到服务器
ssh root@your-droplet-ip

# 运行一键部署脚本
curl -fsSL https://raw.githubusercontent.com/veildeploy/veildeploy/main/scripts/cloud-deploy.sh | bash
```

### AWS Lightsail 部署教程

#### 步骤 1：登录 AWS

1. 访问 https://lightsail.aws.amazon.com
2. 登录你的 AWS 账号（或注册新账号）

#### 步骤 2：创建实例

1. 点击 "Create instance"
2. 配置选择：

**Instance location:**
- 选择离你最近的区域

**Pick your instance image:**
- Platform: Linux/Unix
- Blueprint: OS Only → Ubuntu 22.04 LTS

**Choose your instance plan:**
- 选择 $5/month 套餐（512 MB RAM, 1 vCPU, 20 GB SSD, 1 TB transfer）
- 或 $10/month 套餐（1 GB RAM, 1 vCPU, 40 GB SSD, 2 TB transfer）

**Identify your instance:**
- Name: veildeploy-server

3. 点击 "Create instance"

#### 步骤 3：配置防火墙

1. 点击实例名称进入详情
2. 选择 "Networking" 标签
3. 在 "IPv4 Firewall" 点击 "Add rule"
4. 添加：
   - Application: Custom
   - Protocol: UDP
   - Port: 51820
5. 点击 "Create"

#### 步骤 4：连接并部署

1. 在实例详情页点击 "Connect using SSH"（会打开浏览器终端）
2. 或使用本地 SSH：

```bash
# 下载密钥文件（在实例详情页的 "SSH key" 部分）
chmod 400 LightsailDefaultKey.pem

# 连接
ssh -i LightsailDefaultKey.pem ubuntu@your-instance-ip

# 切换到 root
sudo su -

# 运行部署脚本
curl -fsSL https://raw.githubusercontent.com/veildeploy/veildeploy/main/scripts/cloud-deploy.sh | bash
```

---

## 安全加固

部署完成后，强烈建议进行以下安全加固：

### 1. 修改 SSH 端口

```bash
# 编辑 SSH 配置
nano /etc/ssh/sshd_config

# 找到 #Port 22，修改为：
Port 2222

# 保存退出（Ctrl+X, Y, Enter）

# 防火墙允许新端口
ufw allow 2222/tcp

# 重启 SSH
systemctl restart sshd

# 测试新端口连接（不要关闭当前会话！）
# 打开新终端测试：
ssh -p 2222 root@your-server-ip

# 确认能连接后，删除旧规则
ufw delete allow 22/tcp
```

### 2. 禁用密码登录（使用 SSH 密钥）

**2.1 生成 SSH 密钥（在本地电脑）**

```bash
# Mac/Linux
ssh-keygen -t ed25519 -C "your_email@example.com"

# Windows（PowerShell）
ssh-keygen -t ed25519 -C "your_email@example.com"

# 按回车接受默认路径，设置密码（可选）
```

**2.2 上传公钥到服务器**

```bash
# 方法1：使用 ssh-copy-id（Mac/Linux）
ssh-copy-id -i ~/.ssh/id_ed25519.pub root@your-server-ip

# 方法2：手动复制
# 在本地查看公钥
cat ~/.ssh/id_ed25519.pub

# 在服务器上添加
mkdir -p ~/.ssh
echo "your-public-key-here" >> ~/.ssh/authorized_keys
chmod 700 ~/.ssh
chmod 600 ~/.ssh/authorized_keys
```

**2.3 禁用密码登录**

```bash
# 编辑 SSH 配置
nano /etc/ssh/sshd_config

# 设置以下项：
PasswordAuthentication no
PubkeyAuthentication yes
PermitRootLogin prohibit-password

# 重启 SSH
systemctl restart sshd
```

### 3. 安装 Fail2Ban（防暴力破解）

```bash
# 安装
apt install -y fail2ban

# 配置
cat > /etc/fail2ban/jail.local << EOF
[DEFAULT]
bantime = 3600
findtime = 600
maxretry = 5

[sshd]
enabled = true
port = 2222
logpath = %(sshd_log)s
EOF

# 启动
systemctl start fail2ban
systemctl enable fail2ban

# 查看状态
fail2ban-client status sshd
```

### 4. 配置自动更新

```bash
# 安装
apt install -y unattended-upgrades

# 配置
dpkg-reconfigure -plow unattended-upgrades

# 编辑配置
nano /etc/apt/apt.conf.d/50unattended-upgrades

# 确保启用了：
Unattended-Upgrade::Automatic-Reboot "false";
Unattended-Upgrade::Mail "your-email@example.com";
```

### 5. 设置监控告警

**5.1 安装 Netdata（实时监控）**

```bash
bash <(curl -Ss https://my-netdata.io/kickstart.sh)

# 访问 http://your-server-ip:19999 查看监控面板
```

**5.2 设置邮件告警（可选）**

```bash
# 安装 mailutils
apt install -y mailutils

# 测试发送邮件
echo "Test email" | mail -s "Test" your-email@example.com
```

---

## 性能优化

### 1. 多核 CPU 优化

如果你的服务器有多个 CPU 核心，调整配置：

```bash
# 编辑配置
nano /etc/veildeploy/config.yaml

# 修改 workers 数量（= CPU 核心数）
performance:
  workers: 4    # 如果是 4 核 CPU
```

### 2. MTU 优化

```bash
# 测试最优 MTU
ping -c 5 -M do -s 1400 8.8.8.8

# 如果成功，继续增大
ping -c 5 -M do -s 1450 8.8.8.8
ping -c 5 -M do -s 1472 8.8.8.8

# 找到不会 fragment 的最大值

# 更新配置
nano /etc/veildeploy/config.yaml

network:
  mtu: 1420    # 使用测试得到的值 - 28
```

### 3. 启用 TCP Fast Open

```bash
# 已在系统优化部分配置
# 验证是否启用
sysctl net.ipv4.tcp_fastopen
# 应该显示: net.ipv4.tcp_fastopen = 3
```

### 4. 增加连接数限制

```bash
# 编辑配置
nano /etc/veildeploy/config.yaml

performance:
  max_connections: 2000    # 根据需要调整

security:
  rate_limit: 200          # 每秒新连接限制
```

### 5. 日志轮转

防止日志文件占满磁盘：

```bash
cat > /etc/logrotate.d/veildeploy << EOF
/var/log/veildeploy/*.log {
    daily
    rotate 7
    compress
    delaycompress
    missingok
    notifempty
    create 0640 root root
    postrotate
        systemctl reload veildeploy > /dev/null 2>&1 || true
    endscript
}
EOF
```

---

## 常见问题

### Q1: 无法连接到服务器？

**问题排查：**

```bash
# 1. 检查服务是否运行
systemctl status veildeploy

# 2. 检查端口是否监听
ss -tuln | grep 51820

# 3. 检查防火墙
ufw status

# 4. 检查日志
journalctl -u veildeploy -n 50
```

**常见原因：**
- ❌ 云平台安全组未开放 UDP 51820
- ❌ 防火墙规则未正确配置
- ❌ 服务未启动或崩溃
- ❌ 客户端密码错误

**解决方法：**

1. **确认云平台安全组：**
   - 登录云平台控制台
   - 找到安全组设置
   - 添加入站规则：UDP 51820，源 0.0.0.0/0

2. **检查本地防火墙：**
   ```bash
   ufw allow 51820/udp
   ufw reload
   ```

3. **重启服务：**
   ```bash
   systemctl restart veildeploy
   journalctl -u veildeploy -f
   ```

### Q2: 连接速度慢？

**优化步骤：**

1. **确认 BBR 已启用：**
   ```bash
   sysctl net.ipv4.tcp_congestion_control
   # 应该显示 bbr
   ```

2. **测试网络延迟：**
   ```bash
   # 在本地电脑测试
   ping your-server-ip
   ```

3. **更换地区：**
   - 如果延迟 >200ms，考虑更换离你更近的服务器地区

4. **检查带宽限制：**
   - 查看云平台是否限制了带宽
   - 考虑升级套餐

5. **优化 MTU：**
   - 参考前面"性能优化"部分

### Q3: 服务经常断开？

**可能原因：**
- NAT 超时
- 服务器重启
- 内存不足

**解决方法：**

1. **调整 keepalive：**
   ```bash
   nano /etc/veildeploy/config.yaml

   network:
     keepalive: 10    # 降低到 10 秒
   ```

2. **检查内存使用：**
   ```bash
   free -h
   # 如果内存不足，考虑升级
   ```

3. **设置自动重连（客户端）：**
   ```yaml
   # 客户端 config.yaml
   network:
     auto_reconnect: true
     reconnect_interval: 5
   ```

### Q4: 日志显示 "permission denied"？

```bash
# 检查日志目录权限
ls -la /var/log/veildeploy

# 修复权限
mkdir -p /var/log/veildeploy
chown root:root /var/log/veildeploy
chmod 755 /var/log/veildeploy

# 重启服务
systemctl restart veildeploy
```

### Q5: 如何备份配置？

```bash
# 备份配置文件
cp /etc/veildeploy/config.yaml /root/veildeploy-backup-$(date +%Y%m%d).yaml

# 备份密钥（如果有）
tar -czf /root/veildeploy-keys-$(date +%Y%m%d).tar.gz /etc/veildeploy/*.key

# 定期备份脚本
cat > /root/backup-veildeploy.sh << 'EOF'
#!/bin/bash
BACKUP_DIR="/root/veildeploy-backups"
mkdir -p $BACKUP_DIR
tar -czf $BACKUP_DIR/backup-$(date +%Y%m%d-%H%M%S).tar.gz \
    /etc/veildeploy \
    /var/log/veildeploy
# 保留最近 30 天的备份
find $BACKUP_DIR -name "backup-*.tar.gz" -mtime +30 -delete
EOF

chmod +x /root/backup-veildeploy.sh

# 设置每天自动备份
(crontab -l 2>/dev/null; echo "0 2 * * * /root/backup-veildeploy.sh") | crontab -
```

### Q6: 如何更新 VeilDeploy？

```bash
# 停止服务
systemctl stop veildeploy

# 备份当前版本
cp /usr/local/bin/veildeploy /usr/local/bin/veildeploy.backup

# 下载最新版本
ARCH=$(uname -m)
case $ARCH in
    x86_64) ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
esac

cd /tmp
wget https://github.com/veildeploy/veildeploy/releases/latest/download/veildeploy-linux-${ARCH}.tar.gz
tar -xzf veildeploy-linux-${ARCH}.tar.gz
mv veildeploy /usr/local/bin/
chmod +x /usr/local/bin/veildeploy

# 启动服务
systemctl start veildeploy

# 验证版本
/usr/local/bin/veildeploy --version

# 检查状态
systemctl status veildeploy
```

### Q7: 如何添加多个用户？

```bash
# 编辑配置文件
nano /etc/veildeploy/config.yaml

# 添加认证配置
auth:
  type: password
  users:
    - username: user1
      password: password1
    - username: user2
      password: password2
    - username: user3
      password: password3

# 重启服务
systemctl restart veildeploy
```

客户端连接时配置：
```yaml
server: your-server-ip:51820
username: user1
password: password1
mode: client
```

### Q8: 如何查看当前连接数？

```bash
# 方法1：查看日志
journalctl -u veildeploy | grep -i "connection"

# 方法2：使用 netstat
netstat -an | grep 51820 | grep ESTABLISHED | wc -l

# 方法3：创建状态查询脚本
cat > /usr/local/bin/veildeploy-status << 'EOF'
#!/bin/bash
echo "==================================="
echo "VeilDeploy 服务器状态"
echo "==================================="
echo ""
echo "服务状态:"
systemctl status veildeploy | grep Active
echo ""
echo "端口监听:"
ss -tuln | grep 51820
echo ""
echo "当前连接数:"
ss -tu | grep 51820 | wc -l
echo ""
echo "内存使用:"
free -h | grep Mem
echo ""
echo "CPU 负载:"
uptime
echo ""
EOF

chmod +x /usr/local/bin/veildeploy-status

# 使用
veildeploy-status
```

### Q9: 服务器被墙怎么办？

如果服务器 IP 被封锁：

1. **更换端口：**
   ```bash
   nano /etc/veildeploy/config.yaml

   server: 0.0.0.0:8443    # 改为常见端口
   ```

2. **启用混淆：**
   ```bash
   nano /etc/veildeploy/config.yaml

   obfuscation:
     enabled: true
     type: tls    # 伪装成 TLS 流量
   ```

3. **使用 CDN（如 Cloudflare）：**
   - 参考 `CLOUD_DEPLOYMENT_GUIDE.md` 中的 CDN 配置

4. **最后手段：更换服务器地区或 IP**

### Q10: 如何监控流量使用？

```bash
# 安装 vnstat
apt install -y vnstat

# 启动服务
systemctl start vnstat
systemctl enable vnstat

# 查看流量统计
vnstat

# 实时监控
vnstat -l

# 查看每日流量
vnstat -d

# 查看每月流量
vnstat -m
```

---

## 总结

通过本教程，你应该已经成功在云服务器上部署了 VeilDeploy VPN 节点。

**快速回顾：**
1. ✅ 选择合适的云平台（推荐 Vultr 或 DigitalOcean）
2. ✅ 创建服务器实例（Ubuntu 22.04, 1GB RAM）
3. ✅ 运行一键部署脚本或手动安装
4. ✅ 配置防火墙和安全组
5. ✅ 在本地安装客户端并连接
6. ✅ 进行安全加固（可选但推荐）
7. ✅ 性能优化（BBR、MTU 等）

**管理命令速查：**
```bash
systemctl start veildeploy      # 启动
systemctl stop veildeploy       # 停止
systemctl restart veildeploy    # 重启
systemctl status veildeploy     # 状态
journalctl -u veildeploy -f     # 日志
```

**重要文件位置：**
- 配置文件：`/etc/veildeploy/config.yaml`
- 日志文件：`/var/log/veildeploy/server.log`
- 凭据信息：`/root/veildeploy-credentials.txt`
- 服务文件：`/etc/systemd/system/veildeploy.service`

**获取帮助：**
- 文档：https://docs.veildeploy.com
- GitHub：https://github.com/veildeploy/veildeploy/issues
- 社区：https://forum.veildeploy.com

祝使用愉快！🚀
