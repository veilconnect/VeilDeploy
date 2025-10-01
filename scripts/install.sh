#!/bin/bash
# VeilDeploy 一键安装脚本
# 支持: Linux, macOS
# 用法: curl -fsSL https://get.veildeploy.com | bash

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 版本
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
CONFIG_DIR="${CONFIG_DIR:-$HOME/.veildeploy}"

# Logo
print_logo() {
    echo -e "${BLUE}"
    cat << "EOF"
╔═══════════════════════════════════════════╗
║                                           ║
║        VeilDeploy 一键安装脚本            ║
║                                           ║
║        Next-Gen Anti-Censorship VPN       ║
║                                           ║
╚═══════════════════════════════════════════╝
EOF
    echo -e "${NC}"
}

# 打印步骤
print_step() {
    echo -e "${GREEN}[$(date +'%H:%M:%S')]${NC} $1"
}

# 打印错误
print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 打印警告
print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

# 检测系统
detect_os() {
    print_step "检测系统..."

    OS="$(uname -s)"
    ARCH="$(uname -m)"

    case "$OS" in
        Linux*)
            OS="linux"
            ;;
        Darwin*)
            OS="darwin"
            ;;
        MINGW*|MSYS*)
            OS="windows"
            ;;
        *)
            print_error "不支持的操作系统: $OS"
            exit 1
            ;;
    esac

    case "$ARCH" in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        armv7l)
            ARCH="armv7"
            ;;
        *)
            print_error "不支持的架构: $ARCH"
            exit 1
            ;;
    esac

    echo "  系统: $OS"
    echo "  架构: $ARCH"
}

# 检查依赖
check_dependencies() {
    print_step "检查依赖..."

    local missing_deps=()

    # 检查curl
    if ! command -v curl &> /dev/null; then
        missing_deps+=("curl")
    fi

    # 检查tar
    if ! command -v tar &> /dev/null; then
        missing_deps+=("tar")
    fi

    if [ ${#missing_deps[@]} -gt 0 ]; then
        print_error "缺少依赖: ${missing_deps[*]}"
        echo "请先安装: sudo apt-get install ${missing_deps[*]}"
        exit 1
    fi

    echo "  ✓ 所有依赖已满足"
}

# 下载二进制
download_binary() {
    print_step "下载 VeilDeploy..."

    local download_url
    if [ "$VERSION" = "latest" ]; then
        download_url="https://github.com/veildeploy/veildeploy/releases/latest/download/veildeploy-${OS}-${ARCH}.tar.gz"
    else
        download_url="https://github.com/veildeploy/veildeploy/releases/download/${VERSION}/veildeploy-${OS}-${ARCH}.tar.gz"
    fi

    echo "  下载地址: $download_url"

    # 创建临时目录
    local tmp_dir=$(mktemp -d)
    cd "$tmp_dir"

    # 下载
    if ! curl -fsSL "$download_url" -o veildeploy.tar.gz; then
        print_error "下载失败"
        rm -rf "$tmp_dir"
        exit 1
    fi

    # 解压
    tar -xzf veildeploy.tar.gz

    # 安装
    if [ -w "$INSTALL_DIR" ]; then
        cp veildeploy "$INSTALL_DIR/"
        chmod +x "$INSTALL_DIR/veildeploy"
    else
        echo "  需要管理员权限..."
        sudo cp veildeploy "$INSTALL_DIR/"
        sudo chmod +x "$INSTALL_DIR/veildeploy"
    fi

    # 清理
    cd - > /dev/null
    rm -rf "$tmp_dir"

    echo "  ✓ 安装完成: $INSTALL_DIR/veildeploy"
}

# 初始化配置
init_config() {
    print_step "初始化配置..."

    # 创建配置目录
    mkdir -p "$CONFIG_DIR"

    # 检测安装模式
    echo ""
    echo "请选择安装模式:"
    echo "  1) 服务器模式 (Server)"
    echo "  2) 客户端模式 (Client)"
    echo ""
    read -p "请选择 [1-2]: " mode_choice

    case "$mode_choice" in
        1)
            init_server
            ;;
        2)
            init_client
            ;;
        *)
            print_error "无效选择"
            exit 1
            ;;
    esac
}

