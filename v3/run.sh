#!/usr/bin/env bash
set -euo pipefail
DIALTONE_RUNNER="$HOME/dialtone/dialtone_mod"
if [ ! -x "$DIALTONE_RUNNER" ]; then
  DIALTONE_RUNNER="$HOME/dialtone/dialtone.sh"
fi

exec "$DIALTONE_RUNNER" cargo run -q -- "$@"
