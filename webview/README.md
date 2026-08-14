![MyObj Logo](src/assets/images/LOGO.png)

# 🎨 MyObj Web 前端

MyObj Web 端使用 Vue 3、TypeScript、Vite、Element Plus 和 Pinia 构建，同时适配桌面与移动布局。该目录只包含前端源码；认证、文件数据、下载任务和插件执行均由 MyObj Go 服务端提供。

## ✨ 当前功能

- 📁 文件、目录、上传下载、回收站、分享和公开文件广场。
- 🏷️ 文件名关键词搜索、多标签筛选、标签云和个人标签显示偏好。
- 🚀 HTTP/HLS/Torrent 离线下载与上传、下载任务实时状态。
- 🎬 影视模式、最新视频、视频在线播放和缩略图管理。
- 🧩 WASM 插件、订阅计划和订阅执行记录。
- 🌐 用户资料、外观设置、中英文界面和响应式布局。
- 🛡️ 管理员用户、用户组、权限、磁盘、系统配置、插件和标签规则管理。

## 📋 环境要求

- Node.js `^20.19.0` 或 `>=22.12.0`。
- npm。
- 本地联调时需要 MyObj 后端默认运行在 `http://localhost:8080`。

## 🚀 开发与验证

```bash
npm install
npm run dev
```

开发服务器默认监听 `0.0.0.0:5173` 并自动打开浏览器。`/dev-api` 请求由 Vite 代理到 `VITE_APP_BASE_URL`，随后重写为后端 `/api`。

常用命令：

```bash
npm run build          # 普通生产构建
npm run build:prod     # 显式使用 production 模式
npm run preview        # 预览 dist
npm run test:unit      # Vitest 单元测试
npm run test:e2e       # Playwright 端到端测试
npm run type-check     # TypeScript 检查
npm run lint:check     # ESLint，只检查不修复
npm run format:check   # Prettier，只检查不改写
npm run code-check     # 类型、ESLint、Prettier 串行检查
npm run analyze        # 生产构建与产物分析
```

`npm run lint` 和 `npm run format` 会直接修改源码，审查工作区时应优先使用带 `:check` 的命令。

## ⚙️ 环境变量

| 变量 | 开发默认值 | 生产默认值 | 用途 |
| --- | --- | --- | --- |
| `VITE_APP_BASE_API` | `/dev-api` | `/api` | 浏览器请求使用的 API 前缀 |
| `VITE_APP_BASE_URL` | `http://localhost:8080` | `http://localhost:8080` | Vite 开发代理目标 |
| `VITE_APP_BASE_PATH` | `/` | `/` | Vite 构建和部署基础路径 |
| `VITE_APP_PORT` | `5173` | `5173` | 开发服务器和 HMR 端口 |
| `VITE_BUILD_COMPRESS` | 空 | `gzip` | 构建压缩格式，可用 `gzip`、`brotli` 或逗号组合 |
| `VITE_LOG_LEVEL` | `debug` | `error` | 前端日志级别 |
| `VITE_LOG_ENABLE` | `true` | `true` | 是否启用前端日志 |

生产环境默认使用同源 `/api`。如果前后端分开部署，需要让 Web 服务器代理 `/api`，或在构建前调整 `VITE_APP_BASE_API`；当前请求实现不会从 HTML 的 `api-url` meta 标签读取运行时地址。

## 📁 项目结构

```text
webview/
├── e2e/                  Playwright 端到端测试
├── public/               原样复制的静态资源
├── scripts/              构建分析脚本
├── src/
│   ├── api/              按业务域划分的 API 封装
│   ├── assets/           图片、图标与全局样式
│   ├── components/       公共组件
│   ├── composables/      组合式函数
│   ├── config/           API 等运行配置
│   ├── i18n/             中文和英文文案
│   ├── layout/           桌面与移动布局
│   ├── plugins/          缓存、日志等 Vue 插件
│   ├── router/           路由与访问控制
│   ├── stores/           Pinia 状态
│   ├── theme/            主题能力
│   ├── types/            TypeScript 类型
│   ├── utils/            网络、任务、文件和 UI 工具
│   ├── views/            文件、影视、离线下载、订阅、管理等页面
│   ├── App.vue
│   └── main.ts
├── vite/                 Vite 插件配置
├── package.json
├── vite.config.ts
├── vitest.config.ts
└── playwright.config.ts
```

HTTP 请求统一由 `src/utils/network/request.ts` 处理，API 前缀定义在 `src/config/api.ts`。会话 Token 从本地缓存读取并放入 `Authorization: Bearer ...`；只有后端明确返回会话缺失、失效、过期或撤销原因时才清除本地登录态。

实时任务使用 `GET /api/events` 的 Server-Sent Events。反向代理必须关闭该端点的缓冲和缓存，并设置足够长的读取超时。

## 📦 生产部署

推荐先生成静态资源，再由 Go 服务端从 `webview/dist` 提供页面：

```bash
npm run build:prod
cd ..
go run ./src/cmd/server/main.go
```

项目根目录的跨平台脚本和 Docker 镜像都会采用同一目录布局。若独立使用 Nginx 托管前端，可参考：

```nginx
server {
    listen 80;
    server_name example.com;
    root /srv/myobj/webview/dist;

    location / {
        try_files $uri $uri/ /index.html;
    }

    location = /api/events {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 75s;
        add_header X-Accel-Buffering no always;
    }

    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

完整项目构建、Docker、WebDAV 和 SDK 文档请返回查看[根 README](../README.md)。
