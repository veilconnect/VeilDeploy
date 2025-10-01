#!/bin/bash
set -e

echo "=========================================="
echo "VeilDeploy 快速部署脚本"
echo "=========================================="

# 颜色定义
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

# 步骤 1: 系统信息
echo -e "\n${YELLOW}[1/7] 检查系统信息...${NC}"
uname -a

# 步骤 2: 快速安装依赖
echo -e "\n${YELLOW}[2/7] 安装依赖（跳过系统更新）...${NC}"
export DEBIAN_FRONTEND=noninteractive
apt-get install -y git curl wget build-essential ufw >/dev/null 2>&1 || echo "依赖已安装"
echo -e "${GREEN}✓ 依赖安装完成${NC}"

# 步骤 3: BBR 优化
echo -e "\n${YELLOW}[3/7] 启用 BBR TCP 优化...${NC}"
if ! grep -q "net.core.default_qdisc=fq" /etc/sysctl.conf 2>/dev/null; then
    cat >> /etc/sysctl.conf << EOF

# BBR TCP 优化
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr
net.ipv4.ip_forward=1
EOF
    sysctl -p >/dev/null 2>&1 || true
fi
echo -e "${GREEN}✓ BBR 已启用${NC}"

# 步骤 4: 安装 Go
echo -e "\n${YELLOW}[4/7] 检查 Go 环境...${NC}"
if [ ! -f /usr/local/go/bin/go ]; then
    cd /tmp
    wget -q https://go.dev/dl/go1.21.5.linux-amd64.tar.gz
    tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz
    rm go1.21.5.linux-amd64.tar.gz
fi
export PATH=$PATH:/usr/local/go/bin
echo -e "${GREEN}✓ Go $(/usr/local/go/bin/go version | awk '{print $3}') 已安装${NC}"

# 步骤 5: 克隆并编译
echo -e "\n${YELLOW}[5/7] 克隆并编译 VeilDeploy...${NC}"
cd /root
if [ -d "VeilDeploy" ]; then
    rm -rf VeilDeploy
fi
git clone -q https://github.com/veilconnect/VeilDeploy.git 2>&1 | grep -v "Cloning" || true
cd VeilDeploy
/usr/local/go/bin/go build -o veildeploy . 2>&1
BINARY_SIZE=$(du -h veildeploy | cut -f1)
echo -e "${GREEN}✓ 编译完成 (${BINARY_SIZE})${NC}"

# 步骤 6: 配置防火墙
echo -e "\n${YELLOW}[6/7] 配置防火墙...${NC}"
ufw --force enable >/dev/null 2>&1 || true
ufw allow 22/tcp >/dev/null 2>&1 || true
ufw allow 51820/udp >/dev/null 2>&1 || true
echo -e "${GREEN}✓ 防火墙已配置${NC}"

# 步骤 7: 创建配置和启动服务
echo -e "\n${YELLOW}[7/7] 创建服务配置...${NC}"
mkdir -p /etc/veildeploy
mkdir -p /var/log/veildeploy

# 生成随机密码
PSK=$(openssl rand -base64 24)

cat > /etc/veildeploy/config.json << EOF
{
  "mode": "server",
  "listen": "0.0.0.0:51820",
  "psk": "${PSK}",
  "keepalive": "25s",
  "maxPadding": 255,
  "peers": [
    {
      "name": "client1",
      "allowedIPs": ["10.0.0.2/32"]
    }
  ],
  "management": {
    "enabled": true,
    "listen": "127.0.0.1:7777"
  },
  "logging": {
    "level": "info",
    "file": "/var/log/veildeploy/server.log"
  },
  "tunnel": {
    "type": "tun",
    "name": "veil0",
    "mtu": 1420,
    "address": "10.0.0.1/24"
  }
}
EOF

chmod 600 /etc/veildeploy/config.json

# 创建 systemd 服务
cat > /etc/systemd/system/veildeploy.service << EOF
[Unit]
Description=VeilDeploy VPN Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/veildeploy -config /etc/veildeploy/config.json -mode server
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

cp veildeploy /usr/local/bin/
chmod +x /usr/local/bin/veildeploy

systemctl daemon-reload
systemctl enable veildeploy >/dev/null 2>&1
systemctl start veildeploy
sleep 2

# 验证服务状态
if systemctl is-active --quiet veildeploy; then
    echo -e "${GREEN}✓ 服务启动成功${NC}"
else
    echo -e "${RED}✗ 服务启动失败${NC}"
    journalctl -u veildeploy -n 20 --no-pager
    exit 1
fi

# 保存连接信息
SERVER_IP=$(curl -s ifconfig.me || hostname -I | awk '{print $1}')
cat > /root/veildeploy-credentials.txt << EOF
========================================
VeilDeploy 部署成功！
========================================

服务器信息:
  IP: ${SERVER_IP}
  端口: 51820 (UDP)
  密码: ${PSK}

客户端配置 (保存为 client-config.json):
{
  "mode": "client",
  "endpoint": "${SERVER_IP}:51820",
  "psk": "${PSK}",
  "keepalive": "25s",
  "maxPadding": 255,
  "peers": [
    {
      "name": "server",
      "endpoint": "${SERVER_IP}:51820",
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

管理命令:
  查看状态: systemctl status veildeploy
  查看日志: journalctl -u veildeploy -f
  查看指标: curl http://127.0.0.1:7777/metrics

========================================
EOF

# 显示部署结果
echo ""
echo "=========================================="
echo -e "${GREEN}部署完成！${NC}"
echo "=========================================="
echo ""
echo "服务器: ${SERVER_IP}:51820"
echo "密码: ${PSK}"
echo ""
echo "详细信息已保存到: /root/veildeploy-credentials.txt"
echo ""
echo "服务状态:"
systemctl status veildeploy --no-pager -l | head -15
