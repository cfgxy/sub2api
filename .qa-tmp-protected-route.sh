#!/usr/bin/env bash
set -u

dom=$(timeout 30s /snap/bin/chromium --headless=new --no-sandbox --disable-gpu --incognito --virtual-time-budget=10000 --user-data-dir=/tmp/shan506-qa-protected-route-home --window-size=1440,1000 --dump-dom https://qiye.v2.huayuebridge.com/enterprise/home 2>/dev/null)
status=$?
inputs=$(printf '%s' "$dom" | grep -oi '<input' | wc -l)
email=$(printf '%s' "$dom" | grep -oi 'type="email"' | wc -l)
password=$(printf '%s' "$dom" | grep -oi 'type="password"' | wc -l)
forms=$(printf '%s' "$dom" | grep -oi '<form' | wc -l)
printf 'QA_PROTECTED_ROUTE rc=%s inputs=%s email=%s password=%s forms=%s\n' "$status" "$inputs" "$email" "$password" "$forms"
