#!/usr/bin/env bash
set -Eeuo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

local_start() {
  mkdir -p .run
  if [[ -s .run/prices.pid ]] && kill -0 "$(cat .run/prices.pid)" 2>/dev/null; then
    curl --fail --silent http://127.0.0.1:8080/healthz >/dev/null
    printf 'http://127.0.0.1:8080\n'
    return
  fi
  [[ -x bin/prices ]] || { echo 'Run scripts/prepare.sh first' >&2; exit 1; }
  nohup "$ROOT/bin/prices" >>.run/prices.log 2>&1 </dev/null &
  local pid=$!
  printf '%s\n' "$pid" >.run/prices.pid
  for ((attempt=0;attempt<60;attempt++)); do
    if curl --fail --silent http://127.0.0.1:8080/healthz >/dev/null; then
      printf 'http://127.0.0.1:8080\n'; return
    fi
    if ! kill -0 "$pid" 2>/dev/null; then cat .run/prices.log >&2; return 1; fi
    sleep 1
  done
  echo 'Application did not become ready; see .run/prices.log' >&2
  return 1
}

if [[ ${1:-} == --local || ${RUN_LOCAL:-0} == 1 || ${GITHUB_ACTIONS:-false} == true ]]; then
  local_start
  exit
fi
HOST=${1:-${DEPLOY_HOST:-}}
SSH_KEY=${2:-${DEPLOY_SSH_KEY:-}}
REMOTE_USER=${DEPLOY_USER:-student}
[[ -n $HOST && -r $SSH_KEY ]] || { echo 'Usage: scripts/run.sh <host> <private-key-path> (or DEPLOY_HOST / DEPLOY_SSH_KEY)' >&2; exit 2; }
[[ $HOST =~ ^[a-zA-Z0-9][a-zA-Z0-9.-]*$ && $REMOTE_USER =~ ^[a-z_][a-z0-9_-]*$ ]] || { echo 'Invalid SSH host/user' >&2; exit 2; }
: "${POSTGRES_PASSWORD:?Set POSTGRES_PASSWORD to the training DB password from the assignment}"
[[ $POSTGRES_PASSWORD != *$'\n'* && $POSTGRES_PASSWORD != *$'\r'* ]] || { echo 'Password must be a single line' >&2; exit 2; }
GO_BIN=${GO_BIN:-go}
mkdir -p bin
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "$GO_BIN" build -trimpath -o bin/prices .
SSH=(ssh -i "$SSH_KEY" -o BatchMode=yes -o ConnectTimeout=15 -o StrictHostKeyChecking=accept-new)
TARGET="$REMOTE_USER@$HOST"
# Fixed relative deployment path. Private keys and local env files are never sent.
"${SSH[@]}" "$TARGET" 'mkdir -p "$HOME/sem1-prices"'
tar -czf - bin/prices scripts/install-db.sh | "${SSH[@]}" "$TARGET" 'tar -xzf - -C "$HOME/sem1-prices"'
printf '%s' "$POSTGRES_PASSWORD" | "${SSH[@]}" "$TARGET" 'umask 077; cat > "$HOME/sem1-prices/.db-password"'
"${SSH[@]}" "$TARGET" 'bash -s' >&2 <<'REMOTE'
set -Eeuo pipefail
cd "$HOME/sem1-prices"
export POSTGRES_PASSWORD="$(cat .db-password)"
trap 'rm -f .db-password' EXIT
escaped=${POSTGRES_PASSWORD//\\/\\\\}
escaped=${escaped//\"/\\\"}
umask 077
printf 'POSTGRES_PASSWORD="%s"\n' "$escaped" > service.env
rm -f .db-password
bash scripts/install-db.sh
./bin/prices --prepare
unit=$(mktemp)
trap 'rm -f "$unit" .db-password' EXIT
cat >"$unit" <<UNIT
[Unit]
Description=Semester 1 prices API
After=network.target postgresql.service
Requires=postgresql.service
[Service]
User=$(id -un)
WorkingDirectory=$HOME/sem1-prices
ExecStart=$HOME/sem1-prices/bin/prices
EnvironmentFile=$HOME/sem1-prices/service.env
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true
[Install]
WantedBy=multi-user.target
UNIT
sudo -n install -m 644 "$unit" /etc/systemd/system/sem1-prices.service
sudo -n systemctl daemon-reload
sudo -n systemctl enable sem1-prices.service
sudo -n systemctl restart sem1-prices.service
for ((attempt=0;attempt<60;attempt++)); do
  if curl --fail --silent http://127.0.0.1:8080/healthz >/dev/null; then exit 0; fi
  sleep 1
done
sudo -n journalctl -u sem1-prices.service -n 30 --no-pager >&2
exit 1
REMOTE
# stdout contains only the deployment host; build/SSH diagnostics go to stderr.
printf '%s\n' "$HOST"
