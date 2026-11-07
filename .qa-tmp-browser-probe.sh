#!/usr/bin/env bash
set -u

url='https://qiye.v2.huayuebridge.com/enterprise/login'
for viewport in desktop mobile; do
  if [ "$viewport" = 'desktop' ]; then
    size='1440,1000'
  else
    size='390,844'
  fi

  for run in 1 2 3; do
    dom=$(timeout 30s /snap/bin/chromium --headless=new --no-sandbox --disable-gpu --incognito --virtual-time-budget=10000 --user-data-dir="/tmp/shan506-qa-${viewport}-${run}" --window-size="$size" --screenshot="/tmp/shan506-${viewport}.png" --dump-dom "$url" 2>/dev/null)
    status=$?
    inputs=$(printf '%s' "$dom" | grep -oi '<input' | wc -l)
    email=$(printf '%s' "$dom" | grep -oi 'type="email"' | wc -l)
    password=$(printf '%s' "$dom" | grep -oi 'type="password"' | wc -l)
    forms=$(printf '%s' "$dom" | grep -oi '<form' | wc -l)
    printf 'QA_RENDER viewport=%s run=%s rc=%s inputs=%s email=%s password=%s forms=%s\n' "$viewport" "$run" "$status" "$inputs" "$email" "$password" "$forms"
  done
done
