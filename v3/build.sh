#!/usr/bin/env bash
set -euo pipefail

ROOT="$HOME/dialtone/src/mods/mesh/v3"
cd "$ROOT"

NIX_BIN="$(command -v nix || true)"
if [ -z "$NIX_BIN" ]; then
  NIX_BIN="$(ls -1d /nix/store/*-nix-*/bin/nix 2>/dev/null | sort | tail -n1 || true)"
fi
if [ -z "$NIX_BIN" ] || [ ! -x "$NIX_BIN" ]; then
  echo "nix not found" >&2
  exit 1
fi

"$NIX_BIN" --extra-experimental-features "nix-command flakes" build .#mesh-v3

ARCH="$(uname -m)"
BIN_DIR="$HOME/dialtone/bin"
mkdir -p "$BIN_DIR"
ln -sf "$ROOT/result/bin/mesh-v3" "$BIN_DIR/mesh-v3_${ARCH}"
echo "Built: $BIN_DIR/mesh-v3_${ARCH} -> $ROOT/result/bin/mesh-v3"
