#!/usr/bin/env bash
# 外层只分配一次打包标识，避免 Docker 的不同架构分别取号。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BUILD_INFO="$(mktemp)"
trap 'rm -f "$BUILD_INFO"' EXIT
VERSION="$(python3 "${REPO_ROOT}/backend/scripts/package-version.py" --output "$BUILD_INFO")"
COMMIT="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["source_sha"])' "$BUILD_INFO")"
DATE="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["built_at"])' "$BUILD_INFO")"
UPSTREAM="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["upstream_version"])' "$BUILD_INFO")"

docker build -t "sub2api:v${VERSION}" -t sub2api:latest \
    --label "shan.source_sha=${COMMIT}" \
    --build-arg "VERSION=${VERSION}" \
    --build-arg "COMMIT=${COMMIT}" \
    --build-arg "DATE=${DATE}" \
    --build-arg "UPSTREAM_VERSION=${UPSTREAM}" \
    --build-arg GOPROXY=https://goproxy.cn,direct \
    --build-arg GOSUMDB=sum.golang.google.cn \
    -f "${REPO_ROOT}/Dockerfile" \
    "${REPO_ROOT}"
