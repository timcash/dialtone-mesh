#!/usr/bin/env bash
set -euo pipefail
ROOT="$HOME/dialtone/src/mods/mesh/v3"
exec "$HOME/dialtone/dialtone.sh" nix --extra-experimental-features 'nix-command flakes' develop "$ROOT" -c cargo run -q -- "$@"
