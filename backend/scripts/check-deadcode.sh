#!/bin/bash
# deadcode 基线 diff 门 + staticcheck U1000 豁免门。
# deadcode: 重跑 ./... 与 backend/scripts/deadcode-baseline.txt 比对，新增死代码即失败
#           （基线=SHAN-356 清理后快照；上游继承线索在基线内放行，上游同步新增须显式更新基线）。
# U1000:    未用符号零容忍，仅豁免 scripts/u1000-allowlist.txt 登记的上游原产符号
#           （如 grok_media.go SA5 保护区，上游原产禁删禁改）。
set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
baseline="$repo_root/backend/scripts/deadcode-baseline.txt"
allowlist="$repo_root/backend/scripts/u1000-allowlist.txt"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

cd "$repo_root/backend"

run_deadcode() {
  if command -v deadcode >/dev/null 2>&1; then deadcode "$@"; else go run golang.org/x/tools/cmd/deadcode@latest "$@"; fi
}
run_staticcheck() {
  if command -v staticcheck >/dev/null 2>&1; then staticcheck "$@"; else go run honnef.co/go/tools/cmd/staticcheck@latest "$@"; fi
}

# ---- 门 1: deadcode 基线 diff ----
run_deadcode ./... 2>/dev/null | sort -u > "$tmp/current"
grep -v '^#' "$baseline" | sort -u > "$tmp/base"

new_lines=$(comm -13 "$tmp/base" "$tmp/current")
if [ -n "$new_lines" ]; then
  echo "check-deadcode FAIL: 出现基线外新增死代码（防回潮门）：" >&2
  echo "$new_lines" >&2
  echo "团队侧请删除死代码；若属上游同步带来的新线索，显式更新 backend/scripts/deadcode-baseline.txt 并在 PR 说明。" >&2
  exit 1
fi
shrunk=$(comm -23 "$tmp/base" "$tmp/current" | wc -l)
[ "$shrunk" -gt 0 ] && echo "check-deadcode: 基线中 $shrunk 行已消失（死代码被上游/团队清除），可选择收缩基线。" >&2
echo "check-deadcode OK: deadcode 输出与基线一致（$(wc -l < "$tmp/current") 行，全部为已登记线索）。"

# ---- 门 2: U1000 零容忍（豁免清单制）----
grep -v '^#' "$allowlist" | grep -v '^$' > "$tmp/allow" || true
run_staticcheck -checks U1000 ./... > "$tmp/u1000" 2>/dev/null || true
unregistered=$(grep -v -F -f "$tmp/allow" "$tmp/u1000" || true)
if [ -n "$unregistered" ]; then
  echo "check-u1000 FAIL: 出现未登记的未用符号（U1000 零容忍）：" >&2
  echo "$unregistered" >&2
  echo "团队侧请删除未用符号；仅上游原产且禁改的符号可在 scripts/u1000-allowlist.txt 登记（注明出处）。" >&2
  exit 1
fi
echo "check-u1000 OK: U1000 命中 $(wc -l < "$tmp/u1000") 条，全部为已豁免上游原产符号。"
