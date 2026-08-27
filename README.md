<div align="center">

# 🌟 MyObj

现代化的私有云文件管理系统

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Vue Version](https://img.shields.io/badge/Vue-3.5+-4FC08D?style=flat&logo=vue.js)](https://vuejs.org/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

[功能概览](#-功能概览) · [快速开始](#-快速开始) · [部署与构建](#-部署与构建) · [CLI](#-cli-管理工具) · [测试](#-测试)

[GitHub](https://github.com/H2SYJ/MyObj)

</div>

## 📖 项目简介

MyObj 是一个面向个人和家庭场景的开源私有云文件管理系统，后端使用 Go，Web 端使用 Vue 3。系统提供虚拟目录、多用户隔离、分片上传、离线下载、文件分享、WebDAV、标签管理、视频在线播放和 SQLite/MySQL 双数据库支持。

当前仓库交付的是 Go 服务端、Web 静态资源、CLI 工具和 SDK，不包含原生 Android、iOS 或桌面客户端。

## 🎯 功能概览

- 📁 文件与目录：上传、秒传、断点续传、下载、移动、复制、重命名、回收站、批量操作和公开文件广场。
- 🏷️ 搜索与标签：文件名关键词搜索、全部/任一多标签筛选、统一标签云、自动标签和管理员全局规则。
- 🎬 下载与媒体：HTTP、HLS、Torrent 离线下载，图片/视频在线播放，图片和视频缩略图，影视模式与最新视频列表。
- 🔐 分享与访问：限时分享、密码保护、API Key、JWT 会话和 WebDAV。
- 🧩 插件与订阅：TinyGo/WASI 订阅插件、插件安装审计、权限授权、定时订阅和离线下载联动。
- 👥 管理能力：用户、用户组、权限、磁盘、系统配置、插件和标签规则管理。
- 🗄️ 数据与部署：SQLite 或 MySQL、Redis 缓存、跨平台构建脚本、Docker Compose 和 SQLite 到 MySQL 迁移工具。
- 🖥️ Web 界面：桌面端与移动端响应式布局、中英文界面、主题切换和实时任务事件。

自动标签使用纯 Go 分词实现，不影响 `CGO_ENABLED=0` 构建。图片元数据由内置 Provider 提取；安装 `ffprobe` 后可额外提取音视频时长、分辨率、编码和容器信息。手工标签默认私有，只有明确设为公开时才会随公开文件展示。

## 🛠️ 技术栈

| 层次 | 当前实现 |
| --- | --- |
| 后端 | Go 1.25、Gin 1.11、GORM 1.31 |
| 数据库 | SQLite 3、MySQL 5.7+ |
| 缓存 | Redis 或本地缓存 |
| 前端 | Vue 3.5、TypeScript 5.9、Vite 7.2、Element Plus 2.11、Pinia 3 |
| 媒体 | xgplayer、FFmpeg/ffprobe（可选） |
| 插件 | wazero、TinyGo WASI ABI v2 |
| 接口文档 | Swagger 2.0 |

## 📋 环境要求

开发环境：

- Go 1.25 或更高版本。
- Node.js `^20.19.0` 或 `>=22.12.0`，以及 npm。
- Git。

按需安装：

- Redis：使用 `cache.type="redis"` 时需要。
- MySQL 5.7+：使用 MySQL 数据库时需要。
- FFmpeg/ffprobe：生成视频缩略图和提取音视频元数据时需要；Docker 镜像已包含。
- TinyGo：开发 WASM 订阅插件时需要。
- Docker、Compose 和 buildx：使用容器部署或构建镜像时需要。

## 🚀 快速开始

### 1. 📥 获取源码

```bash
git clone https://github.com/H2SYJ/MyObj.git
cd MyObj
```

### 2. ⚙️ 检查配置

项目默认配置位于 [`config.toml`](config.toml)，默认使用：

- HTTP：`0.0.0.0:8080`
- WebDAV：`0.0.0.0:8081/dav`
- SQLite：`./libs/my_obj.db`
- Redis：`redis:6379`
- 文件目录：`./obj_data`
- 临时目录：`./obj_temp`

本机开发若没有名为 `redis` 的容器或主机，请将缓存改为可连接的 Redis 地址，或按项目支持的本地缓存配置运行。生产部署前必须替换 `[auth].secret`，并按实际环境限制 CORS、监听地址和端口。

数据库表会在服务首次启动时自动迁移。使用 MySQL 时需要先创建目标数据库，再修改 `[database]`；不要同时保留两组启用状态的数据库配置。

### 3. ▶️ 启动后端

```bash
go mod download
go run ./src/cmd/server/main.go
```

### 4. 🎨 启动前端开发服务器

```bash
cd webview
npm install
npm run dev
```

前端默认访问 `http://localhost:5173`，开发代理会把 `/dev-api` 重写为后端的 `/api`。

启动后可访问：

- Web：`http://localhost:8080`
- Swagger：`http://localhost:8080/swagger/index.html`
- WebDAV：`http://localhost:8081/dav`

前端开发服务器与后端静态资源服务是两种入口；修改前端后若要通过 `8080` 查看，需要重新生成 `webview/dist`。

## 📦 部署与构建

### 🧰 跨平台发布目录

`builds/` 中的脚本会构建前端、服务端和 CLI，并把配置、模板、Swagger 文档及静态资源集中到 `bin/`：

```powershell
cd builds
.\windows-build-windows.bat
```

Linux 或 macOS 示例：

```bash
cd builds
chmod +x *.sh
./linux-build-linux.sh
```

所有现有脚本输出 `amd64` 产物。完整矩阵和输出说明见 [builds/README.md](builds/README.md)。

### 🐳 Docker

Dockerfile 会在镜像内依次构建前端和 Go 服务端，使用 Compose 可直接构建并启动：

```bash
docker compose up -d --build
```

`docker_image_build.sh myobj:latest` 可用于构建独立镜像，脚本通过 buildx 构建本机平台镜像，前端资源也由 Dockerfile 自动生成。Compose 默认启动 MyObj 和 Redis；MySQL 服务模板默认注释。宿主机不需要预先安装前端依赖或生成 `webview/dist`。详细挂载、反向代理和排错说明见 [DOCKER_DEPLOY.md](DOCKER_DEPLOY.md)。

### 📥 预编译版本

可从 [GitHub Releases](https://github.com/H2SYJ/MyObj/releases) 获取已发布产物。解压后检查 `config.toml`，再运行 `server` 或 `server.exe`。

## 🔧 CLI 管理工具

### 🏗️ 构建

```bash
go build -buildvcs=false -o myobj-cli ./src/cmd/cli
```

PowerShell：

```powershell
go build -buildvcs=false -o myobj-cli.exe .\src\cmd\cli
```

### 📚 命令范围

```text
myobj-cli user ...        用户查询、密码、用户组、封禁和会话管理
myobj-cli group ...       用户组查询
myobj-cli system ...      系统信息与统计
myobj-cli plugin ...      插件打包与校验
myobj-cli thumbnail ...   历史视频缩略图补齐
myobj-cli database ...    SQLite 到 MySQL 迁移与校验
```

以当前二进制的 `--help` 为准：

```bash
./myobj-cli --help
./myobj-cli database --help
./myobj-cli plugin --help
```

### 🗃️ SQLite 迁移到 MySQL

迁移只复制数据库元数据，不移动 `obj_data` 中的实体文件。迁移前必须停止所有 MyObj 实例和后台任务，目标 MySQL 必须是空库并使用 `utf8mb4_unicode_ci`。

```powershell
$env:MYOBJ_MIGRATE_MYSQL_DSN = "myobj:密码@tcp(mysql:3306)/my_obj?charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai"

.\myobj-cli.exe database migrate-sqlite-to-mysql `
  --source .\libs\my_obj.db `
  --batch-size 1000 `
  --dry-run
```

预演通过后再使用 `--yes` 正式迁移。迁移会创建 SQLite 一致性快照，并校验表行数、数据摘要、关键关联和自增值。失败后应保留原 SQLite 和快照，重建空 MySQL 库后重试；当前不支持向已有数据库合并或断点续跑。

### 🖼️ 历史视频缩略图

```bash
./myobj-cli thumbnail backfill --dry-run
./myobj-cli thumbnail backfill --concurrency 2
```

该命令只处理未加密视频，跳过已有有效缩略图，并要求 `file.thumbnail=true`。宿主机需要可用的 FFmpeg/ffprobe；Docker 镜像已包含这些工具。

## 🔌 API、WebDAV 与插件

- Swagger：服务启动后访问 `http://localhost:8080/swagger/index.html`。
- WebDAV：[docs/WEBDAV_USAGE.md](docs/WEBDAV_USAGE.md)。
- 插件开发：[docs/plugin-development.md](docs/plugin-development.md)。
- TinyGo SDK：[sdk/tinygo/README.md](sdk/tinygo/README.md)。
- Python SDK：[sdk/python/README.md](sdk/python/README.md)。

## 📁 项目结构

```text
MyObj/
├── src/
│   ├── cmd/                 服务端和 CLI 入口
│   ├── config/              配置加载
│   ├── core/                领域对象与业务服务
│   ├── internal/            HTTP API 与数据访问实现
│   ├── pkg/                 认证、下载、插件、标签、上传、WebDAV 等模块
│   └── tests/               早期集成与基准测试
├── webview/                 Vue 3 + TypeScript 前端
├── sdk/
│   ├── python/              Python SDK
│   └── tinygo/              TinyGo 插件 SDK
├── builds/                  amd64 跨平台构建脚本
├── docs/                    Swagger、WebDAV 和插件文档
├── templates/               分享与错误页模板
├── docker-compose.yml
├── Dockerfile
└── config.toml
```

Go 测试既分布在各包内，也有一部分位于 `src/tests/`；前端单元测试与页面代码就近存放，端到端测试位于 `webview/e2e/`。

## 🧪 测试

后端：

```bash
go test ./...
go vet ./...
```

前端：

```bash
cd webview
npm run test:unit
npm run type-check
npm run lint:check
npm run format:check
npm run build:prod
```

需要浏览器环境时再运行：

```bash
npm run test:e2e
```

SDK：

```bash
cd sdk/tinygo
go test ./...
cd ../..
python -m unittest discover -s sdk/python/tests -v
```

MySQL 专用迁移测试需要显式提供测试 DSN；未配置时，相关用例会跳过，不能据此视为已验证真实 MySQL 行为。

## 🤝 贡献

提交代码前请同步更新受影响的文档，并至少运行与改动范围对应的测试。Bug 和功能建议可提交到 [GitHub Issues](https://github.com/H2SYJ/MyObj/issues)。

## 📄 许可证

本项目采用 [Apache License 2.0](LICENSE)。
