#!/usr/bin/env bash
set -euo pipefail
ROOT="$HOME/dialtone/src/mods/mesh/v3"
"$HOME/dialtone/dialtone.sh" nix --extra-experimental-features 'nix-command flakes' develop "$ROOT" -c cargo build --release

ARCH="$(uname -m)"
BIN_DIR="$HOME/dialtone/bin"
mkdir -p "$BIN_DIR"
ln -sf "$ROOT/target/release/mesh-v3" "$BIN_DIR/mesh-v3_${ARCH}"
echo "Built: $BIN_DIR/mesh-v3_${ARCH} -> $ROOT/target/release/mesh-v3"
