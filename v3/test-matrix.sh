#!/usr/bin/env bash
set -euo pipefail

ROOT="$HOME/dialtone/src/mods/mesh/v3"
SSH_WRAPPER="$HOME/dialtone/dialtone.sh"

ssh_gold() {
  "$SSH_WRAPPER" ssh gold "$@" 2>/dev/null
}

run_test() {
  local name="$1"
  shift
  echo "== $name =="
  if "$@"; then
    echo "PASS: $name"
    echo
    return 0
  fi
  echo "FAIL: $name"
  echo
  return 1
}

local_loopback() {
  local bin="$HOME/dialtone/bin/mesh-v3_$(uname -m)"
  [ -x "$bin" ] || (cd "$ROOT" && ./build.sh >/dev/null)

  local log
  log="$(mktemp /tmp/meshv3-local-loop.XXXXXX.log)"
  "$bin" receiver >"$log" 2>&1 &
  local pid=$!

  local ticket=""
  for _ in $(seq 1 40); do
    ticket="$(grep -m1 '^endpoint' "$log" || true)"
    [ -n "$ticket" ] && break
    sleep 0.25
  done

  if [ -z "$ticket" ]; then
    kill "$pid" >/dev/null 2>&1 || true
    rm -f "$log"
    return 1
  fi

  set +e
  "$bin" sender "$ticket"
  local rc=$?
  set -e

  kill "$pid" >/dev/null 2>&1 || true
  rm -f "$log"
  return $rc
}

gold_loopback() {
  ssh_gold 'cd $HOME/dialtone/src/mods/mesh/v3 && ./demo.sh local_to_gold >/dev/null'
}

local_to_gold() {
  cd "$ROOT"
  ./demo.sh local_to_gold >/dev/null
}

gold_to_local() {
  cd "$ROOT"
  ./demo.sh gold_to_local >/dev/null
}

rc=0
run_test "local loopback" local_loopback || rc=1
run_test "gold->local (cross-host)" local_to_gold || rc=1
run_test "local->gold (cross-host)" gold_to_local || rc=1

exit $rc
