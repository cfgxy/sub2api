#!/usr/bin/env bash
# Deploy one immutable Sub2API image to FDE v2 while retaining the stopped
# previous container as a rollback target.
set -euo pipefail

image=${1:?usage: deploy-fde-v2.sh <immutable-ghcr-image> <source-sha> <version>}
source_sha=${2:?missing source SHA}
version=${3:?missing version}

[[ "$image" =~ ^ghcr\.io/cfgxy/sub2api@sha256:[0-9a-f]{64}$ ]] || {
  echo "image must be the immutable cfgxy/sub2api digest" >&2
  exit 2
}
[[ "$source_sha" =~ ^[0-9a-f]{40}$ ]] || { echo "invalid source SHA" >&2; exit 2; }

app=sub2api-v2-app
network=sub2api-v2-net
rollback="${app}.rollback-${GITHUB_RUN_ID:-manual}"

[[ $(docker inspect "$app" --format '{{.State.Running}}') == true ]] || {
  echo "${app} is not running" >&2
  exit 1
}
[[ $(docker network inspect "$network" --format '{{range .Containers}}{{if eq .Name "sub2api-v2-app"}}yes{{end}}{{end}}') == yes ]] || {
  echo "${app} is not attached to ${network}" >&2
  exit 1
}

docker pull "$image" >/dev/null
docker inspect "$app" > "/tmp/${app}-pre-${GITHUB_RUN_ID:-manual}.json"

env_args=()
while IFS= read -r -d '' entry; do
  env_args+=(--env "$entry")
done < <(docker inspect "$app" --format '{{range .Config.Env}}{{printf "%s\x00" .}}{{end}}')

memory=$(docker inspect "$app" --format '{{.HostConfig.Memory}}')
[[ "$memory" =~ ^[0-9]+$ ]] || memory=0

rollback_now() {
  docker rm -f "$app" >/dev/null 2>&1 || true
  docker rename "$rollback" "$app" >/dev/null 2>&1 || true
  docker network connect --alias app "$network" "$app" >/dev/null 2>&1 || true
  docker start "$app" >/dev/null 2>&1 || true
}

# A prior rollback container can be retained only under a unique run name.
docker rm -f "$rollback" >/dev/null 2>&1 || true
docker stop -t 10 "$app" >/dev/null
docker network disconnect "$network" "$app" >/dev/null
docker rename "$app" "$rollback"
trap rollback_now ERR

docker create \
  --name "$app" \
  --network "$network" --network-alias app \
  --volumes-from "$rollback" \
  --restart unless-stopped \
  --memory "$memory" \
  --label shan.environment=fde-v2 \
  --label shan.stack=v2-verification \
  --label shan.source_sha="$source_sha" \
  --label shan.artifact_digest="${image#*@}" \
  --label shan.version="$version" \
  "${env_args[@]}" \
  "$image" >/dev/null

docker start "$app" >/dev/null
for _ in $(seq 1 30); do
  if docker exec "$app" wget -q -T 3 -O /dev/null http://127.0.0.1:8080/health; then
    trap - ERR
    docker inspect "$app" --format 'deployed={{.Image}} health={{.State.Health.Status}}'
    exit 0
  fi
  sleep 2
done

echo "FDE health check timed out" >&2
exit 1
