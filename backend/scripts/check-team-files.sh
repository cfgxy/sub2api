#!/bin/bash
# 团队文件清单守卫：PR 改动的文件若不在团队清单内（= 上游原产文件），告警退出。
# 清单基线 = fork 基点 ab99d56e..main 的团队改动文件 + SHAN-356 起新增文件。
# 用法: check-team-files.sh [base-ref]   （CI 传目标分支名，本地缺省用 fork 基点 SHA）
set -euo pipefail

FORK_BASE=ab99d56e9626e6cd731592dae8553c9758a0efa2
BASE="${1:-$FORK_BASE}"

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"
list="$repo_root/backend/scripts/team-files.txt"

if ! git rev-parse --verify --quiet "$BASE" >/dev/null 2>&1; then
  echo "check-team-files: base ref '$BASE' 不存在，跳过（本地无远端分支属正常）" >&2
  exit 0
fi

changed=$(git diff --name-only "$BASE"...HEAD | sort | grep -v '^$' || true)
[ -n "$changed" ] && violations=$(comm -23 <(printf '%s\n' "$changed") <(grep -v '^#' "$list" | sort -u)) || violations=""

if [ -n "$violations" ]; then
  echo "check-team-files FAIL: 以下改动文件不在团队清单内（上游原产文件，禁止团队侧修改）：" >&2
  echo "$violations" >&2
  echo "如确属团队维护范围扩展，请在 backend/scripts/team-files.txt 显式登记并在 PR 说明。" >&2
  exit 1
fi
echo "check-team-files OK: $(printf '%s\n' "$changed" | grep -c -v '^$' || true) 个改动文件全部在团队清单内。"
