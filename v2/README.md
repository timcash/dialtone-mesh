# mesh v2 build (WSL + gold)

This build follows upstream `libudx` instructions on GitHub (`npm install`, `bare-make generate`, `bare-make build`) and keeps the local workflow minimal.

## One command (recommended)

```bash
cd ~/dialtone/src/mods/mesh/v2
~/dialtone/dialtone.sh make mesh
```

This does:
1. `npm install` in `libudx`
2. `npx bare-make generate`
3. `npx bare-make build`
4. builds `dialtone_mesh_v2_<arch>`

## Build only libudx examples

```bash
cd ~/dialtone/src/mods/mesh/v2
~/dialtone/dialtone.sh make examples
```

Example binaries:
- `libudx/build/examples/client`
- `libudx/build/examples/server`
- `libudx/build/examples/udxperf`

## Why this is the same on WSL and gold

Use `~/dialtone/dialtone.sh ...` on both hosts so `npm`, `npx`, C/C++ toolchain, and build tools come from the same Nix environment.
