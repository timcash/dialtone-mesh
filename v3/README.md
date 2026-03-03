# mesh v3 (iroh quickstart)

Minimal point-to-point ping between two machines using iroh tickets.

## Build (inside Dialtone env)

```bash
cd ~/dialtone/src/mods/mesh/v3
~/dialtone/dialtone.sh nix develop . -c cargo build
```

## Run receiver (example: wsl)

```bash
cd ~/dialtone/src/mods/mesh/v3
~/dialtone/dialtone.sh nix develop . -c cargo run -- receiver
```

This prints an `EndpointTicket` string.

## Run sender (example: gold)

```bash
cd ~/dialtone/src/mods/mesh/v3
~/dialtone/dialtone.sh nix develop . -c cargo run -- sender '<PASTE_TICKET_HERE>'
```

If successful, sender prints round-trip latency.
