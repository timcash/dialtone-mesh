#!/usr/bin/env bash
set -euo pipefail
DIALTONE_RUNNER="$HOME/dialtone/dialtone2.sh"
if [ ! -x "$DIALTONE_RUNNER" ]; then
  DIALTONE_RUNNER="$HOME/dialtone/dialtone.sh"
fi

exec "$DIALTONE_RUNNER" cargo run -q -- "$@"
