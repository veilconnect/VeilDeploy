# GitHub 仓库快速创建指南

## 🚀 最快方式（推荐）

### 步骤 1：运行自动化脚本

在项目根目录 `D:\web\veildeploy\` 打开 PowerShell 或命令提示符。

**PowerShell（推荐）：**
```powershell
.\scripts\setup-github.ps1
```

**命令提示符：**
```cmd
.\scripts\setup-github.bat
```

### 步骤 2：跟随脚本提示操作

脚本会自动：
1. ✅ 检查 Git 配置
2. ✅ 初始化 Git 仓库
3. ✅ 添加所有文件
4. ✅ 创建初始提交
5. ✅ 打开 GitHub 创建仓库页面
6. ✅ 添加远程仓库
7. ✅ 推送代码到 GitHub

你只需要：
- 在 GitHub 网站创建仓库（脚本会自动打开页面）
- 输入仓库地址
- 输入 GitHub 用户名/Token

### 步骤 3：完成！

推送成功后，访问你的 GitHub 仓库查看代码。

---

## 📝 手动创建（理解每一步）

如果你想手动操作以理解每个步骤：

### 1. 在 GitHub 网站创建仓库

1. 访问 https://github.com/new
2. 填写信息：
   - **Repository name**: `veildeploy`
   - **Description**: `A secure, fast, and censorship-resistant VPN protocol`
   - **Visibility**: Public
   - **不要勾选**任何初始化选项
3. 点击 "Create repository"
4. 复制仓库地址（例如：`https://github.com/your-username/veildeploy.git`）

### 2. 在本地初始化 Git

```bash
cd D:\web\veildeploy

# 初始化 Git 仓库
git init

# 配置用户信息（如果还没配置）
git config --global user.name "Your Name"
git config --global user.email "your-email@example.com"

# 添加所有文件
git add .

# 创建初始提交
git commit -m "Initial commit: VeilDeploy 2.0"

# 重命名分支为 main
git branch -M main
```

### 3. 连接远程仓库并推送

```bash
# 添加远程仓库（替换为你的仓库地址）
git remote add origin https://github.com/your-username/veildeploy.git

# 推送代码
git push -u origin main
```

如果提示输入密码，使用 **Personal Access Token**（不是登录密码）。

### 4. 获取 Personal Access Token

1. 访问 https://github.com/settings/tokens
2. 点击 "Generate new token (classic)"
3. 设置：
   - **Note**: VeilDeploy Development
   - **Expiration**: 90 days（或更长）
   - **Scopes**: 勾选 `repo`（所有子选项）
4. 点击 "Generate token"
5. **复制 token**（只显示一次！）

推送时使用：
- **Username**: 你的 GitHub 用户名
- **Password**: 粘贴刚才复制的 token

---

## ⚠️ 常见问题

### Q1: 运行脚本提示 "无法识别的命令"？

**A:** 确保在项目根目录运行脚本：

```powershell
# 切换到项目目录
cd D:\web\veildeploy

# 然后运行脚本
.\scripts\setup-github.ps1
```

### Q2: PowerShell 提示 "无法加载脚本"？

**A:** 需要允许脚本执行：

```powershell
# 以管理员身份运行 PowerShell
Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser

# 然后再运行脚本
.\scripts\setup-github.ps1
```

### Q3: git push 失败，提示 authentication failed？

**A:** 使用 Personal Access Token，不是密码！

1. 获取 Token：https://github.com/settings/tokens
2. 推送时 Password 字段输入 Token

或者配置 SSH 密钥（更方便）：

```bash
# 生成 SSH 密钥
ssh-keygen -t ed25519 -C "your-email@example.com"

# 复制公钥
cat ~/.ssh/id_ed25519.pub

# 在 GitHub 添加 SSH Key：
# https://github.com/settings/keys

# 更改远程仓库 URL
git remote set-url origin git@github.com:your-username/veildeploy.git

# 推送
git push -u origin main
```

### Q4: 已经创建了仓库，如何重新推送？

**A:** 如果之前推送失败：

```bash
# 检查远程仓库
git remote -v

# 如果地址不对，删除并重新添加
git remote remove origin
git remote add origin https://github.com/your-username/veildeploy.git

# 推送
git push -u origin main
```