# 初始化服务器
init_server() {
    print_step "配置服务器模式..."

    # 生成密钥
    echo "  生成密钥..."
    "$INSTALL_DIR/veildeploy" keygen > "$CONFIG_DIR/keys.yaml"

    # 获取公网IP
    local public_ip=$(curl -s ifconfig.me || echo "YOUR_IP")

    # 生成配置
    cat > "$CONFIG_DIR/config.yaml" << EOF
# VeilDeploy 服务器配置
mode: server

# 监听地址
listen: 0.0.0.0:51820

# 密钥（从keys.yaml加载）
private_key: \${PRIVATE_KEY}

# 中国优化配置（高抗审查）
china_optimized: true

# 自动配置
auto:
  # 流量混淆
  obfuscation: obfs4

  # 端口跳跃
  port_hopping: true
  port_range: "10000-60000"
  hop_interval: 60s

  # 流量回落
  fallback: true
  fallback_target: "www.bing.com:443"

  # 完美前向保密
  pfs: true

  # 0-RTT
  zero_rtt: true

# 日志
log:
  level: info
  file: $CONFIG_DIR/server.log
EOF

    echo ""
    echo -e "${GREEN}✓ 服务器配置完成！${NC}"
    echo ""
    echo "服务器信息:"
    echo "  地址: $public_ip:51820"
    echo "  配置: $CONFIG_DIR/config.yaml"
    echo ""
    echo "客户端连接配置:"
    echo "  veil://chacha20:\${PASSWORD}@$public_ip:51820/?obfs=obfs4&cdn=false"
    echo ""
    echo "启动服务器:"
    echo "  $INSTALL_DIR/veildeploy server -c $CONFIG_DIR/config.yaml"
    echo ""
}

# 初始化客户端
init_client() {
    print_step "配置客户端模式..."

    echo ""
    read -p "请输入服务器地址 (例: vpn.example.com:51820): " server_addr
    read -sp "请输入密码: " password
    echo ""

    # 生成配置
    cat > "$CONFIG_DIR/config.yaml" << EOF
# VeilDeploy 客户端配置
mode: client

# 服务器
server: $server_addr
password: $password

# 自动优化
auto:
  mode: auto  # 自动选择最佳配置

# 日志
log:
  level: info
  file: $CONFIG_DIR/client.log
EOF

    echo ""
    echo -e "${GREEN}✓ 客户端配置完成！${NC}"
    echo ""
    echo "配置文件: $CONFIG_DIR/config.yaml"
    echo ""
    echo "连接服务器:"
    echo "  $INSTALL_DIR/veildeploy client -c $CONFIG_DIR/config.yaml"
    echo ""
}

# 安装systemd服务
install_service() {
    if [ "$OS" != "linux" ]; then
        return
    fi

    print_step "安装系统服务..."

    echo ""
    read -p "是否安装为系统服务? [y/N]: " install_systemd

    if [[ ! "$install_systemd" =~ ^[Yy]$ ]]; then
        return
    fi

    # 创建systemd服务文件
    sudo tee /etc/systemd/system/veildeploy.service > /dev/null << EOF
[Unit]
Description=VeilDeploy VPN Service
After=network.target

[Service]
Type=simple
User=$USER
ExecStart=$INSTALL_DIR/veildeploy server -c $CONFIG_DIR/config.yaml
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

    # 重载systemd
    sudo systemctl daemon-reload

    echo ""
    echo "系统服务已安装"
    echo ""
    echo "启动服务:"
    echo "  sudo systemctl start veildeploy"
    echo ""
    echo "开机自启:"
    echo "  sudo systemctl enable veildeploy"
    echo ""
    echo "查看状态:"
    echo "  sudo systemctl status veildeploy"
    echo ""
}

# 主函数
main() {
    print_logo

    detect_os
    check_dependencies
    download_binary
    init_config
    install_service

    echo ""
    echo -e "${GREEN}╔═══════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║                                           ║${NC}"
    echo -e "${GREEN}║        🎉 安装完成！                      ║${NC}"
    echo -e "${GREEN}║                                           ║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════╝${NC}"
    echo ""
    echo "版本信息:"
    "$INSTALL_DIR/veildeploy" version
    echo ""
    echo "帮助文档:"
    echo "  https://docs.veildeploy.com"
    echo ""
    echo "问题反馈:"
    echo "  https://github.com/veildeploy/veildeploy/issues"
    echo ""
}

# 运行
main
