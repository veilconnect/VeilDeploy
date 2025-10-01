#!/bin/bash

#######################################################################
# VeilDeploy 云服务器一键部署脚本
# 适用于: Ubuntu 20.04/22.04, Debian 11/12, CentOS 8+
# 使用方法: curl -fsSL https://get.veildeploy.com/cloud-deploy.sh | bash
#######################################################################

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 输出函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 检查是否为 root
check_root() {
    if [ "$EUID" -ne 0 ]; then
        log_error "请使用 root 用户运行此脚本"
        log_info "运行: sudo bash $0"
        exit 1
    fi
}

# 检测操作系统
detect_os() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        OS=$ID
        VER=$VERSION_ID
    else
        log_error "无法检测操作系统"
        exit 1
    fi

    log_info "检测到操作系统: $OS $VER"
}

# 检查系统要求
check_requirements() {
    log_info "检查系统要求..."

    # 检查内存
    MEMORY=$(free -m | awk '/^Mem:/{print $2}')
    if [ "$MEMORY" -lt 400 ]; then
        log_error "内存不足，至少需要 512MB (当前: ${MEMORY}MB)"
        exit 1
    fi
    log_success "内存检查通过: ${MEMORY}MB"

    # 检查磁盘空间
    DISK=$(df -m / | awk 'NR==2 {print $4}')
    if [ "$DISK" -lt 1000 ]; then
        log_error "磁盘空间不足，至少需要 10GB (当前可用: ${DISK}MB)"
        exit 1
    fi
    log_success "磁盘检查通过: ${DISK}MB 可用"

    # 检查 CPU 架构
    ARCH=$(uname -m)
    case $ARCH in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        *)
            log_error "不支持的 CPU 架构: $ARCH"
            exit 1
            ;;
    esac
    log_success "CPU 架构: $ARCH"
}

# 更新系统
update_system() {
    log_info "[1/8] 更新系统..."

    case $OS in
        ubuntu|debian)
            export DEBIAN_FRONTEND=noninteractive
            apt-get update -qq
            apt-get upgrade -y -qq
            apt-get install -y -qq curl wget vim ufw iptables net-tools
            ;;
        centos|rhel|fedora)
            yum update -y -q
            yum install -y -q curl wget vim firewalld iptables net-tools
            ;;
        *)
            log_error "不支持的操作系统: $OS"
            exit 1
            ;;
    esac

    log_success "系统更新完成"
}

# 优化系统
optimize_system() {
    log_info "[2/8] 优化系统性能..."

    # 检查内核版本
    KERNEL_VERSION=$(uname -r | cut -d. -f1)
    KERNEL_MINOR=$(uname -r | cut -d. -f2)

    # 启用 BBR (需要内核 4.9+)
    if [ "$KERNEL_VERSION" -ge 5 ] || ([ "$KERNEL_VERSION" -eq 4 ] && [ "$KERNEL_MINOR" -ge 9 ]); then
        log_info "启用 BBR TCP 拥塞控制..."

        cat >> /etc/sysctl.conf <<EOF

# BBR TCP 拥塞控制
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr
EOF

        sysctl -p >/dev/null 2>&1

        # 验证 BBR
        if sysctl net.ipv4.tcp_congestion_control | grep -q bbr; then
            log_success "BBR 已启用"
        else
            log_warning "BBR 启用失败，但不影响使用"
        fi
    else
        log_warning "内核版本过低 ($(uname -r))，无法启用 BBR"
        log_info "建议升级内核到 4.9+ 以获得更好性能"
    fi

    # 网络优化
    cat >> /etc/sysctl.conf <<EOF

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

# 文件描述符限制
fs.file-max=51200
EOF

    sysctl -p >/dev/null 2>&1

    # 增加文件描述符限制
    cat >> /etc/security/limits.conf <<EOF
* soft nofile 51200
* hard nofile 51200
EOF

    log_success "系统优化完成"
}

