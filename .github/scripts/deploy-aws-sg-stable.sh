#!/usr/bin/env bash
# Deploy one versioned Sub2API image to the AWS production Compose stack.
# The image tag is the application version and is intentionally retained in
# .env for an auditable, human-readable next/current runtime reference.
set -euo pipefail

image=${1:?usage: deploy-aws-sg-stable.sh <versioned-ghcr-image> <source-sha> <version>}
source_sha=${2:?missing source SHA}
version=${3:?missing version}
stack_dir=${SUB2API_STACK_DIR:-/mnt/data/srv/sub2api}
env_file=${SUB2API_ENV_FILE:-${stack_dir}/.env}
compose_file=${SUB2API_COMPOSE_FILE:-${stack_dir}/docker-compose.yaml}
service=${SUB2API_SERVICE:-sub2api}

[[ "$image" =~ ^ghcr\.io/cfgxy/sub2api:v[0-9]+\.[0-9]+\.[0-9]+-ent\.[0-9]{8}-[1-9][0-9]*$ ]] || {
  echo "image must be a versioned enterprise cfgxy/sub2api tag" >&2
  exit 2
}
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+-ent\.[0-9]{8}-[1-9][0-9]*$ ]] || {
  echo "invalid enterprise package version" >&2
  exit 2
}
[[ "$image" == "ghcr.io/cfgxy/sub2api:v${version}" ]] || {
  echo "image tag must exactly equal application version" >&2
  exit 2
}
[[ "$source_sha" =~ ^[0-9a-f]{40}$ ]] || { echo "invalid source SHA" >&2; exit 2; }
[[ -f "$env_file" && -f "$compose_file" ]] || { echo "missing production stack files" >&2; exit 1; }

backup="${env_file}.bak.${GITHUB_RUN_ID:-manual}"
previous_image=$(sudo docker inspect --format '{{.Config.Image}}' "$service")
previous_started=$(sudo docker inspect --format '{{.State.StartedAt}}' "$service")
rollback_needed=false

rollback() {
  [[ "$rollback_needed" == true ]] || return 0
  echo "deployment failed; restoring ${backup}" >&2
  sudo cp -p "$backup" "$env_file"
  sudo docker compose -f "$compose_file" --env-file "$env_file" up -d --no-deps "$service"
}
trap rollback ERR

# Download before changing the runtime pointer. The deploy user is deliberately
# not in the docker group, so Docker access is limited to explicit sudo calls.
sudo docker pull "$image" >/dev/null
sudo cp -p "$env_file" "$backup"
tmp=$(mktemp)
sudo awk -v image="$image" '
  BEGIN { updated=0 }
  /^SUB2API_IMAGE=/ { print "SUB2API_IMAGE=" image; updated=1; next }
  { print }
  END { if (!updated) print "SUB2API_IMAGE=" image }
' "$env_file" > "$tmp"
sudo install -o root -g root -m 600 "$tmp" "$env_file"
rm -f "$tmp"
rollback_needed=true

# Recreate only the application service. PostgreSQL, Redis, Caddy and NextChat
# remain untouched; Compose receives the same versioned tag stored in .env.
sudo docker compose -f "$compose_file" --env-file "$env_file" up -d --no-deps "$service"

for _ in $(seq 1 30); do
  health=$(sudo docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$service")
  [[ "$health" == healthy ]] && break
  sleep 2
done
[[ "$health" == healthy ]] || { echo "${service} health check timed out: ${health}" >&2; exit 1; }

# Check the edge path after the container health gate. This is intentionally
# external to the Compose network, so it proves the public production route.
curl --fail --silent --show-error --connect-timeout 10 --max-time 30 \
  https://www.apinoria.com/health | grep -q '"status":"ok"'

rollback_needed=false
sudo grep -qx "SUB2API_IMAGE=$image" "$env_file"
current_image=$(sudo docker inspect --format '{{.Config.Image}}' "$service")
current_started=$(sudo docker inspect --format '{{.State.StartedAt}}' "$service")
printf 'deployed_version=%s\nimage=%s\nsource_sha=%s\nprevious_image=%s\nprevious_started=%s\ncurrent_image=%s\ncurrent_started=%s\n' \
  "$version" "$image" "$source_sha" "$previous_image" "$previous_started" "$current_image" "$current_started"
