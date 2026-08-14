# 🧰 MyObj 跨平台构建脚本

本目录提供 Windows、Linux 和 macOS 构建机到 Windows、Linux 和 macOS 目标平台的脚本。当前所有脚本固定输出 `amd64` 产物，并在每次运行时重建整个 `bin/` 目录。

## 📋 环境要求

- Go 1.25 或更高版本。
- Node.js `^20.19.0` 或 `>=22.12.0`，以及 npm。
- 首次构建需要网络下载 Go 和 npm 依赖。
- 运行 `.sh` 脚本需要 Bash。

## 🗺️ 脚本矩阵

| 构建机 | Windows 目标 | Linux 目标 | macOS 目标 |
| --- | --- | --- | --- |
| Windows | `windows-build-windows.bat` | `windows-build-linux.bat` | `windows-build-mac.bat` |
| Linux | `linux-build-windows.sh` | `linux-build-linux.sh` | `linux-build-mac.sh` |
| macOS | `mac-build-windows.sh` | `mac-build-linux.sh` | `mac-build-mac.sh` |

从本目录执行脚本。例如：

```powershell
cd builds
.\windows-build-windows.bat
```

```bash
cd builds
chmod +x *.sh
./linux-build-linux.sh
```

## 🏗️ 构建流程

每个脚本都会：

1. 删除并重新创建项目根目录的 `bin/`。
2. 在 `webview/` 执行 `npm install` 和 `npm run build`。
3. 将 `webview/dist` 复制到 `bin/webview/dist`。
4. 构建 Go 服务端和 CLI，使用 `-ldflags="-s -w"` 减小体积。
5. 复制 `libs`、`templates`、`docs` 和 `config.toml`，并生成 `README.txt`。

除 Windows 构建 Windows 的脚本设置 `CGO_ENABLED=1` 外，其余脚本都设置 `CGO_ENABLED=0`。项目使用纯 Go SQLite 驱动，跨平台产物不依赖目标系统的 C 工具链。

## 📦 输出内容

```text
bin/
├── server 或 server.exe    服务端程序
├── cli 或 cli.exe          CLI 管理工具
├── webview/dist/           前端静态资源
├── libs/                   数据库目录及仓库中已有内容
├── templates/              HTML 模板
├── docs/                   Swagger 与使用文档
├── config.toml             配置文件
└── README.txt              目标平台启动说明
```

脚本会复制仓库当前的 `libs/` 内容。如果构建工作区含有本地数据库或其他运行数据，发布前必须检查 `bin/libs/`，避免把开发数据带入发布包。

## 🚀 部署

将完整 `bin/` 目录复制到目标机器，先修改 `config.toml`，再启动：

```powershell
.\server.exe
```

```bash
chmod +x server cli
./server
```

默认 HTTP 地址为 `http://localhost:8080`，WebDAV 地址为 `http://localhost:8081/dav`。数据库、日志、文件和临时目录都按 `config.toml` 中的相对路径解析，因此应从发布目录启动服务。

## 🔧 常见问题

### 1. 🎨 前端依赖或构建失败

```bash
cd webview
npm install
npm run build
```

确认 Node.js 满足 Vite 7 的版本要求，并优先保留命令输出中的第一个错误。

### 2. 🐹 Go 依赖或编译失败

```bash
go mod download
go test ./...
```

不建议仅为解决构建问题执行 `go mod tidy`，因为它会修改 `go.mod` 和 `go.sum`。

### 3. 💻 目标平台无法运行

- 确认目标系统为 `amd64`；现有脚本不生成 `arm64`。
- 确认选用了正确的 `GOOS` 目标脚本。
- Linux/macOS 需要为二进制添加执行权限。
- 前端空白时检查 `webview/dist/index.html` 和 `webview/dist/assets` 是否完整。

更多运行、Docker 和验证说明见[项目根 README](../README.md)。
