# VeilDeploy Windows 一键安装脚本
# 用法: iwr -useb https://get.veildeploy.com/install.ps1 | iex

param(
    [string]$Version = "latest",
    [string]$InstallDir = "$env:ProgramFiles\VeilDeploy",
    [string]$ConfigDir = "$env:USERPROFILE\.veildeploy"
)

$ErrorActionPreference = "Stop"

# 颜色函数
function Write-ColorOutput {
    param(
        [Parameter(Mandatory=$true)]
        [string]$Message,
        [string]$Color = "White"
    )
    Write-Host $Message -ForegroundColor $Color
}

# Logo
function Show-Logo {
    Write-ColorOutput @"
╔═══════════════════════════════════════════╗
║                                           ║
║        VeilDeploy 一键安装脚本            ║
║                                           ║
║        Next-Gen Anti-Censorship VPN       ║
║                                           ║
╚═══════════════════════════════════════════╝
"@ -Color Cyan
}

# 步骤
function Write-Step {
    param([string]$Message)
    Write-ColorOutput "[$(Get-Date -Format 'HH:mm:ss')] $Message" -Color Green
}

# 错误
function Write-ErrorMsg {
    param([string]$Message)
    Write-ColorOutput "[ERROR] $Message" -Color Red
}

# 检测系统
function Test-SystemRequirements {
    Write-Step "检测系统..."

    # 检查Windows版本
    $os = Get-WmiObject Win32_OperatingSystem
    $version = [Environment]::OSVersion.Version

    if ($version.Major -lt 10) {
        Write-ErrorMsg "需要 Windows 10 或更高版本"
        exit 1
    }

    Write-Host "  系统: Windows $($os.Caption)"
    Write-Host "  版本: $($version)"
    Write-Host "  架构: $($env:PROCESSOR_ARCHITECTURE)"
}

