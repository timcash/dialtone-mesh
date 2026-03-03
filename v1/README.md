# Mesh Mod (`v1`)

The `mesh` mod provides a high-performance, UDP-based communication layer for the Dialtone system. It is built on top of [libudx](libudx/), providing reliable, multiplexed, and congestion-controlled streams.

## Features

- **High Performance**: Leverages `libudx` for low-latency UDP communication.
- **Mesh-Aware**: Built-in orchestration for deploying, building, and joining nodes across the LAN mesh.
- **Cross-Platform**: Fully supports Linux and macOS (ARM64/x86_64) with automated toolchain selection (`clang`/`gcc`).
- **Nix-Integrated**: Reproducible builds and environment management via Nix.

## Command API

```bash
./dialtone.sh mesh v1 <command> [args]
```

### Core Commands

- **`install`**: Installs build dependencies (CMake, Ninja, libuv, etc.).
- **`build [--arch host|all] [--host name|all]`**: Compiles the mesh C binary. 
  - Automatically handles macOS environment (`CGO_ENABLED=0`).
  - Uses `clang` on Darwin for stability.
- **`test`**: Runs local self-tests to verify UDP stream reliability.
- **`start`**: Starts the local mesh runtime (background by default).
- **`join [--host name|all]`**: The "One-Touch" command to build and start the mesh on local or remote hosts using the mod itself.
- **`deploy`**: High-level orchestration to install, build, and start the mesh on target hosts.
- **`shell-server`**: Serves the `dialtone.sh` bootstrap script via the mesh binary's C-based HTTP mode.

## Development

The mesh mod is a hybrid mod:
- **Go Orchestrator**: Logic lives in `go/mesh.go`.
- **C Runtime**: The core binary is `mesh_v1.c`, linked against the `libudx` submodule.

### Local Build Example
```bash
./dialtone.sh mesh v1 build --arch host
```

### Mesh Join Example
```bash
./dialtone.sh mesh v1 join --host gold
```

## Internal Components

- **[go/](go/)**: Go implementation of the mod CLI and remote orchestration logic.
- **[libudx/](libudx/)**: C implementation of the UDX protocol (Reliable UDP).
- **[bin/](bin/)**: Destination for compiled native binaries.

---
*Note: This mod requires an active Nix environment or local installation of C build tools.*