### Q5: 想要取消某些文件的提交怎么办？

**A:** 编辑 `.gitignore` 文件，然后：

```bash
# 停止追踪文件但保留本地
git rm --cached filename

# 重新提交
git commit -m "Update .gitignore"
git push
```

---

## 📚 后续操作

### 创建 Release（发布版本）

编译好二进制文件后：

1. 访问仓库 → Releases → "Create a new release"
2. Tag version: `v2.0.0`
3. Release title: `VeilDeploy 2.0.0 - Initial Release`
4. 上传编译好的文件
5. 点击 "Publish release"

### 编译二进制文件

```powershell
# 创建 release 目录
mkdir release

# Linux AMD64
$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o release/veildeploy-linux-amd64 .

# Linux ARM64
$env:GOOS="linux"; $env:GOARCH="arm64"; go build -o release/veildeploy-linux-arm64 .

# Windows AMD64
$env:GOOS="windows"; $env:GOARCH="amd64"; go build -o release/veildeploy-windows-amd64.exe .

# macOS AMD64
$env:GOOS="darwin"; $env:GOARCH="amd64"; go build -o release/veildeploy-darwin-amd64 .

# macOS ARM64
$env:GOOS="darwin"; $env:GOARCH="arm64"; go build -o release/veildeploy-darwin-arm64 .

# 打包
cd release
tar -czf veildeploy-linux-amd64.tar.gz veildeploy-linux-amd64
tar -czf veildeploy-linux-arm64.tar.gz veildeploy-linux-arm64
# Windows 需要 7-Zip 或其他压缩工具创建 .zip
```

### 启用 GitHub Pages

如果想托管文档网站：

1. 访问仓库 → Settings → Pages
2. Source: 选择 `main` 分支，`/docs` 目录
3. 点击 Save
4. 访问 `https://your-username.github.io/veildeploy/`

---

## 🎯 快速参考

### Git 常用命令

```bash
# 查看状态
git status

# 添加文件
git add .
git add filename

# 提交更改
git commit -m "commit message"

# 推送到远程
git push

# 拉取最新代码
git pull

# 查看提交历史
git log
git log --oneline

# 创建分支
git checkout -b feature-name

# 切换分支
git checkout main

# 合并分支
git merge feature-name

# 查看远程仓库
git remote -v

# 撤销更改（未提交）
git checkout -- filename

# 撤销提交（保留更改）
git reset --soft HEAD~1

# 撤销提交（丢弃更改）
git reset --hard HEAD~1
```

### 仓库地址格式

```
HTTPS: https://github.com/username/veildeploy.git
SSH:   git@github.com:username/veildeploy.git
```

### 重要链接

- **创建仓库**: https://github.com/new
- **Personal Access Tokens**: https://github.com/settings/tokens
- **SSH Keys**: https://github.com/settings/keys
- **Git 下载**: https://git-scm.com/download/win

---

## ✅ 完成检查清单

创建 GitHub 仓库后，确认以下内容：

- [ ] 仓库已在 GitHub 创建
- [ ] 代码已成功推送
- [ ] README.md 正确显示
- [ ] LICENSE 文件存在
- [ ] .gitignore 生效（敏感文件未上传）
- [ ] 所有文档文件都在
- [ ] 可以访问仓库页面

可选：
- [ ] 创建了 Release
- [ ] 上传了二进制文件
- [ ] 启用了 GitHub Pages
- [ ] 添加了仓库描述和标签

---

## 🆘 需要帮助？

如果遇到问题：

1. **查看错误信息**: Git 的错误信息通常很明确
2. **检查网络**: 确保能访问 GitHub
3. **验证配置**: `git config --list`
4. **重新运行脚本**: 脚本可以多次运行
5. **手动操作**: 参考本文档的手动步骤

**常见错误代码：**
- `fatal: not a git repository`: 不在项目目录或未初始化
- `fatal: remote origin already exists`: 删除后重新添加 `git remote remove origin`
- `fatal: Authentication failed`: 使用 Token 而不是密码

---

好了，现在你可以运行脚本创建 GitHub 仓库了！🚀

选择一种方式：
1. **自动化**：`.\scripts\setup-github.ps1`（推荐）
2. **手动**：跟随本文档的手动步骤

祝顺利！
