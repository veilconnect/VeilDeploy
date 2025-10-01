# VeilDeploy GitHub 仓库设置脚本
# PowerShell 版本

# 设置控制台编码
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "VeilDeploy GitHub 仓库设置脚本" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# 检查是否在正确的目录
if (-not (Test-Path "go.mod")) {
    Write-Host "❌ 错误：请在 VeilDeploy 项目根目录运行此脚本！" -ForegroundColor Red
    Read-Host "按回车键退出"
    exit 1
}

# 检查 Git 是否安装
try {
    $gitVersion = git --version
    Write-Host "✅ Git 已安装: $gitVersion" -ForegroundColor Green
} catch {
    Write-Host "❌ 错误：Git 未安装或不在 PATH 中" -ForegroundColor Red
    Write-Host "请访问 https://git-scm.com/download/win 下载安装" -ForegroundColor Yellow
    Read-Host "按回车键退出"
    exit 1
}

Write-Host ""
Write-Host "[1/8] 检查 Git 配置..." -ForegroundColor Yellow

$userName = git config user.name
$userEmail = git config user.email

if (-not $userName -or -not $userEmail) {
    Write-Host ""
    Write-Host "需要配置 Git 用户信息：" -ForegroundColor Yellow
    $userName = Read-Host "输入你的 GitHub 用户名"
    $userEmail = Read-Host "输入你的 GitHub 邮箱"

    git config --global user.name $userName
    git config --global user.email $userEmail

    Write-Host "✅ Git 配置完成" -ForegroundColor Green
} else {
    Write-Host "✅ Git 已配置 ($userName <$userEmail>)" -ForegroundColor Green
}

Write-Host ""
Write-Host "[2/8] 初始化 Git 仓库..." -ForegroundColor Yellow

if (-not (Test-Path ".git")) {
    git init
    Write-Host "✅ Git 仓库初始化完成" -ForegroundColor Green
} else {
    Write-Host "✅ Git 仓库已存在" -ForegroundColor Green
}

Write-Host ""
Write-Host "[3/8] 添加文件到 Git..." -ForegroundColor Yellow

git add .
$fileCount = (git diff --cached --numstat | Measure-Object).Count
Write-Host "✅ 已添加 $fileCount 个文件" -ForegroundColor Green

Write-Host ""
Write-Host "[4/8] 创建初始提交..." -ForegroundColor Yellow

$commitMessage = @"
Initial commit: VeilDeploy 2.0

- Implemented Noise Protocol Framework
- Added multi-factor authentication
- Implemented advanced routing system
- Added bridge discovery mechanism
- Created comprehensive documentation
- 78 passing tests with 70% coverage
"@

git commit -m $commitMessage 2>$null

