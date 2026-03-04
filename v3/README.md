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
