# VeilDeploy VPN 节点部署指南

## 📋 目录

1. [快速开始](#快速开始)
2. [服务器端部署](#服务器端部署)
3. [客户端部署](#客户端部署)
4. [高级配置](#高级配置)
5. [生产环境部署](#生产环境部署)
6. [常见问题](#常见问题)

---

## 🚀 快速开始

### 方式一：一键安装（推荐）

#### Linux/macOS

```bash
# 下载并运行安装脚本
curl -fsSL https://get.veildeploy.com | bash

# 或者使用本地脚本
bash scripts/install.sh
```

#### Windows

```powershell
# PowerShell (管理员权限)
iwr -useb https://get.veildeploy.com/install.ps1 | iex

# 或者使用本地脚本
.\scripts\install.ps1
```

### 方式二：手动安装

#### 1. 下载二进制文件

从 [GitHub Releases](https://github.com/veildeploy/veildeploy/releases) 下载对应平台的二进制文件。

```bash
# Linux amd64
wget https://github.com/veildeploy/veildeploy/releases/latest/download/veildeploy-linux-amd64.tar.gz
tar -xzf veildeploy-linux-amd64.tar.gz
sudo mv veildeploy /usr/local/bin/

# macOS
wget https://github.com/veildeploy/veildeploy/releases/latest/download/veildeploy-darwin-amd64.tar.gz
tar -xzf veildeploy-darwin-amd64.tar.gz
sudo mv veildeploy /usr/local/bin/

# Windows
# 下载 veildeploy-windows-amd64.zip 并解压到 C:\Program Files\VeilDeploy\
```

#### 2. 验证安装

```bash
veildeploy version
```

---

## 🖥️ 服务器端部署

### 场景 1：基础 VPN 服务器（最简配置）

#### 步骤 1：创建配置文件

```bash
mkdir -p ~/.veildeploy
cat > ~/.veildeploy/config.yaml << 'EOF'
# VeilDeploy 服务器配置
server: 0.0.0.0:51820
password: YOUR_SECURE_PASSWORD_HERE
mode: server
EOF
```

**重要**：请将 `YOUR_SECURE_PASSWORD_HERE` 替换为强密码！

#### 步骤 2：启动服务器

```bash
# 前台运行（测试用）
veildeploy server -c ~/.veildeploy/config.yaml

# 后台运行
nohup veildeploy server -c ~/.veildeploy/config.yaml > /var/log/veildeploy.log 2>&1 &
```

#### 步骤 3：配置防火墙

```bash
# Ubuntu/Debian
sudo ufw allow 51820/tcp
sudo ufw allow 51820/udp

# CentOS/RHEL
sudo firewall-cmd --permanent --add-port=51820/tcp
sudo firewall-cmd --permanent --add-port=51820/udp
sudo firewall-cmd --reload

# iptables
sudo iptables -A INPUT -p tcp --dport 51820 -j ACCEPT
sudo iptables -A INPUT -p udp --dport 51820 -j ACCEPT
```

#### 步骤 4：测试连接

```bash
# 测试端口是否开放
nc -zv YOUR_SERVER_IP 51820
```

---

### 场景 2：高抗审查服务器（中国优化）

```yaml
# VeilDeploy 高抗审查配置
server: 0.0.0.0:51820
password: YOUR_SECURE_PASSWORD_HERE
mode: server

advanced:
  # 流量混淆（伪装成TLS）
  obfuscation: obfs4

  # 动态端口跳跃
  port_hopping: true
  port_range: "10000-60000"
  hop_interval: 60s

  # 流量回落（检测到探测时伪装成正常网站）
  fallback: true
  fallback_addr: www.bing.com:443

  # 完美前向保密
  pfs: true

  # 0-RTT快速重连
  zero_rtt: true

  # 加密算法
  cipher: chacha20

  # MTU优化
  mtu: 1420
  keep_alive: 15s
```

---

### 场景 3：企业服务器（多用户+认证）

#### 步骤 1：启用密码认证

```yaml
server: 0.0.0.0:51820
password: ADMIN_PASSWORD
mode: server

# 启用用户认证
auth:
  enabled: true
  type: password
  database: /var/lib/veildeploy/users.json

  # 账户锁定配置
  max_retries: 5
  lockout_time: 15m

advanced:
  # 启用2FA
  2fa: true

  # 其他配置...
  obfuscation: tls
  pfs: true
```

#### 步骤 2：创建用户

```bash
# 创建管理员用户
veildeploy user create admin \
  --password "SecureP@ss123" \
  --email "admin@company.com" \
  --role admin

# 创建普通用户
veildeploy user create employee1 \
  --password "Employee@123" \
  --email "employee1@company.com"

# 启用2FA
veildeploy user enable-2fa employee1
# 扫描显示的二维码绑定验证器
```

#### 步骤 3：证书认证（可选）

```bash
# 生成CA证书
veildeploy cert generate-ca \
  --common-name "Company VPN CA" \
  --organization "My Company" \
  --valid-for 10y \
  --output /etc/veildeploy/ca/

# 为用户签发证书
veildeploy cert issue \
  --ca /etc/veildeploy/ca/ca.crt \
  --ca-key /etc/veildeploy/ca/ca.key \
  --common-name "employee1@company.com" \
  --valid-for 1y \
  --output /etc/veildeploy/certs/employee1/

# 配置服务器使用证书认证
cat >> ~/.veildeploy/config.yaml << 'EOF'
auth:
  enabled: true
  type: certificate
  ca_cert: /etc/veildeploy/ca/ca.crt
  ca_key: /etc/veildeploy/ca/ca.key
EOF
```

---

### 场景 4：使用 systemd 服务（推荐生产环境）

#### 创建服务文件

```bash
sudo tee /etc/systemd/system/veildeploy.service > /dev/null << 'EOF'
[Unit]
Description=VeilDeploy VPN Server
After=network.target
Documentation=https://docs.veildeploy.com

[Service]
Type=simple
User=veildeploy
Group=veildeploy
ExecStart=/usr/local/bin/veildeploy server -c /etc/veildeploy/config.yaml
Restart=on-failure
RestartSec=5s
LimitNOFILE=65536

# 安全加固
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/veildeploy /var/log/veildeploy

[Install]
WantedBy=multi-user.target
EOF
```

#### 创建用户和目录

```bash
# 创建系统用户
sudo useradd -r -s /bin/false veildeploy

# 创建目录
sudo mkdir -p /etc/veildeploy /var/lib/veildeploy /var/log/veildeploy
sudo chown -R veildeploy:veildeploy /etc/veildeploy /var/lib/veildeploy /var/log/veildeploy

# 复制配置
sudo cp ~/.veildeploy/config.yaml /etc/veildeploy/
sudo chown veildeploy:veildeploy /etc/veildeploy/config.yaml
sudo chmod 600 /etc/veildeploy/config.yaml
```

#### 启动服务

```bash
# 重载 systemd
sudo systemctl daemon-reload

# 启动服务
sudo systemctl start veildeploy

# 查看状态
sudo systemctl status veildeploy

# 开机自启
sudo systemctl enable veildeploy

# 查看日志
sudo journalctl -u veildeploy -f
```

---

## 💻 客户端部署

### 场景 1：极简客户端（3行配置）

```bash
# 创建配置
mkdir -p ~/.veildeploy
cat > ~/.veildeploy/config.yaml << 'EOF'
server: YOUR_SERVER_IP:51820
password: YOUR_PASSWORD
mode: auto
EOF

# 启动客户端
veildeploy client -c ~/.veildeploy/config.yaml
```

**说明**：`mode: auto` 会自动检测网络环境并优化配置。

---

### 场景 2：URL 快速连接

```bash
# 方式1：直接使用URL
veildeploy connect "veil://chacha20:mypass@vpn.example.com:51820/?obfs=tls"

# 方式2：从QR码导入
veildeploy import-qr qrcode.png

# 方式3：从剪贴板
veildeploy connect "$(pbpaste)"  # macOS
veildeploy connect "$(xclip -o)"  # Linux
```

---

### 场景 3：智能分流客户端

```yaml
server: vpn.example.com:51820
password: YOUR_PASSWORD
mode: client

# 路由配置
routing:
  default_action: proxy

  # 应用预设规则
  presets:
    - china-direct    # 中国网站直连
    - block-ads       # 广告拦截
    - local-direct    # 本地网络直连

  # 自定义规则
  rules:
    # Google 走代理
    - type: domain-suffix
      pattern: .google.com
      action: proxy

    # 中国IP直连
    - type: geoip
      pattern: CN
      action: direct

    # 拦截跟踪器
    - type: domain-keyword
      pattern: analytics
      action: block
```

---

### 场景 4：企业客户端（证书认证+2FA）

```yaml
server: vpn.company.com:51820
password: YOUR_PASSWORD
mode: client

# 证书认证
auth:
  type: certificate
  client_cert: /path/to/employee.crt
  client_key: /path/to/employee.key
  ca_cert: /path/to/ca.crt

# 2FA认证
2fa:
  enabled: true
  # 使用验证器App生成的6位数字码
```

#### 连接时输入2FA码

```bash
# 启动时会提示输入2FA令牌
veildeploy client -c ~/.veildeploy/config.yaml

# 或通过命令行参数
veildeploy client -c ~/.veildeploy/config.yaml --2fa-token 123456
```

---

## 🔧 高级配置

### 桥接模式（突破封锁）

#### 服务器端：注册为桥接节点

```bash
# 启动桥接发现服务（在独立服务器上）
veildeploy bridge-discovery \
  --listen :8080 \
  --database /var/lib/veildeploy/bridges.json

# 注册桥接节点
veildeploy bridge register \
  --discovery https://bridges.veildeploy.com \
  --address bridge1.example.com:51820 \
  --type direct \
  --capacity 100 \
  --location US
```

#### 客户端：获取桥接节点

```bash
# 方式1：通过HTTPS获取
veildeploy bridge get \
  --discovery https://bridges.veildeploy.com \
  --count 3

# 方式2：通过邮件获取
# 发送邮件到 bridges@veildeploy.com

# 方式3：使用桥接地址连接
veildeploy connect \
  --bridge bridge1.example.com:51820 \
  -c config.yaml
```

---

### CDN 加速

```yaml
server: vpn.example.com:51820
password: YOUR_PASSWORD
mode: client

advanced:
  # 使用Cloudflare CDN
  cdn: cloudflare
  cdn_domain: cdn.example.com

  # 或使用自定义CDN
  cdn: custom
  cdn_url: https://cdn.example.com/proxy
```

---

### 多服务器负载均衡

```yaml
mode: client
password: YOUR_PASSWORD

# 服务器列表
servers:
  - address: vpn1.example.com:51820
    priority: 1
    weight: 10

  - address: vpn2.example.com:51820
    priority: 1
    weight: 5

  - address: vpn3.example.com:51820
    priority: 2
    weight: 1

# 负载均衡策略
load_balance:
  strategy: weighted  # random/weighted/latency
  health_check: true
  check_interval: 30s
```

---

## 🏭 生产环境部署

### 架构建议

```
┌─────────────────────────────────────────────────┐
│                    客户端                        │
│  (Windows/macOS/Linux/iOS/Android)              │
└────────────────┬────────────────────────────────┘
                 │
                 │ HTTPS/TLS
                 ↓
┌─────────────────────────────────────────────────┐
│              CDN/负载均衡                         │
│  (Cloudflare/Nginx/HAProxy)                     │
└────────────────┬────────────────────────────────┘
                 │
         ┌───────┴───────┐
         ↓               ↓
┌─────────────┐  ┌─────────────┐
│ VPN Server 1│  │ VPN Server 2│
│   (主节点)   │  │   (备节点)   │
└──────┬──────┘  └──────┬──────┘
       │                │
       └────────┬───────┘
                ↓
     ┌─────────────────┐
     │   桥接发现服务    │
     │  (独立服务器)     │
     └─────────────────┘
```

### Docker 部署

#### Dockerfile

```dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY . .
RUN go build -o veildeploy .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/veildeploy .
COPY config.yaml .

EXPOSE 51820
CMD ["./veildeploy", "server", "-c", "config.yaml"]
```

#### docker-compose.yml

```yaml
version: '3.8'

services:
  veildeploy-server:
    build: .
    container_name: veildeploy
    restart: unless-stopped
    ports:
      - "51820:51820/tcp"
      - "51820:51820/udp"
    volumes:
      - ./config.yaml:/root/config.yaml:ro
      - veildeploy-data:/var/lib/veildeploy
    environment:
      - VEILDEPLOY_LOG_LEVEL=info
    networks:
      - vpn-network

  bridge-discovery:
    build: .
    container_name: veildeploy-bridges
    restart: unless-stopped
    ports:
      - "8080:8080"
    command: ["./veildeploy", "bridge-discovery", "--listen", ":8080"]
    volumes:
      - bridge-data:/var/lib/veildeploy
    networks:
      - vpn-network

volumes:
  veildeploy-data:
  bridge-data:

networks:
  vpn-network:
    driver: bridge
```

#### 启动

```bash
# 构建并启动
docker-compose up -d

# 查看日志
docker-compose logs -f

# 停止
docker-compose down
```

---

### Kubernetes 部署

#### deployment.yaml

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: veildeploy-server
  namespace: vpn
spec:
  replicas: 3
  selector:
    matchLabels:
      app: veildeploy
  template:
    metadata:
      labels:
        app: veildeploy
    spec:
      containers:
      - name: veildeploy
        image: veildeploy/veildeploy:latest
        ports:
        - containerPort: 51820
          protocol: TCP
        - containerPort: 51820
          protocol: UDP
        volumeMounts:
        - name: config
          mountPath: /etc/veildeploy
        - name: data
          mountPath: /var/lib/veildeploy
        resources:
          requests:
            memory: "256Mi"
            cpu: "500m"
          limits:
            memory: "1Gi"
            cpu: "2000m"
      volumes:
      - name: config
        configMap:
          name: veildeploy-config
      - name: data
        persistentVolumeClaim:
          claimName: veildeploy-data

---
apiVersion: v1
kind: Service
metadata:
  name: veildeploy-service
  namespace: vpn
spec:
  type: LoadBalancer
  selector:
    app: veildeploy
  ports:
  - name: tcp
    port: 51820
    targetPort: 51820
    protocol: TCP
  - name: udp
    port: 51820
    targetPort: 51820
    protocol: UDP
```

---

### 监控和日志

#### Prometheus 监控

```yaml
# VeilDeploy 内置 Prometheus metrics
# 启动时添加 --metrics-port 参数

veildeploy server -c config.yaml --metrics-port 9090
```

**Metrics 端点**: `http://localhost:9090/metrics`

**关键指标**:
- `veildeploy_connections_total` - 总连接数
- `veildeploy_active_connections` - 活跃连接
- `veildeploy_bytes_sent` - 发送字节数
- `veildeploy_bytes_received` - 接收字节数
- `veildeploy_auth_failures` - 认证失败次数

#### 日志配置

```yaml
log:
  level: info  # debug/info/warn/error
  format: json  # json/text
  output: /var/log/veildeploy/server.log

  # 日志轮转
  rotate:
    enabled: true
    max_size: 100  # MB
    max_backups: 10
    max_age: 30  # days
```

---

## ❓ 常见问题

### Q1: 如何更改服务器端口？

修改配置文件中的 `server` 字段：

```yaml
server: 0.0.0.0:8443  # 改为8443端口
```

记得更新防火墙规则！

### Q2: 如何查看在线用户？

```bash
veildeploy status

# 或通过API
curl http://localhost:7777/api/connections
```

### Q3: 如何重置用户密码？

```bash
veildeploy user reset-password username --password NewPass123!
```

### Q4: 连接失败怎么办？

**检查清单**:

1. 服务器是否运行？
   ```bash
   systemctl status veildeploy
   ```

2. 防火墙是否开放？
   ```bash
   sudo ufw status
   sudo firewall-cmd --list-all
   ```

3. 配置是否正确？
   ```bash
   veildeploy config validate -c config.yaml
   ```

4. 查看详细日志：
   ```bash
   veildeploy client -c config.yaml --log-level debug
   ```

### Q5: 如何提升性能？

**服务器端优化**:

```yaml
advanced:
  # 使用更快的加密算法
  cipher: chacha20

  # 禁用压缩（除非带宽受限）
  compression: false

  # 优化MTU
  mtu: 1420

  # 减少keepalive
  keep_alive: 25s

  # 禁用不需要的功能
  obfuscation: none
  port_hopping: false
```

**系统优化**:

```bash
# 增加文件描述符限制
sudo tee -a /etc/security/limits.conf << EOF
*  soft  nofile  65536
*  hard  nofile  65536
EOF

# 优化内核参数
sudo tee -a /etc/sysctl.conf << EOF
net.core.rmem_max = 134217728
net.core.wmem_max = 134217728
net.ipv4.tcp_rmem = 4096 87380 67108864
net.ipv4.tcp_wmem = 4096 65536 67108864
EOF

sudo sysctl -p
```

### Q6: 如何备份配置？

```bash
# 备份配置和数据
tar -czf veildeploy-backup-$(date +%Y%m%d).tar.gz \
  /etc/veildeploy \
  /var/lib/veildeploy

# 恢复
tar -xzf veildeploy-backup-20250101.tar.gz -C /
```

---

## 📚 更多资源

- **完整文档**: https://docs.veildeploy.com
- **API 文档**: https://api.veildeploy.com
- **GitHub**: https://github.com/veildeploy/veildeploy
- **社区支持**: https://community.veildeploy.com
- **问题反馈**: https://github.com/veildeploy/veildeploy/issues

---

## 🆘 获取帮助

```bash
# 查看命令帮助
veildeploy --help
veildeploy server --help
veildeploy client --help

# 生成示例配置
veildeploy config generate --mode server > server.yaml
veildeploy config generate --mode client > client.yaml

# 验证配置
veildeploy config validate -c config.yaml

# 运行诊断
veildeploy diagnose
```

---

**祝您部署顺利！** 🎉

如有问题，请随时在 GitHub Issues 提问。
