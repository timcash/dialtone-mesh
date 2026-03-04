# mesh v3 (iroh index + gossip)

`mesh-v3` is a single binary with multiple modes:

- `node`: peer node (iroh endpoint + gossip + auto-register)
- `index`: HTTP/WebSocket index API/UI
- `hub`: runs `index` + `node` together in one process

Default mode is `node`.

## Build (native)

```bash
cd ~/dialtone/src/mods/mesh/v3
./build.sh
```

This builds with nix and links:

- `~/dialtone/bin/mesh-v3_$(uname -m)` -> native build

Use `./build.sh --rebuild` to force rebuild.

## Build rover target (from WSL)

```bash
cd ~/dialtone/src/mods/mesh/v3
nix --extra-experimental-features "nix-command flakes" build .#mesh-v3-rover
```

This produces an `aarch64-linux` build suitable for rover.

## Run node

```bash
MESH_V3_NODE=gold MESH_V3_INDEX_URL=https://index.dialtone.earth \
  ~/dialtone/bin/mesh-v3_$(uname -m) node
```

## Run index

```bash
~/dialtone/bin/mesh-v3_$(uname -m) index 127.0.0.1:8787
```

## Run hub (index + node in one process)

```bash
MESH_V3_NODE=wsl MESH_V3_INDEX_URL=https://index.dialtone.earth \
  ~/dialtone/bin/mesh-v3_$(uname -m) hub 127.0.0.1:8787
```

## APIs

- `GET /health`
- `GET /nodes`
- `PUT /nodes`
- `POST /register`
- `GET /ticket/:node`
- `GET /ws` (live peer updates)

## Hotspot gossip debugging log (March 4, 2026)

What was tried to get `rover-1` gossip stable while on phone hotspot `tim`:

1. Rover hotspot reconnection + service restart
- Connected rover via link-local SSH (`tim@169.254.217.151`).
- Forced Wi-Fi profile `tim` on `wlan1` using `sudo nmcli connection up "tim" ifname wlan1`.
- Verified `yes:tim` and `mesh-v3-rover.service` = `active`.

2. Verified rover runtime behavior from log
- Checked `/home/tim/dialtone/tmp/mesh-v3-rover.log`.
- Observed intermittent index HTTP failures (`/nodes` and `/register`) while on cellular.
- Also observed successful register cycles and live gossip receives from other nodes (`grey`, `wsl`).

3. Added resilience in `mesh-v3` code
- Gossip heartbeats now include peer hints (`node`, `node_id`, `ticket`, `updated_at_unix`).
- Node merges hinted peers into local cache and uses cache when index fetch fails.
- Added timestamped logs (`[unix_ts] ...`) for all key gossip/register events.
- Added register retry/backoff and configurable success interval (`MESH_V3_REGISTER_INTERVAL_SECS`, default `30`).
- Added compatibility for older index responses:
  - `RegisterResponse.nodes` is optional (`#[serde(default)]`).
  - `Entry.node_id` is optional on decode (`#[serde(default)]`) and derived from ticket if missing.
- Added persistent peer cache on disk:
  - default path: `~/dialtone/tmp/mesh-v3-peer-cache.json`
  - loaded on startup and refreshed periodically.

4. Deployed updated rover binary
- Built rover target from `v3` flake (`.#mesh-v3-rover`) and copied to:
  - `/home/tim/dialtone/bin/mesh-v3_arm64`
- Restarted `mesh-v3-rover.service`.
- Verified cache file creation and fresh logs with `auto-register ... peers=N` and gossip messages.

5. Gold/grey verification work in progress
- Confirmed SSH access to `gold` and `grey`.
- Began scanning host logs for fresh rover heartbeats.
- Need one cleanup step in remote grep commands (zsh glob handling) before final cross-host heartbeat proof is fully captured in this README.