# 安装 VeilDeploy
install_veildeploy() {
    log_info "[3/8] 安装 VeilDeploy..."

    # 获取最新版本
    LATEST_VERSION=$(curl -s https://api.github.com/repos/veildeploy/veildeploy/releases/latest | grep -oP '"tag_name": "\K(.*)(?=")')

    if [ -z "$LATEST_VERSION" ]; then
        log_warning "无法获取最新版本，使用默认版本"
        LATEST_VERSION="v2.0.0"
    fi

    log_info "下载版本: $LATEST_VERSION"

    # 下载
    DOWNLOAD_URL="https://github.com/veildeploy/veildeploy/releases/download/${LATEST_VERSION}/veildeploy-linux-${ARCH}.tar.gz"

    cd /tmp
    if ! wget -q --show-progress "$DOWNLOAD_URL" -O veildeploy.tar.gz; then
        log_error "下载失败，请检查网络连接"
        exit 1
    fi

    # 解压
    tar -xzf veildeploy.tar.gz

    # 安装
    mv veildeploy /usr/local/bin/
    chmod +x /usr/local/bin/veildeploy

    # 创建目录
    mkdir -p /etc/veildeploy
    mkdir -p /var/log/veildeploy

    # 验证安装
    if /usr/local/bin/veildeploy --version >/dev/null 2>&1; then
        log_success "VeilDeploy 安装成功"
    else
        log_error "VeilDeploy 安装失败"
        exit 1
    fi

    # 清理
    rm -f /tmp/veildeploy.tar.gz
}

# 生成配置
generate_config() {
    log_info "[4/8] 生成配置文件..."

    # 生成安全的随机密码 (32 字符)
    PASSWORD=$(openssl rand -base64 32 | tr -d "=+/" | cut -c1-32)

    # 获取服务器 IP
    SERVER_IP=$(curl -s ifconfig.me || curl -s icanhazip.com || curl -s ipinfo.io/ip)

    if [ -z "$SERVER_IP" ]; then
        log_warning "无法自动获取服务器 IP，请手动配置"
        SERVER_IP="YOUR_SERVER_IP"
    fi

    # 生成配置文件
    cat > /etc/veildeploy/config.yaml <<EOF
# VeilDeploy 服务器配置
# 生成时间: $(date)

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

    log_success "配置文件已生成"

    # 保存密码到文件
    cat > /root/veildeploy-credentials.txt <<EOF
=================================
VeilDeploy 服务器信息
=================================

服务器地址: $SERVER_IP:51820
密码: $PASSWORD
生成时间: $(date)

客户端配置:
---------------------------------
server: $SERVER_IP:51820
password: $PASSWORD
mode: client

URL 配置:
---------------------------------
veil://$PASSWORD@$SERVER_IP:51820

=================================
请妥善保管此信息！
=================================
EOF

    chmod 600 /root/veildeploy-credentials.txt
}

# 配置防火墙
configure_firewall() {
    log_info "[5/8] 配置防火墙..."

    case $OS in
        ubuntu|debian)
            # 使用 UFW
            if command -v ufw >/dev/null 2>&1; then
                # 允许 SSH
                ufw allow 22/tcp >/dev/null 2>&1 || true

                # 允许 VeilDeploy
                ufw allow 51820/udp >/dev/null 2>&1 || true

                # 启用防火墙
                echo "y" | ufw enable >/dev/null 2>&1 || true

                log_success "UFW 防火墙配置完成"
            fi
            ;;
        centos|rhel|fedora)
            # 使用 firewalld
            if command -v firewall-cmd >/dev/null 2>&1; then
                systemctl start firewalld >/dev/null 2>&1 || true
                systemctl enable firewalld >/dev/null 2>&1 || true

                firewall-cmd --permanent --add-port=51820/udp >/dev/null 2>&1 || true
                firewall-cmd --reload >/dev/null 2>&1 || true

                log_success "firewalld 防火墙配置完成"
            fi
            ;;
    esac

    # iptables 规则（作为后备）
    iptables -I INPUT -p udp --dport 51820 -j ACCEPT >/dev/null 2>&1 || true

    log_warning "请确保云平台安全组也开放了 UDP 51820 端口"
}

# 创建系统服务
create_service() {
    log_info "[6/8] 创建系统服务..."

    cat > /etc/systemd/system/veildeploy.service <<EOF
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

    log_success "系统服务创建完成"
}

# 启动服务
start_service() {
    log_info "[7/8] 启动 VeilDeploy 服务..."

    # 启动服务
    systemctl start veildeploy

    # 设置开机自启动
    systemctl enable veildeploy >/dev/null 2>&1

    # 等待服务启动
    sleep 2

    # 检查服务状态
    if systemctl is-active --quiet veildeploy; then
        log_success "VeilDeploy 服务启动成功"
    else
        log_error "VeilDeploy 服务启动失败"
        log_info "查看日志: journalctl -u veildeploy -n 50"
        exit 1
    fi
}

# 验证部署
verify_deployment() {
    log_info "[8/8] 验证部署..."

    # 检查端口监听
    sleep 1
    if netstat -tuln 2>/dev/null | grep -q ":51820" || ss -tuln 2>/dev/null | grep -q ":51820"; then
        log_success "端口 51820 监听正常"
    else
        log_warning "无法确认端口监听状态"
    fi

    # 检查进程
    if pgrep -x veildeploy >/dev/null; then
        log_success "VeilDeploy 进程运行正常"
    else
        log_error "VeilDeploy 进程未运行"
        exit 1
    fi

    log_success "部署验证完成"
}

# 显示部署信息
show_info() {
    clear

    cat <<EOF

${GREEN}╔═══════════════════════════════════════════════════════════╗
║                                                           ║
║     🎉  VeilDeploy 部署成功！                             ║
║                                                           ║
╚═══════════════════════════════════════════════════════════╝${NC}

${BLUE}服务器信息:${NC}
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

$(cat /root/veildeploy-credentials.txt)

${BLUE}管理命令:${NC}
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  启动服务:  systemctl start veildeploy
  停止服务:  systemctl stop veildeploy
  重启服务:  systemctl restart veildeploy
  查看状态:  systemctl status veildeploy
  查看日志:  journalctl -u veildeploy -f

${BLUE}配置文件:${NC}
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  服务器配置: /etc/veildeploy/config.yaml
  凭据信息:   /root/veildeploy-credentials.txt

${BLUE}下一步:${NC}
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  1. 复制上面的"客户端配置"到本地电脑
  2. 在本地安装 VeilDeploy 客户端
  3. 使用配置连接到服务器

${YELLOW}安全提示:${NC}
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  • 请妥善保管 /root/veildeploy-credentials.txt 文件
  • 建议修改 SSH 端口并禁用密码登录
  • 定期备份配置文件
  • 启用 Fail2Ban 防止暴力破解

${GREEN}享受安全的网络连接！ 🚀${NC}

EOF
}

# 主函数
main() {
    log_info "开始部署 VeilDeploy VPN 服务器..."
    echo ""

    check_root
    detect_os
    check_requirements
    update_system
    optimize_system
    install_veildeploy
    generate_config
    configure_firewall
    create_service
    start_service
    verify_deployment
    show_info
}

# 执行主函数
main