# 检查管理员权限
function Test-Administrator {
    $currentUser = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
    return $currentUser.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

# 下载文件
function Get-VeilDeploy {
    Write-Step "下载 VeilDeploy..."

    # 确定架构
    $arch = if ($env:PROCESSOR_ARCHITECTURE -eq "AMD64") { "amd64" } else { "386" }

    # 构建下载URL
    if ($Version -eq "latest") {
        $url = "https://github.com/veildeploy/veildeploy/releases/latest/download/veildeploy-windows-$arch.zip"
    } else {
        $url = "https://github.com/veildeploy/veildeploy/releases/download/$Version/veildeploy-windows-$arch.zip"
    }

    Write-Host "  下载地址: $url"

    # 创建临时目录
    $tempDir = New-Item -ItemType Directory -Path "$env:TEMP\veildeploy-install-$(Get-Random)" -Force

    try {
        # 下载
        $zipFile = Join-Path $tempDir "veildeploy.zip"
        Invoke-WebRequest -Uri $url -OutFile $zipFile -UseBasicParsing

        # 解压
        Expand-Archive -Path $zipFile -DestinationPath $tempDir -Force

        # 安装
        if (-not (Test-Path $InstallDir)) {
            New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        }

        Copy-Item -Path "$tempDir\veildeploy.exe" -Destination $InstallDir -Force

        Write-Host "  ✓ 安装完成: $InstallDir\veildeploy.exe"
    }
    finally {
        # 清理
        Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# 添加到PATH
function Add-ToPath {
    Write-Step "添加到系统路径..."

    $currentPath = [Environment]::GetEnvironmentVariable("Path", "Machine")

    if ($currentPath -notlike "*$InstallDir*") {
        $newPath = "$currentPath;$InstallDir"
        [Environment]::SetEnvironmentVariable("Path", $newPath, "Machine")
        $env:Path = $newPath
        Write-Host "  ✓ 已添加到系统路径"
    } else {
        Write-Host "  ✓ 已存在于系统路径"
    }
}

# 初始化配置
function Initialize-Config {
    Write-Step "初始化配置..."

    # 创建配置目录
    if (-not (Test-Path $ConfigDir)) {
        New-Item -ItemType Directory -Path $ConfigDir -Force | Out-Null
    }

    Write-Host ""
    Write-Host "请选择安装模式:"
    Write-Host "  1) 服务器模式 (Server)"
    Write-Host "  2) 客户端模式 (Client)"
    Write-Host ""

    $choice = Read-Host "请选择 [1-2]"

    switch ($choice) {
        "1" { Initialize-Server }
        "2" { Initialize-Client }
        default {
            Write-ErrorMsg "无效选择"
            exit 1
        }
    }
}

# 初始化服务器
function Initialize-Server {
    Write-Step "配置服务器模式..."

    # 生成密钥
    Write-Host "  生成密钥..."
    & "$InstallDir\veildeploy.exe" keygen | Out-File "$ConfigDir\keys.yaml" -Encoding UTF8

    # 获取公网IP
    try {
        $publicIP = (Invoke-WebRequest -Uri "https://ifconfig.me" -UseBasicParsing).Content.Trim()
    } catch {
        $publicIP = "YOUR_IP"
    }

    # 生成配置
    $config = @"
# VeilDeploy 服务器配置
mode: server

# 监听地址
listen: 0.0.0.0:51820

# 密钥（从keys.yaml加载）
private_key: `${PRIVATE_KEY}

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
  file: $ConfigDir\server.log
"@

    $config | Out-File "$ConfigDir\config.yaml" -Encoding UTF8

    Write-Host ""
    Write-ColorOutput "✓ 服务器配置完成！" -Color Green
    Write-Host ""
    Write-Host "服务器信息:"
    Write-Host "  地址: ${publicIP}:51820"
    Write-Host "  配置: $ConfigDir\config.yaml"
    Write-Host ""
    Write-Host "启动服务器:"
    Write-Host "  veildeploy server -c $ConfigDir\config.yaml"
    Write-Host ""
}

# 初始化客户端
function Initialize-Client {
    Write-Step "配置客户端模式..."

    Write-Host ""
    $serverAddr = Read-Host "请输入服务器地址 (例: vpn.example.com:51820)"
    $password = Read-Host "请输入密码" -AsSecureString
    $passwordPlain = [Runtime.InteropServices.Marshal]::PtrToStringAuto(
        [Runtime.InteropServices.Marshal]::SecureStringToBSTR($password)
    )

    $config = @"
# VeilDeploy 客户端配置
mode: client

# 服务器
server: $serverAddr
password: $passwordPlain

# 自动优化
auto:
  mode: auto  # 自动选择最佳配置

# 日志
log:
  level: info
  file: $ConfigDir\client.log
"@

    $config | Out-File "$ConfigDir\config.yaml" -Encoding UTF8

    Write-Host ""
    Write-ColorOutput "✓ 客户端配置完成！" -Color Green
    Write-Host ""
    Write-Host "配置文件: $ConfigDir\config.yaml"
    Write-Host ""
    Write-Host "连接服务器:"
    Write-Host "  veildeploy client -c $ConfigDir\config.yaml"
    Write-Host ""
}

# 安装Windows服务
function Install-Service {
    Write-Step "安装Windows服务..."

    Write-Host ""
    $installService = Read-Host "是否安装为Windows服务? [y/N]"

    if ($installService -ne "y" -and $installService -ne "Y") {
        return
    }

    # 使用NSSM安装服务
    $nssmUrl = "https://nssm.cc/release/nssm-2.24.zip"
    $nssmZip = "$env:TEMP\nssm.zip"

    Write-Host "  下载NSSM..."
    Invoke-WebRequest -Uri $nssmUrl -OutFile $nssmZip -UseBasicParsing

    $nssmDir = "$env:TEMP\nssm"
    Expand-Archive -Path $nssmZip -DestinationPath $nssmDir -Force

    $nssm = Get-ChildItem -Path $nssmDir -Filter "nssm.exe" -Recurse | Select-Object -First 1

    # 安装服务
    & $nssm.FullName install VeilDeploy "$InstallDir\veildeploy.exe" "server -c $ConfigDir\config.yaml"
    & $nssm.FullName set VeilDeploy Description "VeilDeploy VPN Service"
    & $nssm.FullName set VeilDeploy Start SERVICE_AUTO_START

    Write-Host ""
    Write-Host "Windows服务已安装"
    Write-Host ""
    Write-Host "启动服务:"
    Write-Host "  net start VeilDeploy"
    Write-Host ""
    Write-Host "或使用服务管理器 (services.msc)"
    Write-Host ""

    # 清理
    Remove-Item -Path $nssmZip -Force -ErrorAction SilentlyContinue
    Remove-Item -Path $nssmDir -Recurse -Force -ErrorAction SilentlyContinue
}

# 创建防火墙规则
function Add-FirewallRule {
    Write-Step "配置防火墙..."

    try {
        # 检查是否已存在规则
        $existingRule = Get-NetFirewallRule -DisplayName "VeilDeploy" -ErrorAction SilentlyContinue

        if (-not $existingRule) {
            New-NetFirewallRule -DisplayName "VeilDeploy" `
                                -Direction Inbound `
                                -Program "$InstallDir\veildeploy.exe" `
                                -Action Allow `
                                -Profile Any `
                                -ErrorAction Stop | Out-Null

            Write-Host "  ✓ 防火墙规则已添加"
        } else {
            Write-Host "  ✓ 防火墙规则已存在"
        }
    } catch {
        Write-Host "  ⚠ 防火墙规则添加失败: $_" -ForegroundColor Yellow
    }
}

# 主函数
function Main {
    Show-Logo

    # 检查管理员权限
    if (-not (Test-Administrator)) {
        Write-ErrorMsg "需要管理员权限运行此脚本"
        Write-Host "请右键点击PowerShell，选择'以管理员身份运行'"
        exit 1
    }

    Test-SystemRequirements
    Get-VeilDeploy
    Add-ToPath
    Initialize-Config
    Add-FirewallRule
    Install-Service

    Write-Host ""
    Write-ColorOutput "╔═══════════════════════════════════════════╗" -Color Green
    Write-ColorOutput "║                                           ║" -Color Green
    Write-ColorOutput "║        🎉 安装完成！                      ║" -Color Green
    Write-ColorOutput "║                                           ║" -Color Green
    Write-ColorOutput "╚═══════════════════════════════════════════╝" -Color Green
    Write-Host ""

    Write-Host "版本信息:"
    & "$InstallDir\veildeploy.exe" version
    Write-Host ""
    Write-Host "帮助文档:"
    Write-Host "  https://docs.veildeploy.com"
    Write-Host ""
    Write-Host "问题反馈:"
    Write-Host "  https://github.com/veildeploy/veildeploy/issues"
    Write-Host ""
}

# 运行
Main
