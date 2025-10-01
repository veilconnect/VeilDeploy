# VeilDeploy 云服务器部署完整指南

## 📋 目录

1. [云服务器选择与配置要求](#云服务器选择与配置要求)
2. [主流云平台部署教程](#主流云平台部署教程)
3. [完整部署流程](#完整部署流程)
4. [性能优化建议](#性能优化建议)
5. [安全加固](#安全加固)
6. [成本优化](#成本优化)

---

## ☁️ 云服务器选择与配置要求

### 最低配置要求

| 配置项 | 最低要求 | 推荐配置 | 说明 |
|-------|---------|---------|------|
| **CPU** | 1核 | 2核+ | 2核可支撑100+并发 |
| **内存** | 512MB | 1GB+ | 1GB可支撑200+并发 |
| **存储** | 10GB | 20GB+ | SSD更佳 |
| **带宽** | 1Mbps | 5Mbps+ | 影响速度和并发数 |
| **流量** | 500GB/月 | 1TB/月+ | 取决于用户数量 |
| **操作系统** | Linux | Ubuntu 22.04 | CentOS/Debian也可 |

### 配置建议对照表

| 使用场景 | CPU | 内存 | 带宽 | 月流量 | 预估成本 |
|---------|-----|------|------|--------|---------|
| **个人使用** (1-5人) | 1核 | 1GB | 2Mbps | 500GB | $5-10/月 |
| **小团队** (5-20人) | 2核 | 2GB | 5Mbps | 1TB | $15-30/月 |
| **中型企业** (20-100人) | 4核 | 4GB | 10Mbps | 2TB | $50-100/月 |
| **大型企业** (100+人) | 8核+ | 8GB+ | 20Mbps+ | 5TB+ | $150+/月 |

### 云服务商推荐

#### 1. 适合中国用户的云服务商

| 服务商 | 优点 | 缺点 | 推荐指数 |
|-------|------|------|---------|
| **Vultr** | 价格便宜、日本/新加坡节点快 | 部分IP被墙 | ⭐⭐⭐⭐⭐ |
| **DigitalOcean** | 稳定性好、文档完善 | 价格略贵 | ⭐⭐⭐⭐⭐ |
| **Linode** | 性能优秀、技术支持好 | 价格较高 | ⭐⭐⭐⭐ |
| **AWS Lightsail** | 大厂稳定、全球节点多 | 配置复杂 | ⭐⭐⭐⭐ |
| **Google Cloud** | 性能最好、赠送$300 | 配置复杂、需信用卡 | ⭐⭐⭐⭐ |
| **Bandwagon (搬瓦工)** | 针对中国优化 | 价格较贵、经常缺货 | ⭐⭐⭐⭐ |

#### 2. 推荐机房位置

**按延迟排序（中国用户）**:

1. **香港** - 延迟最低 (20-50ms)，但价格贵，带宽小
2. **日本东京** - 延迟低 (50-100ms)，性价比高 ⭐推荐
3. **新加坡** - 延迟中等 (70-120ms)，稳定性好
4. **韩国首尔** - 延迟低 (50-90ms)，但IP容易被墙
5. **美国西海岸** - 延迟较高 (150-200ms)，但价格便宜
6. **欧洲** - 延迟高 (200-300ms)，不推荐中国用户

**最佳选择**: 日本东京 + 新加坡（备用）

---

## 🚀 主流云平台部署教程

### 方案 1: Vultr 部署（推荐新手）

#### 步骤 1: 购买服务器

1. 访问 [Vultr官网](https://www.vultr.com) 注册账号
2. 充值（支持支付宝/PayPal/信用卡）
3. 点击 "Deploy New Server"
4. 选择配置：

```
Server Type: Cloud Compute
Location: Tokyo, Japan (推荐)
Server Size: $6/month (1 CPU, 1GB RAM, 1TB 流量)
Operating System: Ubuntu 22.04 x64
```

5. 点击 "Deploy Now"，等待3-5分钟

#### 步骤 2: 获取服务器信息

部署完成后，记录以下信息：

```
IP Address: 123.456.789.101
Username: root
Password: YourRandomPassword
```

#### 步骤 3: SSH 连接服务器

**Windows 用户**:
```powershell
# 使用 PowerShell
ssh root@123.456.789.101

# 或使用 PuTTY
# 下载 PuTTY，输入IP，点击连接
```

**Mac/Linux 用户**:
```bash
ssh root@123.456.789.101
```

输入密码登录。

#### 步骤 4: 一键安装 VeilDeploy

```bash
# 1. 更新系统
apt update && apt upgrade -y

# 2. 安装 VeilDeploy
curl -fsSL https://get.veildeploy.com | bash

# 3. 安装过程中选择：
#    - 选择 "1" (服务器模式)
#    - 设置一个强密码
#    - 选择 "y" 安装为系统服务
```

#### 步骤 5: 配置防火墙

```bash
# Ubuntu
ufw allow 51820
ufw allow 22  # SSH端口，必须保留！
ufw enable

# 确认规则
ufw status
```

#### 步骤 6: 测试连接

```bash
# 查看服务状态
systemctl status veildeploy

# 查看日志
journalctl -u veildeploy -f
```

#### 步骤 7: 客户端连接

在本地电脑创建客户端配置：

```yaml
# ~/.veildeploy/config.yaml
server: 123.456.789.101:51820
password: YOUR_SERVER_PASSWORD
mode: auto
```

启动客户端：
```bash
veildeploy client -c ~/.veildeploy/config.yaml
```

**完成！现在可以科学上网了！** 🎉

---

### 方案 2: DigitalOcean 部署

#### 步骤 1: 使用推荐链接注册

访问 [DigitalOcean](https://www.digitalocean.com)（使用推荐链接可获$200试用）

#### 步骤 2: 创建 Droplet

1. 点击 "Create" > "Droplets"
2. 选择配置：

```
Image: Ubuntu 22.04 (LTS) x64
Plan: Basic
CPU Options: Regular (1GB RAM, $6/month)
Datacenter: Singapore or Tokyo
Authentication: SSH Key (推荐) 或 Password
```

3. 高级选项（可选）：
   - ✅ 勾选 "User Data"，粘贴以下内容：

```bash
#!/bin/bash
apt update
apt upgrade -y
curl -fsSL https://get.veildeploy.com | bash
```

4. 点击 "Create Droplet"

#### 步骤 3: 配置 SSH Key（推荐）

**生成 SSH Key**:
```bash
# 在本地电脑执行
ssh-keygen -t ed25519 -C "your_email@example.com"

# 查看公钥
cat ~/.ssh/id_ed25519.pub
```

复制公钥内容，在 DigitalOcean 的 "SSH Keys" 中添加。

#### 步骤 4: 连接并配置

```bash
# SSH连接（使用SSH Key无需密码）
ssh root@YOUR_DROPLET_IP

# 如果使用了User Data，VeilDeploy已自动安装
# 否则手动安装
curl -fsSL https://get.veildeploy.com | bash
```

#### 步骤 5: 配置服务器

```bash
# 编辑配置
nano /etc/veildeploy/config.yaml

# 内容如下
server: 0.0.0.0:51820
password: YOUR_STRONG_PASSWORD
mode: server

advanced:
  obfuscation: obfs4
  port_hopping: true
  pfs: true
  zero_rtt: true

# 重启服务
systemctl restart veildeploy
```

---

### 方案 3: AWS Lightsail 部署

#### 步骤 1: 创建实例

1. 登录 [AWS Lightsail](https://lightsail.aws.amazon.com)
2. 点击 "Create instance"
3. 选择配置：

```
Instance Location: Tokyo (ap-northeast-1)
Platform: Linux/Unix
Blueprint: Ubuntu 22.04 LTS
Instance Plan: $5/month (1GB RAM, 40GB SSD, 2TB Transfer)
```

#### 步骤 2: 配置启动脚本

在 "Launch script" 中添加：

```bash
#!/bin/bash
curl -fsSL https://get.veildeploy.com | bash
```

#### 步骤 3: 配置网络

1. 进入实例详情页
2. 点击 "Networking" 标签
3. 添加防火墙规则：

```
Application: Custom
Protocol: TCP+UDP
Port: 51820
```

#### 步骤 4: SSH 连接

使用 Lightsail 的浏览器 SSH：
1. 点击实例
2. 点击 "Connect using SSH"
3. 在浏览器终端中配置 VeilDeploy

---

### 方案 4: Google Cloud Platform 部署

#### 步骤 1: 激活 $300 免费额度

1. 访问 [Google Cloud](https://cloud.google.com/free)
2. 注册并添加信用卡（不会扣费）
3. 获得 $300 试用额度（可用12个月）

#### 步骤 2: 创建 VM 实例

1. 进入 "Compute Engine" > "VM instances"
2. 点击 "Create Instance"
3. 配置：

```
Name: veildeploy-server
Region: asia-northeast1 (Tokyo)
Zone: asia-northeast1-a
Machine Type: e2-micro (0.25 vCPU, 1GB RAM) - 免费层
Boot Disk: Ubuntu 22.04 LTS (20GB)
Firewall: ✅ Allow HTTP/HTTPS
```

#### 步骤 3: 配置防火墙规则

1. 进入 "VPC Network" > "Firewall"
2. 创建规则：

```
Name: veildeploy-port
Targets: All instances
Source IP ranges: 0.0.0.0/0
Protocols and ports: tcp:51820,udp:51820
```

#### 步骤 4: 连接并安装

```bash
# 使用 gcloud 命令连接
gcloud compute ssh veildeploy-server --zone=asia-northeast1-a

# 或在控制台使用浏览器 SSH

# 安装 VeilDeploy
curl -fsSL https://get.veildeploy.com | bash
```

---

### 方案 5: 阿里云/腾讯云部署（国内用户）

⚠️ **注意**: 国内云服务器需要备案，且可能受监管限制。建议使用境外服务器。

如果必须使用国内云：

1. 购买香港/新加坡节点
2. 确保选择 "按流量计费"
3. 配置安全组：开放 51820 端口
4. 其余步骤与上述类似

---

## 📖 完整部署流程（标准化）

### 第一步：服务器初始化（所有云平台通用）

```bash
# 1. 更新系统
apt update && apt upgrade -y

# 2. 安装基础工具
apt install -y curl wget vim git ufw

# 3. 配置时区
timedatectl set-timezone Asia/Shanghai

# 4. 优化系统参数
cat >> /etc/sysctl.conf << EOF
# VeilDeploy优化
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr
net.core.rmem_max=134217728
net.core.wmem_max=134217728
net.ipv4.tcp_rmem=4096 87380 67108864
net.ipv4.tcp_wmem=4096 65536 67108864
fs.file-max=51200
EOF

sysctl -p

# 5. 创建 swap（可选，内存<2GB时推荐）
fallocate -l 2G /swapfile
chmod 600 /swapfile
mkswap /swapfile
swapon /swapfile
echo '/swapfile none swap sw 0 0' >> /etc/fstab
```

### 第二步：安装 VeilDeploy

```bash
# 方式1：一键安装（推荐）
curl -fsSL https://get.veildeploy.com | bash

# 方式2：手动安装
wget https://github.com/veildeploy/veildeploy/releases/latest/download/veildeploy-linux-amd64.tar.gz
tar -xzf veildeploy-linux-amd64.tar.gz
mv veildeploy /usr/local/bin/
chmod +x /usr/local/bin/veildeploy
```

### 第三步：配置 VeilDeploy

```bash
# 创建配置目录
mkdir -p /etc/veildeploy

# 创建服务器配置
cat > /etc/veildeploy/config.yaml << 'EOF'
# VeilDeploy 服务器配置
server: 0.0.0.0:51820
password: CHANGE_THIS_TO_STRONG_PASSWORD
mode: server

# 中国优化配置
advanced:
  # 流量混淆
  obfuscation: obfs4

  # 动态端口跳跃
  port_hopping: true
  port_range: "10000-60000"
  hop_interval: 60s

  # 流量回落
  fallback: true
  fallback_addr: www.bing.com:443

  # 安全特性
  pfs: true
  zero_rtt: true

  # 性能优化
  cipher: chacha20
  mtu: 1420
  keep_alive: 15s

# 日志配置
log:
  level: info
  file: /var/log/veildeploy/server.log
EOF

# 生成强密码
PASSWORD=$(openssl rand -base64 32)
sed -i "s/CHANGE_THIS_TO_STRONG_PASSWORD/$PASSWORD/" /etc/veildeploy/config.yaml

# 显示密码（记录下来！）
echo "===================="
echo "您的服务器密码是："
echo "$PASSWORD"
echo "===================="
echo "请保存此密码！"
```

### 第四步：创建 systemd 服务

```bash
# 创建服务文件
cat > /etc/systemd/system/veildeploy.service << 'EOF'
[Unit]
Description=VeilDeploy VPN Server
After=network.target
Documentation=https://docs.veildeploy.com

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/veildeploy server -c /etc/veildeploy/config.yaml
Restart=on-failure
RestartSec=5s
LimitNOFILE=65536

# 安全加固
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

# 创建日志目录
mkdir -p /var/log/veildeploy

# 重载 systemd
systemctl daemon-reload

# 启动服务
systemctl start veildeploy

# 设置开机自启
systemctl enable veildeploy

# 查看状态
systemctl status veildeploy
```

### 第五步：配置防火墙

```bash
# 配置 UFW 防火墙
ufw allow 22/tcp      # SSH（必须！）
ufw allow 51820/tcp   # VeilDeploy TCP
ufw allow 51820/udp   # VeilDeploy UDP

# 如果启用了端口跳跃
ufw allow 10000:60000/tcp
ufw allow 10000:60000/udp

# 启用防火墙
ufw --force enable

# 查看状态
ufw status numbered
```

### 第六步：验证部署

```bash
# 1. 检查服务状态
systemctl status veildeploy

# 2. 检查端口监听
netstat -tulpn | grep 51820

# 3. 查看日志
journalctl -u veildeploy -n 50

# 4. 测试连接
telnet localhost 51820
```

### 第七步：生成客户端配置

```bash
# 获取服务器公网IP
SERVER_IP=$(curl -s ifconfig.me)

# 读取密码
PASSWORD=$(grep 'password:' /etc/veildeploy/config.yaml | awk '{print $2}')

# 生成客户端配置
cat > ~/client-config.yaml << EOF
# VeilDeploy 客户端配置
server: $SERVER_IP:51820
password: $PASSWORD
mode: auto
EOF

# 显示配置
echo "===================="
echo "客户端配置："
cat ~/client-config.yaml
echo "===================="

# 生成连接URL
echo "快速连接URL："
echo "veil://chacha20:$PASSWORD@$SERVER_IP:51820/?obfs=obfs4&pfs=true"
```

---

## ⚡ 性能优化建议

### 1. 内核优化（BBR 加速）

```bash
# 安装最新内核（Ubuntu）
apt install -y linux-image-generic

# 启用 BBR
echo "net.core.default_qdisc=fq" >> /etc/sysctl.conf
echo "net.ipv4.tcp_congestion_control=bbr" >> /etc/sysctl.conf
sysctl -p

# 验证 BBR
sysctl net.ipv4.tcp_congestion_control
# 应该输出：net.ipv4.tcp_congestion_control = bbr
```

### 2. 网络优化

```bash
cat >> /etc/sysctl.conf << EOF
# TCP优化
net.ipv4.tcp_fastopen=3
net.ipv4.tcp_slow_start_after_idle=0
net.ipv4.tcp_notsent_lowat=16384

# 连接数优化
net.ipv4.ip_local_port_range=1024 65535
net.ipv4.tcp_max_syn_backlog=8192
net.core.somaxconn=8192

# 内存优化
net.ipv4.tcp_mem=88560 118080 177120
net.ipv4.tcp_rmem=4096 87380 16777216
net.ipv4.tcp_wmem=4096 65536 16777216
EOF

sysctl -p
```

### 3. VeilDeploy 配置优化

```yaml
advanced:
  # 高性能模式（牺牲部分安全性）
  cipher: chacha20-poly1305  # 最快
  compression: false         # 禁用压缩

  # 调整 MTU
  mtu: 1420  # 标准，如果经常丢包降到 1380

  # 减少 keepalive 开销
  keep_alive: 25s

  # 根据需求选择
  obfuscation: none  # 无混淆最快
  # 或
  obfuscation: obfs4  # 抗审查最好
```

### 4. 多核 CPU 优化

```bash
# 安装 irqbalance（自动平衡中断）
apt install -y irqbalance
systemctl enable irqbalance
systemctl start irqbalance
```

---

## 🔒 安全加固

### 1. SSH 安全加固

```bash
# 修改 SSH 配置
cat >> /etc/ssh/sshd_config << EOF
# 安全加固
PermitRootLogin prohibit-password  # 禁止密码登录root
PasswordAuthentication no          # 禁用密码认证（使用SSH Key）
PubkeyAuthentication yes           # 启用公钥认证
ChallengeResponseAuthentication no
UsePAM yes
X11Forwarding no
MaxAuthTries 3
MaxSessions 5
ClientAliveInterval 300
ClientAliveCountMax 2
EOF

# 重启 SSH
systemctl restart sshd
```

### 2. 安装 Fail2Ban（防暴力破解）

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
port = 22
logpath = /var/log/auth.log

[veildeploy]
enabled = true
port = 51820
logpath = /var/log/veildeploy/server.log
maxretry = 10
EOF

# 启动
systemctl enable fail2ban
systemctl start fail2ban

# 查看状态
fail2ban-client status
```

### 3. 自动更新

```bash
# 安装自动更新
apt install -y unattended-upgrades

# 配置
dpkg-reconfigure --priority=low unattended-upgrades
```

### 4. 定期备份

```bash
# 创建备份脚本
cat > /root/backup-veildeploy.sh << 'EOF'
#!/bin/bash
BACKUP_DIR="/root/backups"
DATE=$(date +%Y%m%d-%H%M%S)

mkdir -p $BACKUP_DIR

# 备份配置
tar -czf $BACKUP_DIR/veildeploy-$DATE.tar.gz \
  /etc/veildeploy \
  /var/lib/veildeploy

# 保留最近7天的备份
find $BACKUP_DIR -name "veildeploy-*.tar.gz" -mtime +7 -delete

echo "Backup completed: veildeploy-$DATE.tar.gz"
EOF

chmod +x /root/backup-veildeploy.sh

# 添加到 crontab（每天凌晨3点备份）
echo "0 3 * * * /root/backup-veildeploy.sh" | crontab -
```

---

## 💰 成本优化

### 1. 按流量计费 vs 按带宽计费

| 方案 | 适合场景 | 优点 | 缺点 |
|-----|---------|------|------|
| **按流量** | 轻度使用 | 便宜，用多少付多少 | 超量费用高 |
| **按带宽** | 重度使用 | 稳定，不限流量 | 固定成本高 |

**建议**:
- 个人使用：按流量
- 企业使用：按带宽

### 2. 竞价实例（Spot Instance）

AWS/GCP 提供竞价实例，价格可低至正常价格的 10-30%：

```bash
# AWS Spot Instance 可节省 70%
# GCP Preemptible VM 可节省 80%
```

**注意**: 竞价实例可能被随时回收，不适合生产环境。

### 3. 预留实例（Reserved Instance）

长期使用可购买预留实例：

- 1年预留：节省 30-40%
- 3年预留：节省 50-60%

### 4. 流量优化

```yaml
advanced:
  # 启用压缩（节省流量）
  compression: true

  # 使用更高效的加密算法
  cipher: chacha20
```

### 5. 多用户分摊成本

```bash
# 10个用户共享 $20/月 服务器 = 每人 $2/月
# 配置多用户认证
veildeploy user create user1 --password "Pass123!"
veildeploy user create user2 --password "Pass456!"
# ...
```

---

## 📊 监控和维护

### 1. 安装监控面板

```bash
# 安装 Netdata（实时性能监控）
bash <(curl -Ss https://my-netdata.io/kickstart.sh)

# 访问: http://YOUR_SERVER_IP:19999
```

### 2. 查看连接数

```bash
# 实时查看连接
watch -n 1 'netstat -an | grep 51820 | wc -l'

# 查看详细连接
ss -tuln | grep 51820
```

### 3. 流量统计

```bash
# 安装 vnstat
apt install -y vnstat

# 启动服务
systemctl enable vnstat
systemctl start vnstat

# 查看流量
vnstat
vnstat -d  # 按天统计
vnstat -m  # 按月统计
```

### 4. 日志分析

```bash
# 查看实时日志
journalctl -u veildeploy -f

# 查看错误日志
journalctl -u veildeploy -p err

# 查看今天的日志
journalctl -u veildeploy --since today

# 导出日志
journalctl -u veildeploy > /tmp/veildeploy.log
```

### 5. 自动告警

```bash
# 安装监控告警工具
apt install -y monitoring-plugins nagios-plugins-contrib

# 创建检查脚本
cat > /usr/local/bin/check-veildeploy.sh << 'EOF'
#!/bin/bash
if ! systemctl is-active --quiet veildeploy; then
    echo "VeilDeploy is DOWN!"
    # 发送邮件或Telegram通知
    curl -X POST "https://api.telegram.org/bot<TOKEN>/sendMessage" \
      -d "chat_id=<CHAT_ID>" \
      -d "text=VeilDeploy服务器宕机！"
    exit 1
fi
echo "VeilDeploy is running"
exit 0
EOF

chmod +x /usr/local/bin/check-veildeploy.sh

# 添加到 crontab（每5分钟检查一次）
echo "*/5 * * * * /usr/local/bin/check-veildeploy.sh" | crontab -
```

---

## 🔧 常见问题排查

### 问题 1: 无法连接到服务器

**排查步骤**:

```bash
# 1. 检查服务是否运行
systemctl status veildeploy

# 2. 检查端口是否监听
netstat -tulpn | grep 51820

# 3. 检查防火墙
ufw status
iptables -L -n

# 4. 检查云平台安全组
# 登录云平台控制台检查安全组规则

# 5. 测试端口连通性
# 在本地电脑执行
telnet YOUR_SERVER_IP 51820
nc -zv YOUR_SERVER_IP 51820
```

### 问题 2: 连接速度慢

**解决方案**:

```bash
# 1. 测试网络质量
ping YOUR_SERVER_IP
mtr YOUR_SERVER_IP

# 2. 启用 BBR（见上文）

# 3. 优化 MTU
# 编辑配置，将 mtu 从 1420 改为 1380 或 1280

# 4. 检查 CPU 占用
top
htop

# 5. 检查带宽限制
speedtest-cli
```

### 问题 3: 服务频繁重启

```bash
# 查看崩溃日志
journalctl -u veildeploy -p err --since "1 hour ago"

# 检查内存
free -h

# 检查磁盘空间
df -h

# 增加 swap（如果内存不足）
fallocate -l 2G /swapfile
chmod 600 /swapfile
mkswap /swapfile
swapon /swapfile
```

### 问题 4: IP 被墙

**解决方案**:

```bash
# 1. 启用端口跳跃和混淆
# 编辑配置文件
advanced:
  obfuscation: obfs4
  port_hopping: true

# 2. 更换 IP
# 在云平台控制台重新分配弹性IP

# 3. 使用 CDN
advanced:
  cdn: cloudflare

# 4. 使用桥接模式
veildeploy bridge register
```

---

## 📚 脚本合集

### 一键部署脚本（适用所有云平台）

```bash
#!/bin/bash
# VeilDeploy 一键部署脚本

set -e

echo "======================================"
echo "    VeilDeploy 云服务器一键部署"
echo "======================================"
echo ""

# 检查 root 权限
if [[ $EUID -ne 0 ]]; then
   echo "错误：此脚本需要 root 权限运行"
   exit 1
fi

# 更新系统
echo "[1/8] 更新系统..."
apt update && apt upgrade -y

# 安装依赖
echo "[2/8] 安装依赖..."
apt install -y curl wget vim ufw

# 系统优化
echo "[3/8] 优化系统参数..."
cat >> /etc/sysctl.conf << EOF
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr
net.core.rmem_max=134217728
net.core.wmem_max=134217728
fs.file-max=51200
EOF
sysctl -p

# 安装 VeilDeploy
echo "[4/8] 安装 VeilDeploy..."
curl -fsSL https://get.veildeploy.com | bash

# 配置服务器
echo "[5/8] 配置服务器..."
mkdir -p /etc/veildeploy
PASSWORD=$(openssl rand -base64 24)

cat > /etc/veildeploy/config.yaml << EOF
server: 0.0.0.0:51820
password: $PASSWORD
mode: server

advanced:
  obfuscation: obfs4
  port_hopping: true
  pfs: true
  zero_rtt: true
  cipher: chacha20
  mtu: 1420

log:
  level: info
  file: /var/log/veildeploy/server.log
EOF

# 配置防火墙
echo "[6/8] 配置防火墙..."
ufw allow 22
ufw allow 51820
ufw allow 10000:60000/tcp
ufw allow 10000:60000/udp
ufw --force enable

# 启动服务
echo "[7/8] 启动服务..."
systemctl daemon-reload
systemctl enable veildeploy
systemctl start veildeploy

# 显示信息
echo "[8/8] 部署完成！"
echo ""
echo "======================================"
echo "        部署信息"
echo "======================================"
echo "服务器地址: $(curl -s ifconfig.me):51820"
echo "密码: $PASSWORD"
echo ""
echo "客户端配置："
echo "---"
cat > ~/client-config.yaml << CLIENTEOF
server: $(curl -s ifconfig.me):51820
password: $PASSWORD
mode: auto
CLIENTEOF
cat ~/client-config.yaml
echo "---"
echo ""
echo "配置文件已保存到: ~/client-config.yaml"
echo ""
echo "查看服务状态: systemctl status veildeploy"
echo "查看日志: journalctl -u veildeploy -f"
echo ""
echo "======================================"
```

**使用方法**:

```bash
# 下载并执行
wget https://raw.githubusercontent.com/veildeploy/veildeploy/main/scripts/cloud-deploy.sh
chmod +x cloud-deploy.sh
./cloud-deploy.sh
```

---

## 🎓 总结

### 推荐配置（性价比最高）

**云服务商**: Vultr 或 DigitalOcean
**机房位置**: 日本东京
**服务器配置**: 2核 2GB 内存, 5Mbps 带宽
**月费**: $15-20
**可支持**: 20-50人同时使用

### 快速部署清单

- [ ] 购买云服务器（Vultr/DO/AWS）
- [ ] SSH 连接服务器
- [ ] 运行一键部署脚本
- [ ] 配置防火墙规则
- [ ] 启动服务并设置自启
- [ ] 生成客户端配置
- [ ] 测试连接
- [ ] 配置监控和备份

### 下一步

- 阅读 [DEPLOYMENT_GUIDE.md](./DEPLOYMENT_GUIDE.md) 了解更多配置选项
- 阅读 [IMPROVEMENTS_SUMMARY.md](./IMPROVEMENTS_SUMMARY.md) 了解所有功能
- 加入社区: https://community.veildeploy.com

---

**祝您部署顺利！**  🚀

如遇问题，请访问 [GitHub Issues](https://github.com/veildeploy/veildeploy/issues)
