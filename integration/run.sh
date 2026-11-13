#!/usr/bin/env bash
# Runs check-nginx-status against a real nginx (stub_status) inside a Docker container.
set -euo pipefail
cd "$(dirname "$0")"

image=check-nginx-status-integration
container=check-nginx-status-integration-$$

docker build -q -t "$image" . >/dev/null
docker run -d --rm --name "$container" "$image" >/dev/null
trap 'docker stop "$container" >/dev/null' EXIT

for _ in $(seq 30); do
  docker exec "$container" wget -qO- http://127.0.0.1/nginx-status >/dev/null 2>&1 && break
  sleep 1
done

failures=0
expect() {
  local want=$1; shift
  local out rc=0
  # Run as an unprivileged user, like sensu-agent does.
  out=$(docker exec -u nobody "$container" check-nginx-status "$@" 2>&1) || rc=$?
  out=$(tail -1 <<<"$out")
  if [[ $rc -eq $want ]]; then
    echo "ok   [$rc] check-nginx-status $* -> $out"
  else
    echo "FAIL [$rc, want $want] check-nginx-status $* -> $out"
    failures=$((failures + 1))
  fi
}

base=http://127.0.0.1

expect 0 -u $base/nginx-status
expect 0 -u $base/nginx-status -a 0 -W 0 -w 0
expect 3 -u $base/nginx-status -w -1
expect 3 -u $base/nginx-status --load-grace -1
expect 1 -u $base/redirect
expect 1 -u $base/html
expect 2 -u $base/missing
expect 2 -u http://127.0.0.1:81/nginx-status
expect 3 -u 127.0.0.1/nginx-status
expect 3

# Hold idle connections open: active connections above 3 (the check adds its own).
docker exec -d "$container" sh -c 'for i in 1 2 3 4; do sleep 30 | nc 127.0.0.1 80 & done; wait'
sleep 1
docker exec "$container" mkdir -p /var/cache/sensu/sensu-agent
docker exec "$container" chown nobody /var/cache/sensu/sensu-agent

expect 1 -u $base/nginx-status -a 3 --load-grace 0
expect 0 -u $base/nginx-status -a 3                      # within grace, state file written
expect 0 -u $base/nginx-status -a 3                      # still within grace
sleep 2
expect 1 -u $base/nginx-status -a 3 --load-grace 1       # first seen 2s ago
expect 1 -u $base/nginx-status -a 3 --state-dir /missing # state not kept: no grace
if docker exec "$container" test -s /var/cache/sensu/sensu-agent/check-nginx-status.json; then
  echo "ok   state file written"
else
  echo "FAIL state file missing"; failures=$((failures + 1))
fi

# Options from the environment, as set by the check definition.
out=$(docker exec -u nobody -e CHECK_NGINX_URL=$base/nginx-status -e CHECK_NGINX_ACTIVE_CONNECTIONS=3 \
  -e CHECK_NGINX_LOAD_GRACE=0 "$container" check-nginx-status 2>&1) && rc=0 || rc=$?
if [[ $rc -eq 1 ]]; then echo "ok   [$rc] env vars -> $out"; else echo "FAIL [$rc, want 1] env vars -> $out"; failures=$((failures + 1)); fi

exit $failures
