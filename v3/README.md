# mesh v3 (iroh index + gossip)

`mesh-v3` is a single binary with multiple modes:

- `node`: peer node (iroh endpoint + gossip + auto-register)
- `index`: HTTP/WebSocket index API/UI
- `hub`: runs `index` + `node` together in one process

Default mode is `node`.

## Build (native)

```bash
cd <repo-root>
./dialtone_mod mesh v3 build --target native
```

This builds with nix and links:

- `<repo-root>/bin/mesh-v3_$(uname -m)` -> native build

Use `./dialtone_mod mesh v3 build --rebuild` to force rebuild.

## Build rover target (from WSL)

```bash
cd <repo-root>
./dialtone_mod mesh v3 build --target rover
```

This produces an `aarch64-linux` build suitable for rover and links:

- `<repo-root>/bin/mesh-v3_arm64` -> rover build

This command uses separate nix out-links:

- native: `.result-native`
- rover: `.result-rover`

This avoids cross-build collisions with the generic `result` symlink.

## Run node

```bash
<repo-root>/bin/mesh-v3_$(uname -m) \
  --node gold \
  --index-url https://index.dialtone.earth \
  --dht \
  node
```

## Run index

```bash
<repo-root>/bin/mesh-v3_$(uname -m) index 127.0.0.1:8787
```

## Run hub (index + node in one process)

```bash
<repo-root>/bin/mesh-v3_$(uname -m) \
  --node wsl \
  --index-url https://index.dialtone.earth \
  --dht \
  hub 127.0.0.1:8787
```

### Runtime flags

- `--node <name>`
- `--index-url <url>`
- `--gossip-interval <secs>`
- `--register-interval <secs>`
- `--peer-cache <path>`
- `--no-auto-register`
- `--dht` / `--no-dht`
- `--dns` / `--no-dns`
- `--relay-only`

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

## Why rover may show fewer heartbeats on hotspot

- Heartbeats are periodic, not continuous. If `MESH_V3_GOSSIP_INTERVAL_SECS` is `60`, a healthy peer only emits about one heartbeat per minute.
- Phone hotspots often change NAT mappings and can briefly interrupt UDP reachability.
- During those windows, gossip may route via relay or reconnect and you will see fewer inbound heartbeat lines even though the node is still alive and registering.
- If older cached peer IDs accumulate, one host can appear as many peers until cache/index state is cleaned.

Current expected steady-state for 4 hosts (`gold`, `grey`, `wsl`, `rover-1`):
- each node should converge near `3` known peers (all others).

### Verified routing state on rover (March 4, 2026)

- Active network devices:
  - `wlan1` connected to `tim`
  - `eth0` connected only for `169.254.0.0/16` link-local
- Default route:
  - `default via 172.20.10.1 dev wlan1`
- Internet route checks:
  - `ip route get 1.1.1.1` -> `dev wlan1 src 172.20.10.3`
  - `ip route get 8.8.8.8` -> `dev wlan1 src 172.20.10.3`

Conclusion:
- Internet-bound discovery/relay/gossip traffic is using hotspot (`wlan1`), not local-link (`eth0`).
- Link-local is still used only for management SSH at `169.254.217.151`.

## Hotspot constraints and iroh-based solutions

Based on iroh endpoint/ticket/relay behavior in the docs:

- iroh quickstart and endpoint model:
  - https://docs.iroh.computer/quickstart
  - https://docs.rs/iroh/latest/iroh/endpoint/
- iroh ticket model (peer addressing):
  - https://docs.rs/iroh-tickets/latest/iroh_tickets/endpoint/struct.EndpointTicket.html
- gossip protocol API:
  - https://docs.rs/iroh-gossip/latest/iroh_gossip/
- DNS discovery behavior:
  - https://docs.iroh.computer/connecting/dns-discovery

### Practical fixes for hotspot reliability

1. Keep one canonical peer record per host name.
- Already applied in `merge_entries`: newest entry wins, older entries for same `node` are removed.
- This prevents `gold`/`wsl`/`rover` duplicate IDs from inflating peer counts.

2. Keep index registration frequent, gossip slightly slower.
- Use fast register (`30s`) so fresh tickets are always available.
- Use configurable heartbeat interval:
  - `MESH_V3_GOSSIP_INTERVAL_SECS` (default `60`, min `10`).
- On hotspot, setting `30` can improve observed liveness.

3. Keep relay path available as fallback.
- Hotspot NAT can block direct UDP holepunch at times.
- iroh relay-backed connectivity is the fallback when direct path fails.
- Ensure relay URLs are present in endpoint addresses and do not disable relay mode for hotspot nodes.

4. Persist peer cache across restarts.
- Already implemented (`~/dialtone/tmp/mesh-v3-peer-cache.json`).
- Lets rover reconnect to known peers even if index fetch is temporarily failing.

5. Keep index response and node parser backward-compatible.
- Already implemented (`node_id` and `nodes` defaulting on decode).
- Prevents mixed-version clusters from silently dropping heartbeat/register data.

6. Optional: add a small stale-peer TTL at index layer.
- Drop entries not updated for `N` minutes to reduce stale candidates.
- This further lowers wasted joins on dead endpoint IDs after hotspot churn.

## iroh documentation reviewed

- Quickstart (endpoint + ticket flow):
  - https://docs.iroh.computer/quickstart
- Endpoint concepts and address freshness:
  - https://docs.iroh.computer/concepts/endpoints
- Discovery model (DNS/Pkarr/default behavior):
  - https://docs.iroh.computer/concepts/discovery
- DNS discovery guide:
  - https://docs.iroh.computer/connecting/dns-discovery
- DHT discovery option (opt-in):
  - https://docs.iroh.computer/connecting/dht-discovery
- Local discovery (mDNS, opt-in):
  - https://docs.iroh.computer/connecting/local-discovery
- Dedicated relay/discovery infrastructure guidance:
  - https://docs.iroh.computer/deployment/dedicated-infrastructure
