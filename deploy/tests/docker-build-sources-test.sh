#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

fail() {
  printf 'docker build source configuration test failed: %s\n' "$1" >&2
  exit 1
}

assert_line() {
  file=$1
  line=$2
  grep -Fqx "$line" "$file" || fail "$file is missing: $line"
}

# Local builds should use domestic mirrors unless callers override a build arg.
assert_line Dockerfile 'ARG GOPROXY=https://goproxy.cn,direct'
assert_line Dockerfile 'ARG GOSUMDB=sum.golang.google.cn'
assert_line Dockerfile 'ARG NPM_CONFIG_REGISTRY=https://registry.npmmirror.com'
assert_line Dockerfile 'ARG COREPACK_NPM_REGISTRY=https://registry.npmmirror.com'
assert_line Dockerfile 'ARG PIP_INDEX_URL=https://pypi.tuna.tsinghua.edu.cn/simple'
assert_line Dockerfile 'ARG UV_INDEX_URL=https://pypi.tuna.tsinghua.edu.cn/simple'
assert_line Dockerfile 'ARG APT_MIRROR=https://mirrors.aliyun.com/debian'
assert_line Dockerfile 'ARG ALPINE_REPOSITORY=https://mirrors.aliyun.com/alpine'

# GitHub-hosted runners must use official sources and branch-scoped BuildKit
# cache entries so image layers are not evicted by unrelated release branches.
workflow=.github/workflows/docker-image.yml
assert_line "$workflow" '            GOPROXY=https://proxy.golang.org,direct'
assert_line "$workflow" '            GOSUMDB=sum.golang.org'
assert_line "$workflow" '            NPM_CONFIG_REGISTRY=https://registry.npmjs.org'
assert_line "$workflow" '            COREPACK_NPM_REGISTRY=https://registry.npmjs.org'
assert_line "$workflow" '            PIP_INDEX_URL=https://pypi.org/simple'
assert_line "$workflow" '            UV_INDEX_URL=https://pypi.org/simple'
assert_line "$workflow" '            APT_MIRROR=https://deb.debian.org/debian'
assert_line "$workflow" '            ALPINE_REPOSITORY=https://dl-cdn.alpinelinux.org/alpine'
assert_line "$workflow" '          cache-from: type=gha,scope=sub2api-docker-image-${{ github.ref_name }}'
assert_line "$workflow" '          cache-to: type=gha,mode=max,scope=sub2api-docker-image-${{ github.ref_name }}'

grep -Fq 'RUN --mount=type=cache,id=sub2api-pnpm-store' Dockerfile || fail 'pnpm cache mount is missing'
grep -Fq 'RUN --mount=type=cache,id=sub2api-gomod' Dockerfile || fail 'Go module cache mount is missing'
grep -Fq 'id=sub2api-gobuild,target=/root/.cache/go-build' Dockerfile || fail 'Go build cache mount is missing'

printf 'docker build source configuration test passed\n'
