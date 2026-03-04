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

TARGET="native"
REBUILD=0
while [ $# -gt 0 ]; do
  case "$1" in
    --rebuild)
      REBUILD=1
      ;;
    --target)
      shift
      TARGET="${1:-}"
      ;;
    native|rover)
      TARGET="$1"
      ;;
    *)
      echo "usage: ./build.sh [--rebuild] [--target native|rover]" >&2
      exit 1
      ;;
  esac
  shift || true
done

case "$TARGET" in
  native)
    ATTR="mesh-v3"
    OUT_LINK="$ROOT/.result-native"
    ARCH="$(uname -m)"
    BIN_OUT="$HOME/dialtone/bin/mesh-v3_${ARCH}"
    ;;
  rover)
    ATTR="mesh-v3-rover"
    OUT_LINK="$ROOT/.result-rover"
    BIN_OUT="$HOME/dialtone/bin/mesh-v3_arm64"
    ;;
  *)
    echo "invalid target: $TARGET (expected native|rover)" >&2
    exit 1
    ;;
esac

BUILD_ARGS=(--extra-experimental-features "nix-command flakes" build ".#${ATTR}" --out-link "$OUT_LINK")
if [ "$REBUILD" -eq 1 ]; then
  BUILD_ARGS+=(--rebuild)
fi
"$NIX_BIN" "${BUILD_ARGS[@]}"

BIN_DIR="$HOME/dialtone/bin"
mkdir -p "$BIN_DIR"
ln -sf "$OUT_LINK/bin/mesh-v3" "$BIN_OUT"
echo "Built ($TARGET): $BIN_OUT -> $OUT_LINK/bin/mesh-v3"
