#!/bin/bash
set -e

echo "=========================================="
echo "VeilDeploy 自动部署脚本"
echo "=========================================="

# 颜色定义
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

# 步骤 1: 系统信息
echo -e "\n${YELLOW}[1/8] 检查系统信息...${NC}"
uname -a
cat /etc/os-release | head -5

# 步骤 2: 更新系统
echo -e "\n${YELLOW}[2/8] 更新系统并安装依赖...${NC}"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq git curl wget build-essential ufw >/dev/null 2>&1
echo -e "${GREEN}✓ 系统更新完成${NC}"

# 步骤 3: BBR 优化
echo -e "\n${YELLOW}[3/8] 启用 BBR TCP 优化...${NC}"
if ! grep -q "net.core.default_qdisc=fq" /etc/sysctl.conf; then
    cat >> /etc/sysctl.conf << EOF

# BBR TCP 优化
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr
net.ipv4.tcp_fastopen=3
net.core.rmem_max=16777216
net.core.wmem_max=16777216
net.ipv4.ip_forward=1
fs.file-max=51200
EOF
    sysctl -p >/dev/null 2>&1
fi
echo -e "${GREEN}✓ BBR 已启用${NC}"

# 步骤 4: 安装 Go
echo -e "\n${YELLOW}[4/8] 安装 Go 1.21.5...${NC}"
if [ ! -f /usr/local/go/bin/go ]; then
    cd /tmp
    wget -q https://go.dev/dl/go1.21.5.linux-amd64.tar.gz
    tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz
    rm go1.21.5.linux-amd64.tar.gz
fi
export PATH=$PATH:/usr/local/go/bin
export GOPATH=/root/go
echo -e "${GREEN}✓ Go $(/usr/local/go/bin/go version | awk '{print $3}') 已安装${NC}"

# 步骤 5: 克隆并编译
echo -e "\n${YELLOW}[5/8] 克隆并编译 VeilDeploy...${NC}"
cd /root
if [ -d "VeilDeploy" ]; then
    rm -rf VeilDeploy
fi
git clone -q https://github.com/veilconnect/VeilDeploy.git
cd VeilDeploy
/usr/local/go/bin/go build -o veildeploy .
BINARY_SIZE=$(du -h veildeploy | cut -f1)
echo -e "${GREEN}✓ 编译完成 (${BINARY_SIZE})${NC}"

# 步骤 6: 配置防火墙
echo -e "\n${YELLOW}[6/8] 配置防火墙...${NC}"
ufw --force enable >/dev/null 2>&1
ufw allow 22/tcp >/dev/null 2>&1
ufw allow 51820/udp >/dev/null 2>&1
echo -e "${GREEN}✓ 防火墙已配置${NC}"

# 步骤 7: 创建配置
echo -e "\n${YELLOW}[7/8] 创建服务配置...${NC}"
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
echo -e "${GREEN}✓ 配置文件已创建${NC}"

# 步骤 8: 启动服务
echo -e "\n${YELLOW}[8/8] 启动服务...${NC}"
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
SERVER_IP=$(curl -s ifconfig.me)
cat > /root/veildeploy-credentials.txt << EOF
========================================
VeilDeploy 部署成功！
========================================

服务器信息:
  IP: ${SERVER_IP}
  端口: 51820 (UDP)
  密码: ${PSK}

客户端配置:
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
  状态: systemctl status veildeploy
  日志: journalctl -u veildeploy -f
  指标: curl http://127.0.0.1:7777/metrics

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

# 显示服务状态
systemctl status veildeploy --no-pager -l

# 显示监听端口
echo ""
echo "监听端口:"
netstat -tuln | grep -E "51820|7777"

# 显示管理接口
echo ""
echo "管理接口:"
curl -s http://127.0.0.1:7777/metrics | head -10
