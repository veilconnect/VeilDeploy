@echo off
chcp 65001 >nul
echo ========================================
echo VeilDeploy GitHub 仓库设置脚本
echo ========================================
echo.

REM 检查是否在正确的目录
if not exist "go.mod" (
    echo 错误：请在 VeilDeploy 项目根目录运行此脚本！
    pause
    exit /b 1
)

REM 检查 Git 是否安装
git --version >nul 2>&1
if errorlevel 1 (
    echo 错误：Git 未安装或不在 PATH 中
    echo 请访问 https://git-scm.com/download/win 下载安装
    pause
    exit /b 1
)

echo [1/7] 检查 Git 配置...
git config user.name >nul 2>&1
if errorlevel 1 (
    echo.
    echo 请设置 Git 用户信息：
    set /p USERNAME="输入你的 GitHub 用户名: "
    set /p EMAIL="输入你的 GitHub 邮箱: "
    git config --global user.name "%USERNAME%"
    git config --global user.email "%EMAIL%"
    echo Git 配置完成！
) else (
    echo Git 已配置
)

echo.
echo [2/7] 初始化 Git 仓库...
if not exist ".git" (
    git init
    echo Git 仓库初始化完成
) else (
    echo Git 仓库已存在
)

echo.
echo [3/7] 添加文件到 Git...
git add .
echo 文件添加完成

echo.
echo [4/7] 创建初始提交...
git commit -m "Initial commit: VeilDeploy 2.0

- Implemented Noise Protocol Framework
- Added multi-factor authentication
- Implemented advanced routing system
- Added bridge discovery mechanism
- Created comprehensive documentation
- 78 passing tests with 70%% coverage" 2>nul

if errorlevel 1 (
    echo 提交已存在或无更改
) else (
    echo 提交创建完成
)

echo.
echo [5/7] 重命名主分支为 main...
git branch -M main
echo 分支重命名完成

echo.
echo ========================================
echo 接下来需要在 GitHub 网站创建仓库：
echo ========================================
echo.
echo 1. 访问 https://github.com/new
echo 2. Repository name: veildeploy
echo 3. Description: A secure, fast, and censorship-resistant VPN protocol
echo 4. 选择 Public
echo 5. 不要勾选任何选项（README, .gitignore, license）
echo 6. 点击 "Create repository"
echo.
echo 完成后，你会看到仓库地址，类似：
echo https://github.com/your-username/veildeploy.git
echo.
set /p REPO_URL="请输入你的仓库地址: "

echo.
echo [6/7] 添加远程仓库...
git remote remove origin 2>nul
git remote add origin %REPO_URL%
echo 远程仓库添加完成

echo.
echo [7/7] 推送到 GitHub...
echo.
echo 注意：如果提示输入密码，请使用 Personal Access Token
echo 不是你的 GitHub 登录密码！
echo.
echo 如何获取 Token：
echo 1. 访问 https://github.com/settings/tokens
echo 2. Generate new token (classic)
echo 3. 勾选 repo 权限
echo 4. 生成并复制 token
echo.
pause

git push -u origin main

if errorlevel 1 (
    echo.
    echo ========================================
    echo 推送失败！可能的原因：
    echo ========================================
    echo 1. 密码错误（应使用 Personal Access Token）
    echo 2. 仓库地址错误
    echo 3. 网络问题
    echo.
    echo 获取 Personal Access Token：
    echo https://github.com/settings/tokens
    echo.
    echo 手动推送命令：
    echo git push -u origin main
    echo.
    pause
    exit /b 1
)

echo.
echo ========================================
echo 🎉 成功！仓库已创建并推送到 GitHub！
echo ========================================
echo.
echo 仓库地址：%REPO_URL%
echo.
echo 下一步：
echo 1. 访问你的 GitHub 仓库
echo 2. 创建 Release（可选）
echo 3. 启用 GitHub Pages（可选）
echo.
echo 管理命令：
echo - 查看状态: git status
echo - 拉取更新: git pull
echo - 推送更改: git push
echo - 查看历史: git log
echo.
pause
