#!/usr/bin/env bash
# Stage an immutable image for the AWS production stack. Deliberately does not
# run docker compose up/down/restart; activation remains an explicit operation.
set -euo pipefail

image=${1:?usage: stage-aws-sg-image.sh <immutable-ghcr-image>}
env_file=${SUB2API_ENV_FILE:-/mnt/data/srv/sub2api/.env}

[[ "$image" =~ ^ghcr\.io/cfgxy/sub2api@sha256:[0-9a-f]{64}$ ]] || {
  echo "image must be the immutable cfgxy/sub2api digest" >&2
  exit 2
}
[[ -f "$env_file" ]] || { echo "missing env file: $env_file" >&2; exit 1; }

# Verify availability before changing the next-start pointer.
docker pull "$image" >/dev/null
sudo cp -p "$env_file" "${env_file}.bak.${GITHUB_RUN_ID:-manual}"
tmp=$(mktemp)
sudo awk -v image="$image" '
  BEGIN { updated=0 }
  /^SUB2API_IMAGE=/ { print "SUB2API_IMAGE=" image; updated=1; next }
  { print }
  END { if (!updated) print "SUB2API_IMAGE=" image }
' "$env_file" > "$tmp"
sudo install -o root -g root -m 600 "$tmp" "$env_file"
rm -f "$tmp"

sudo grep -qx "SUB2API_IMAGE=$image" "$env_file"
printf 'staged_image=%s\nrestart=not_requested\n' "$image"
