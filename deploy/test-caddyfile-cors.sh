#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
caddyfile="$repo_root/deploy/Caddyfile"

for command in go curl; do
	if ! command -v "$command" >/dev/null 2>&1; then
		echo "test-caddyfile-cors.sh requires $command" >&2
		exit 1
	fi
done

if command -v caddy >/dev/null 2>&1; then
	caddy_mode=local
elif command -v docker >/dev/null 2>&1; then
	caddy_mode=docker
else
	echo "test-caddyfile-cors.sh requires caddy or docker" >&2
	exit 1
fi

work_dir=$(mktemp -d)
backend_port=$((20000 + ($$ % 10000)))
proxy_port=$((30000 + ($$ % 10000)))
backend_pid=''
caddy_pid=''

cleanup() {
	if [ -n "$caddy_pid" ]; then
		kill "$caddy_pid" 2>/dev/null || true
		attempt=0
		while kill -0 "$caddy_pid" 2>/dev/null && [ "$attempt" -lt 50 ]; do
			attempt=$((attempt + 1))
			sleep 0.1
		done
		kill -KILL "$caddy_pid" 2>/dev/null || true
		wait "$caddy_pid" 2>/dev/null || true
	fi
	if [ -n "$backend_pid" ]; then
		kill "$backend_pid" 2>/dev/null || true
		attempt=0
		while kill -0 "$backend_pid" 2>/dev/null && [ "$attempt" -lt 50 ]; do
			attempt=$((attempt + 1))
			sleep 0.1
		done
		kill -KILL "$backend_pid" 2>/dev/null || true
		wait "$backend_pid" 2>/dev/null || true
	fi
	rm -rf "$work_dir"
}
trap cleanup EXIT HUP INT TERM

cat > "$work_dir/upstream.go" <<'EOF'
package main

import (
	"fmt"
	"net/http"
	"os"
)

const allowedOrigin = "https://qiye.v2.huayuebridge.com"

func main() {
	if len(os.Args) != 2 {
		panic("missing listen address")
	}
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	http.HandleFunc("/api/v1/enterprise/keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, "created")
	})
	if err := http.ListenAndServe(os.Args[1], nil); err != nil {
		panic(err)
	}
}
EOF

go run "$work_dir/upstream.go" "127.0.0.1:$backend_port" >"$work_dir/upstream.log" 2>&1 &
backend_pid=$!

attempt=0
until curl --fail --silent "http://127.0.0.1:$backend_port/api/v1/enterprise/keys" >/dev/null; do
	attempt=$((attempt + 1))
	if ! kill -0 "$backend_pid" 2>/dev/null; then
		cat "$work_dir/upstream.log" >&2
		exit 1
	fi
	if [ "$attempt" -ge 300 ]; then
		echo "test upstream did not become ready" >&2
		cat "$work_dir/upstream.log" >&2
		exit 1
	fi
	sleep 0.1
done

test_caddyfile="$work_dir/Caddyfile"
sed \
	-e "s|^api\.sub2api\.com {|http://127.0.0.1:$proxy_port {|" \
	-e "s|^\([[:space:]]*\)reverse_proxy localhost:8080 {|\1reverse_proxy 127.0.0.1:$backend_port {|" \
	"$caddyfile" | awk '
	/^[[:space:]]*(tls|log)[[:space:]]*\{/ {
		skip = 1
		depth = 0
	}
	skip {
		for (field = 1; field <= NF; field++) {
			if ($field == "{") depth++
			if ($field == "}") depth--
		}
		if (depth == 0) skip = 0
		next
	}
	{ print }
' > "$test_caddyfile"

if ! grep -Fq "http://127.0.0.1:$proxy_port {" "$test_caddyfile" \
	|| ! grep -Fq "reverse_proxy 127.0.0.1:$backend_port {" "$test_caddyfile"; then
	echo "failed to derive the test Caddyfile from the deployment proxy route" >&2
	exit 1
fi

if [ "$caddy_mode" = local ]; then
	caddy run --config "$test_caddyfile" --adapter caddyfile >"$work_dir/caddy.log" 2>&1 &
else
	docker run --rm --network host --mount "type=bind,src=$work_dir,dst=/work,readonly" caddy:2 \
		caddy run --config /work/Caddyfile --adapter caddyfile >"$work_dir/caddy.log" 2>&1 &
fi
caddy_pid=$!

attempt=0
until curl --fail --silent "http://127.0.0.1:$proxy_port/api/v1/enterprise/keys" >/dev/null; do
	attempt=$((attempt + 1))
	if ! kill -0 "$caddy_pid" 2>/dev/null; then
		cat "$work_dir/caddy.log" >&2
		exit 1
	fi
	if [ "$attempt" -ge 300 ]; then
		echo "Caddy proxy did not become ready" >&2
		cat "$work_dir/caddy.log" >&2
		exit 1
	fi
	sleep 0.1
done

assert_single_origin() {
	headers=$1
	count=$(tr -d '\r' < "$headers" | awk 'tolower($1) == "access-control-allow-origin:" { count++ } END { print count + 0 }')
	if [ "$count" -ne 1 ] || ! tr -d '\r' < "$headers" | grep -Fxiq 'Access-Control-Allow-Origin: https://qiye.v2.huayuebridge.com'; then
		echo "proxied response must contain exactly one application-owned Access-Control-Allow-Origin" >&2
		exit 1
	fi
}

preflight_headers="$work_dir/preflight.headers"
preflight_status=$(curl --silent --show-error --output /dev/null --dump-header "$preflight_headers" --write-out '%{http_code}' \
	--request OPTIONS \
	--header 'Origin: https://qiye.v2.huayuebridge.com' \
	--header 'Access-Control-Request-Method: PATCH' \
	--header 'Access-Control-Request-Headers: Idempotency-Key' \
	"http://127.0.0.1:$proxy_port/api/v1/enterprise/keys")

if [ "$preflight_status" != 204 ]; then
	echo "proxied PATCH preflight must reach the application and return 204" >&2
	exit 1
fi
assert_single_origin "$preflight_headers"
if ! grep -Eiq '^Access-Control-Allow-Methods:.*PATCH' "$preflight_headers" \
	|| ! grep -Eiq '^Access-Control-Allow-Headers:.*Idempotency-Key' "$preflight_headers"; then
	echo "proxied preflight must preserve application CORS methods and headers" >&2
	exit 1
fi

post_headers="$work_dir/post.headers"
post_status=$(curl --silent --show-error --output /dev/null --dump-header "$post_headers" --write-out '%{http_code}' \
	--request POST \
	--header 'Origin: https://qiye.v2.huayuebridge.com' \
	--header 'Idempotency-Key: test-key' \
	"http://127.0.0.1:$proxy_port/api/v1/enterprise/keys")

if [ "$post_status" != 201 ]; then
	echo "proxied Key POST must reach the application" >&2
	exit 1
fi
assert_single_origin "$post_headers"

if grep -Eiq '^[[:space:]]*[+?>-]*Access-Control-(Allow|Expose|Max-Age)' "$caddyfile"; then
	echo "deployment Caddyfile must delegate CORS responses to the application" >&2
	exit 1
fi

echo "Caddy delegates Key CORS preflight and responses to the application"
