#!/usr/bin/env bash
set -euo pipefail
ROOT="$HOME/dialtone/src/mods/mesh/v3"
DIALTONE_RUNNER="$HOME/dialtone/dialtone2.sh"
if [ ! -x "$DIALTONE_RUNNER" ]; then
  DIALTONE_RUNNER="$HOME/dialtone/dialtone.sh"
fi

"$DIALTONE_RUNNER" cargo build --release

ARCH="$(uname -m)"
BIN_DIR="$HOME/dialtone/bin"
mkdir -p "$BIN_DIR"
ln -sf "$ROOT/target/release/mesh-v3" "$BIN_DIR/mesh-v3_${ARCH}"
echo "Built: $BIN_DIR/mesh-v3_${ARCH} -> $ROOT/target/release/mesh-v3"
