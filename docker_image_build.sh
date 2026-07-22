#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")" && pwd)"
IMAGE="${1:-myobj:latest}"

if ! command -v docker >/dev/null 2>&1; then
    echo "错误：未找到 docker 命令。" >&2
    exit 1
fi

if ! docker buildx version >/dev/null 2>&1; then
    echo "错误：当前 Docker 不支持 buildx。" >&2
    exit 1
fi

if ! command -v npm >/dev/null 2>&1; then
    echo "错误：未找到 npm 命令，无法构建前端。" >&2
    exit 1
fi

echo "正在构建前端资源……"
(
    cd "${ROOT_DIR}/webview"
    npm run build:prod
)

echo "正在构建 Docker 镜像：${IMAGE}"
docker buildx build \
    --tag "${IMAGE}" \
    --load \
    "${ROOT_DIR}"

IMAGE_PLATFORM="$(docker image inspect "${IMAGE}" --format '{{.Os}}/{{.Architecture}}')"

echo "构建完成。"
echo "镜像：${IMAGE}"
echo "平台：${IMAGE_PLATFORM}"
