#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")" && pwd)"
PLATFORM="linux/arm64"
IMAGE="${1:-myobj:latest}"
OUTPUT_FILE="${2:-${ROOT_DIR}/myobj-linux-arm64.tar}"

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

echo "正在构建 ${PLATFORM} 镜像：${IMAGE}"
OUTPUT_DIR="$(dirname "${OUTPUT_FILE}")"
mkdir -p "${OUTPUT_DIR}"

TEMP_FILE="${OUTPUT_FILE}.tmp"
rm -f "${TEMP_FILE}"
trap 'rm -f "${TEMP_FILE}"' 0

docker buildx build \
    --platform "${PLATFORM}" \
    --tag "${IMAGE}" \
    --output "type=docker,dest=${TEMP_FILE}" \
    "${ROOT_DIR}"

if [ ! -s "${TEMP_FILE}" ]; then
    echo "错误：镜像导出文件为空。" >&2
    exit 1
fi

mv -f "${TEMP_FILE}" "${OUTPUT_FILE}"
trap - 0

echo "构建完成。"
echo "镜像：${IMAGE}"
echo "平台：${PLATFORM}"
echo "文件：${OUTPUT_FILE}"