if ($LASTEXITCODE -eq 0) {
    Write-Host "✅ 提交创建完成" -ForegroundColor Green
} else {
    Write-Host "ℹ️  提交已存在或无更改" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "[5/8] 重命名主分支为 main..." -ForegroundColor Yellow

git branch -M main
Write-Host "✅ 分支重命名完成" -ForegroundColor Green

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "创建 GitHub 仓库" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

Write-Host "请按照以下步骤在 GitHub 网站创建仓库：" -ForegroundColor Yellow
Write-Host ""
Write-Host "1. 访问 " -NoNewline
Write-Host "https://github.com/new" -ForegroundColor Blue
Write-Host "2. Repository name: " -NoNewline
Write-Host "veildeploy" -ForegroundColor Green
Write-Host "3. Description: " -NoNewline
Write-Host "A secure, fast, and censorship-resistant VPN protocol" -ForegroundColor Green
Write-Host "4. 选择 " -NoNewline
Write-Host "Public" -ForegroundColor Green
Write-Host "5. " -NoNewline
Write-Host "不要勾选" -ForegroundColor Red -NoNewline
Write-Host " 任何选项（README, .gitignore, license）"
Write-Host "6. 点击 " -NoNewline
Write-Host "Create repository" -ForegroundColor Green
Write-Host ""

# 打开 GitHub 新建仓库页面
Write-Host "是否打开 GitHub 创建仓库页面？(Y/N) " -ForegroundColor Yellow -NoNewline
$openBrowser = Read-Host

if ($openBrowser -eq "Y" -or $openBrowser -eq "y") {
    Start-Process "https://github.com/new"
    Write-Host "✅ 已在浏览器中打开" -ForegroundColor Green
    Write-Host ""
    Write-Host "请在浏览器中完成仓库创建后继续..." -ForegroundColor Yellow
}

Write-Host ""
Write-Host "完成后，你会看到仓库地址，类似：" -ForegroundColor Yellow
Write-Host "https://github.com/your-username/veildeploy.git" -ForegroundColor Gray
Write-Host ""

$repoUrl = Read-Host "请输入你的仓库地址"

# 验证 URL 格式
if ($repoUrl -notmatch "^https://github\.com/.+/.+\.git$") {
    Write-Host ""
    Write-Host "⚠️  警告：URL 格式可能不正确" -ForegroundColor Yellow
    Write-Host "正确格式示例：https://github.com/username/veildeploy.git" -ForegroundColor Gray
    Write-Host ""
    $continue = Read-Host "是否继续？(Y/N)"
    if ($continue -ne "Y" -and $continue -ne "y") {
        Write-Host "已取消" -ForegroundColor Red
        exit 1
    }
}

Write-Host ""
Write-Host "[6/8] 添加远程仓库..." -ForegroundColor Yellow

git remote remove origin 2>$null
git remote add origin $repoUrl

if ($LASTEXITCODE -eq 0) {
    Write-Host "✅ 远程仓库添加完成" -ForegroundColor Green
} else {
    Write-Host "❌ 添加远程仓库失败" -ForegroundColor Red
    Read-Host "按回车键退出"
    exit 1
}

Write-Host ""
Write-Host "[7/8] 准备推送到 GitHub..." -ForegroundColor Yellow
Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "重要提示" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "如果提示输入密码，请使用 " -NoNewline
Write-Host "Personal Access Token" -ForegroundColor Green
Write-Host "而不是你的 GitHub 登录密码！" -ForegroundColor Red
Write-Host ""
Write-Host "如何获取 Token：" -ForegroundColor Yellow
Write-Host "1. 访问 https://github.com/settings/tokens" -ForegroundColor Gray
Write-Host "2. 点击 'Generate new token (classic)'" -ForegroundColor Gray
Write-Host "3. 输入名称：VeilDeploy Development" -ForegroundColor Gray
Write-Host "4. 勾选 'repo' 权限（包括所有子选项）" -ForegroundColor Gray
Write-Host "5. 点击 'Generate token'" -ForegroundColor Gray
Write-Host "6. 复制生成的 token（只显示一次！）" -ForegroundColor Gray
Write-Host ""

$openTokenPage = Read-Host "是否打开 Token 创建页面？(Y/N)"

if ($openTokenPage -eq "Y" -or $openTokenPage -eq "y") {
    Start-Process "https://github.com/settings/tokens/new"
    Write-Host "✅ 已在浏览器中打开" -ForegroundColor Green
    Write-Host ""
    Write-Host "请在浏览器中创建 Token，然后复制保存好" -ForegroundColor Yellow
    Write-Host ""
    Read-Host "准备好后按回车继续"
}

Write-Host ""
Write-Host "[8/8] 推送到 GitHub..." -ForegroundColor Yellow
Write-Host ""

git push -u origin main

if ($LASTEXITCODE -eq 0) {
    Write-Host ""
    Write-Host "========================================" -ForegroundColor Green
    Write-Host "🎉 成功！仓库已创建并推送到 GitHub！" -ForegroundColor Green
    Write-Host "========================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "仓库地址：" -NoNewline
    Write-Host "$repoUrl" -ForegroundColor Blue
    Write-Host ""
    Write-Host "你可以访问以下页面：" -ForegroundColor Yellow
    Write-Host "- 仓库主页：" -NoNewline
    Write-Host "$($repoUrl -replace '\.git$', '')" -ForegroundColor Blue
    Write-Host "- 提交历史：" -NoNewline
    Write-Host "$($repoUrl -replace '\.git$', '')/commits" -ForegroundColor Blue
    Write-Host ""
    Write-Host "下一步（可选）：" -ForegroundColor Yellow
    Write-Host "1. 创建 Release - 上传编译好的二进制文件" -ForegroundColor Gray
    Write-Host "2. 启用 GitHub Pages - 托管文档网站" -ForegroundColor Gray
    Write-Host "3. 添加 CI/CD - 自动化测试和构建" -ForegroundColor Gray
    Write-Host "4. 邀请协作者 - 团队开发" -ForegroundColor Gray
    Write-Host ""
    Write-Host "常用 Git 命令：" -ForegroundColor Yellow
    Write-Host "- 查看状态：  " -NoNewline -ForegroundColor Gray
    Write-Host "git status" -ForegroundColor White
    Write-Host "- 拉取更新：  " -NoNewline -ForegroundColor Gray
    Write-Host "git pull" -ForegroundColor White
    Write-Host "- 推送更改：  " -NoNewline -ForegroundColor Gray
    Write-Host "git add . && git commit -m 'message' && git push" -ForegroundColor White
    Write-Host "- 查看历史：  " -NoNewline -ForegroundColor Gray
    Write-Host "git log --oneline" -ForegroundColor White
    Write-Host ""

    # 询问是否打开仓库页面
    $openRepo = Read-Host "是否在浏览器中打开仓库？(Y/N)"
    if ($openRepo -eq "Y" -or $openRepo -eq "y") {
        Start-Process ($repoUrl -replace '\.git$', '')
        Write-Host "✅ 已在浏览器中打开" -ForegroundColor Green
    }

} else {
    Write-Host ""
    Write-Host "========================================" -ForegroundColor Red
    Write-Host "❌ 推送失败！" -ForegroundColor Red
    Write-Host "========================================" -ForegroundColor Red
    Write-Host ""
    Write-Host "可能的原因：" -ForegroundColor Yellow
    Write-Host "1. 密码错误（应使用 Personal Access Token，不是登录密码）" -ForegroundColor Gray
    Write-Host "2. 仓库地址错误" -ForegroundColor Gray
    Write-Host "3. 网络连接问题" -ForegroundColor Gray
    Write-Host "4. 权限不足" -ForegroundColor Gray
    Write-Host ""
    Write-Host "获取 Personal Access Token：" -ForegroundColor Yellow
    Write-Host "https://github.com/settings/tokens" -ForegroundColor Blue
    Write-Host ""
    Write-Host "手动推送命令：" -ForegroundColor Yellow
    Write-Host "git push -u origin main" -ForegroundColor White
    Write-Host ""
    Write-Host "或配置 SSH 密钥（更方便）：" -ForegroundColor Yellow
    Write-Host "1. ssh-keygen -t ed25519 -C 'your-email@example.com'" -ForegroundColor Gray
    Write-Host "2. 将 ~/.ssh/id_ed25519.pub 添加到 GitHub" -ForegroundColor Gray
    Write-Host "3. git remote set-url origin git@github.com:username/veildeploy.git" -ForegroundColor Gray
    Write-Host "4. git push -u origin main" -ForegroundColor Gray
    Write-Host ""
}

Write-Host ""
Read-Host "按回车键退出"
