# syntax=docker/dockerfile:1.7

# 前端构建固定使用构建机平台，避免跨平台构建时通过 QEMU 运行 Node.js。
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend-builder

WORKDIR /build/webview

# 先按锁文件安装依赖，使源码变化可以复用依赖缓存。
COPY webview/package.json webview/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm,sharing=locked \
    npm ci

COPY webview/ ./
RUN npm run build:prod

# 构建阶段固定使用构建机平台，通过 Go 原生交叉编译生成目标平台二进制，
# 避免在 amd64 构建机上通过 QEMU 运行 arm64 编译器。
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
# 默认优先使用国内 Go 模块代理；构建时仍可通过 --build-arg GOPROXY=... 覆盖。
ARG GOPROXY=https://goproxy.cn|https://proxy.golang.org|direct

# 设置工作目录
WORKDIR /build

# 下载私有仓库或回退到 VCS 下载依赖时需要 Git。
RUN apk add --no-cache git

# 复制 go mod 文件
COPY go.mod go.sum ./

# 下载依赖，并让模块缓存在不同构建之间复用。
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    GOPROXY="${GOPROXY}" go mod download

# 仅复制 Go 源码和编译期依赖的 Swagger Go 包，前端产物变化不会使
# Go 编译缓存失效。
COPY src ./src
COPY docs ./docs

# 项目使用纯 Go SQLite 驱动，无需 CGO。复用模块和编译缓存，且不再使用
# 强制全量重编译的 -a 参数。
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -buildvcs=false -o /out/myobj ./src/cmd/server && \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -buildvcs=false -o /out/myobj-cli ./src/cmd/cli

# 运行镜像需要的静态资源不参与 Go 编译缓存计算。
COPY templates ./templates

# 运行阶段
FROM alpine:latest

# 设置工作目录
WORKDIR /app

# 安装运行时依赖
RUN apk add --no-cache \
	ca-certificates \
	tzdata \
	ffmpeg

# 设置时区为上海
ENV TZ=Asia/Shanghai

# 创建必要的目录
RUN mkdir -p /app/logs \
    /app/libs \
    /app/obj_data \
    /app/obj_temp \
    /app/webview/dist

# 从构建阶段复制可执行文件
COPY --from=builder /out/myobj .
COPY --from=builder /out/myobj-cli .

# 复制前端静态文件
COPY --from=frontend-builder /build/webview/dist ./webview/dist

# 复制模板文件
COPY --from=builder /build/templates ./templates

# 复制 docs 目录（Swagger 文档）
COPY --from=builder /build/docs ./docs

# 暴露端口
# 8080: HTTP服务端口
# 8081: WebDAV服务端口
EXPOSE 8080 8081

# 设置挂载点
VOLUME ["/app/config.toml", "/app/logs", "/app/libs", "/app/obj_data", "/app/obj_temp"]

# 启动应用
CMD ["./myobj"]
