#!/bin/sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
BACKEND_DIR="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(CDPATH= cd -- "$BACKEND_DIR/.." && pwd)"
VERSION_FILE="$BACKEND_DIR/cmd/server/VERSION"

# 打包分支的标签或 VERSION 可能携带 -ent.YYYYMMDD-序号 打包后缀；
# 外层打包入口需要不含后缀的上游版本 X.Y.Z，这里统一剥离。
strip_packaging_suffix() {
  printf '%s\n' "$1" | sed -E 's/-(ent|ob)\.[0-9]{8}(-.*)?$//'
}

# Prefer the exact release tag when building from a tagged checkout so
# source builds from vX.Y.Z don't inherit the previous VERSION file value.
if command -v git >/dev/null 2>&1; then
  TAG="$(
    git -C "$REPO_DIR" describe --tags --exact-match --match 'v[0-9]*' 2>/dev/null || \
    git -C "$REPO_DIR" describe --tags --exact-match --match '[0-9]*' 2>/dev/null || \
    true
  )"
  if [ -n "$TAG" ]; then
    printf '%s\n' "$(strip_packaging_suffix "${TAG#v}")"
    exit 0
  fi
fi

printf '%s\n' "$(strip_packaging_suffix "$(tr -d '\r\n' < "$VERSION_FILE")")"
