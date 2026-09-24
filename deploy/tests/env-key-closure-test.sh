#!/bin/sh
# env 键闭环守卫（SHAN-342 处方：样例键集 = compose 插值集，防 DA1 类回归）。
# 校验三向偏差必须落在 deploy/tests/env-key-baseline.txt 登记范围内：
#   1) compose 必填插值(:?)键必须出现在样例（登记豁免除外）
#   2) 样例键必须被 compose 插值（登记豁免除外）
#   3) compose 插值键必须出现在样例（登记豁免除外）
# 另对团队自有 SHAN152_* 键做无豁免双向硬闭环。
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/sub2api-env-closure.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

composes="deploy/docker-compose.yml deploy/docker-compose.local.yml deploy/docker-compose.standalone.yml deploy/docker-compose.dev.yml deploy/docker-compose.shan-152.yml"

cat $composes | grep -ohE '\$\{[A-Z][A-Z0-9_]*' | sed 's/\${//' | sort -u > "$tmp/compose_all"
cat $composes | grep -ohE '\$\{[A-Z][A-Z0-9_]*:\?' | sed 's/\${//;s/:?$//' | sort -u > "$tmp/required"
cat deploy/.env.example deploy/shan-152.env.example | grep -oE '^[A-Z][A-Z0-9_]*' | sort -u > "$tmp/samples"

awk '$1=="required-missing" {print $2}' deploy/tests/env-key-baseline.txt | sort -u > "$tmp/bl_req"
awk '$1=="sample-extra"     {print $2}' deploy/tests/env-key-baseline.txt | sort -u > "$tmp/bl_sample"
awk '$1=="compose-extra"    {print $2}' deploy/tests/env-key-baseline.txt | sort -u > "$tmp/bl_compose"

fail=0

# 1) 必填缺口
comm -23 "$tmp/required" "$tmp/samples" > "$tmp/req_miss_raw"
comm -23 "$tmp/req_miss_raw" "$tmp/bl_req" > "$tmp/req_miss"
if [ -s "$tmp/req_miss" ]; then
  echo "env-closure FAIL: compose 必填插值键不在样例中（DA1 类回归）：" >&2
  sed 's/^/  /' "$tmp/req_miss" >&2
  fail=1
fi

# 2) 样例多余键
comm -23 "$tmp/samples" "$tmp/compose_all" > "$tmp/s_extra_raw"
comm -23 "$tmp/s_extra_raw" "$tmp/bl_sample" > "$tmp/s_extra"
if [ -s "$tmp/s_extra" ]; then
  echo "env-closure FAIL: 样例出现 compose 不插值的新键（请同步 compose 或登记基线）：" >&2
  sed 's/^/  /' "$tmp/s_extra" >&2
  fail=1
fi

# 3) compose 独有键（排除已由 1) 覆盖的必填键）
comm -23 "$tmp/compose_all" "$tmp/samples" > "$tmp/c_extra_raw"
comm -23 "$tmp/c_extra_raw" "$tmp/bl_compose" > "$tmp/c_extra_noBl"
grep -vxF -f "$tmp/required" "$tmp/c_extra_noBl" > "$tmp/c_extra" || true
if [ -s "$tmp/c_extra" ]; then
  echo "env-closure FAIL: compose 出现样例未列的新插值键（请补样例或登记基线）：" >&2
  sed 's/^/  /' "$tmp/c_extra" >&2
  fail=1
fi

# 4) SHAN152_* 团队键无豁免双向硬闭环
grep '^SHAN152' "$tmp/compose_all" > "$tmp/c_shan" || true
grep '^SHAN152' "$tmp/samples" > "$tmp/s_shan" || true
s_only=$(comm -23 "$tmp/s_shan" "$tmp/c_shan")
c_only=$(comm -13 "$tmp/s_shan" "$tmp/c_shan")
if [ -n "$s_only" ]; then
  echo "env-closure FAIL: shan-152 样例出现 compose 不插值的 SHAN152 键：" >&2
  echo "$s_only" >&2
  fail=1
fi
if [ -n "$c_only" ]; then
  echo "env-closure FAIL: shan-152 compose 出现样例未列的 SHAN152 键：" >&2
  echo "$c_only" >&2
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  echo "处置：补齐对应样例/compose 键，或确属现状设计时在 deploy/tests/env-key-baseline.txt 登记。" >&2
  exit 1
fi
echo "env-closure OK: 样例键集与 compose 插值集闭环（偏差均在基线内），SHAN152_* 硬闭环通过。"
