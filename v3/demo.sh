#!/usr/bin/env bash
set -euo pipefail

ROOT="$HOME/dialtone/src/mods/mesh/v3"
SSH_WRAPPER="$HOME/dialtone/dialtone.sh"

ssh_gold() {
  "$SSH_WRAPPER" ssh gold "$@" 2>/dev/null
}

usage() {
  cat <<USAGE
Usage:
  demo.sh local_to_gold
  demo.sh gold_to_local
  demo.sh both

Notes:
- local = current machine (WSL)
- gold  = ssh host alias 'gold' in env/ssh_config
- Requires built binaries in ~/dialtone/bin on both hosts.
USAGE
}

need_bin_local() {
  local bin="$HOME/dialtone/bin/mesh-v3_$(uname -m)"
  if [ ! -x "$bin" ]; then
    (cd "$ROOT" && ./build.sh >/dev/null)
  fi
  echo "$bin"
}

need_bin_gold() {
  local out
  out="$(ssh_gold 'BIN=$HOME/dialtone/bin/mesh-v3_$(uname -m); if [ ! -x "$BIN" ]; then cd $HOME/dialtone/src/mods/mesh/v3 && ./build.sh >/dev/null; fi; echo "$BIN"')"
  echo "$out" | grep '^/' | tail -n1
}

start_local_receiver() {
  local bin="$1"
  local log
  log="$(mktemp /tmp/meshv3-local-recv.XXXXXX.log)"
  "$bin" receiver >"$log" 2>&1 &
  local pid=$!

  local ticket=""
  for _ in $(seq 1 40); do
    ticket="$(grep -m1 '^endpoint' "$log" || true)"
    if [ -n "$ticket" ]; then
      break
    fi
    sleep 0.25
  done

  if [ -z "$ticket" ]; then
    echo "failed to get local receiver ticket" >&2
    kill "$pid" >/dev/null 2>&1 || true
    return 1
  fi

  printf '%s\n%s\n%s\n' "$pid" "$log" "$ticket"
}

start_gold_receiver() {
  local bin="$1"
  local log
  log="$(mktemp /tmp/meshv3-gold-recv.XXXXXX.log)"
  ssh_gold "$bin receiver" >"$log" 2>&1 &
  local ssh_pid=$!

  local ticket=""
  for _ in $(seq 1 40); do
    ticket="$(grep -m1 '^endpoint' "$log" || true)"
    if [ -n "$ticket" ]; then
      break
    fi
    sleep 0.25
  done

  if [ -z "$ticket" ]; then
    echo "failed to get gold receiver ticket" >&2
    kill "$ssh_pid" >/dev/null 2>&1 || true
    return 1
  fi

  printf '%s\n%s\n%s\n' "$ssh_pid" "$log" "$ticket"
}

stop_local_receiver() {
  local pid="$1"
  kill "$pid" >/dev/null 2>&1 || true
}

stop_gold_receiver() {
  local ssh_pid="$1"
  kill "$ssh_pid" >/dev/null 2>&1 || true
}

gold_receiver_alive() {
  local ssh_pid="$1"
  kill -0 "$ssh_pid" >/dev/null 2>&1
}

run_local_sender() {
  local bin="$1"
  local ticket="$2"
  "$bin" sender "$ticket"
}

run_gold_sender() {
  local bin="$1"
  local ticket="$2"
  ssh_gold "$bin sender '$ticket'"
}

demo_local_to_gold() {
  local local_bin gold_bin
  local_bin="$(need_bin_local)"
  gold_bin="$(need_bin_gold)"

  local recv_data pid log ticket
  mapfile -t recv_data < <(start_local_receiver "$local_bin")
  if [ "${#recv_data[@]}" -lt 3 ]; then
    echo "failed to start local receiver" >&2
    return 1
  fi
  pid="${recv_data[0]}"
  log="${recv_data[1]}"
  ticket="${recv_data[2]}"

  echo "[local_to_gold] receiver(local) ticket: $ticket"
  run_gold_sender "$gold_bin" "$ticket"

  stop_local_receiver "$pid"
  rm -f "$log"
}

demo_gold_to_local() {
  local local_bin gold_bin
  local_bin="$(need_bin_local)"
  gold_bin="$(need_bin_gold)"

  local recv_data ssh_pid log ticket
  mapfile -t recv_data < <(start_gold_receiver "$gold_bin")
  if [ "${#recv_data[@]}" -lt 3 ]; then
    echo "failed to start gold receiver" >&2
    return 1
  fi
  ssh_pid="${recv_data[0]}"
  log="${recv_data[1]}"
  ticket="${recv_data[2]}"

  echo "[gold_to_local] receiver(gold) ticket: $ticket"
  if ! gold_receiver_alive "$ssh_pid"; then
    echo "gold receiver is not alive before sender start" >&2
    tail -n 50 "$log" >&2 || true
    return 1
  fi

  if ! run_local_sender "$local_bin" "$ticket"; then
    echo "local sender failed; gold receiver log:" >&2
    tail -n 80 "$log" >&2 || true
    stop_gold_receiver "$ssh_pid"
    return 1
  fi

  stop_gold_receiver "$ssh_pid"
  rm -f "$log"
}

cmd="${1:-}"
case "$cmd" in
  local_to_gold)
    demo_local_to_gold
    ;;
  gold_to_local)
    demo_gold_to_local
    ;;
  both)
    demo_local_to_gold
    demo_gold_to_local
    ;;
  *)
    usage
    exit 2
    ;;
esac
