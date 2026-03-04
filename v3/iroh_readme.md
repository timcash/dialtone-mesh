# iroh in mesh/v3

This is a practical guide for building a peer-to-peer app with iroh in this repo.

## Mental model

- **Endpoint**: your live local node instance (process + keys + sockets + transports).
- **Ticket**: a portable connection hint for another peer to reach that endpoint.
- **Node ID**: the stable cryptographic identity of an endpoint (public key-derived id).
- **Relay / direct addr**: candidate network paths for reaching the endpoint.

Think of it like:
- Endpoint = running server/client identity right now
- Ticket = "business card" you hand to others so they can dial you

## Endpoint vs ticket

## Endpoint

An endpoint is created by your app at runtime (for example `Endpoint::bind()` in Rust).

It has:
- node id
- direct addresses (LAN/public if available)
- relay addresses
- supported protocols (ping, gossip, blobs, etc)

The endpoint is what actually sends/receives traffic.

## Ticket

A ticket is serialized endpoint reachability info.  
Other peers consume it to connect without manual IP exchange.

In mesh/v3, you currently:
- print a ticket at startup
- register it in the index
- fetch tickets for other peers and connect/join gossip

Important:
- tickets are not identity by themselves; node id is the identity anchor
- tickets can become stale as addresses change, so refresh/heartbeat matters

## FQDN vs endpoint vs ticket

- **FQDN** (example: `index.dialtone.earth`) is just DNS for a web/API service.
- **Endpoint** is the live iroh node in each peer process.
- **Ticket** is peer connection metadata for one endpoint.

Your app typically uses all three:
- FQDN for discovery/index API
- tickets for dialing peers
- endpoint for actual peer traffic

## iroh protocol roles

Use each protocol for a specific job:

- **iroh-ping**
  - liveness / RTT checks to peer endpoints
  - good for diagnostics and path quality

- **iroh-gossip**
  - low-latency peer pub/sub
  - presence, heartbeats, signaling, small events

- **iroh-blobs**
  - content-addressed binary transfer/storage
  - large payloads by hash

- **iroh-docs / iroh-automerge (CRDT layer)**
  - collaborative shared state
  - conflict-free replicated data
  - reference blob hashes from CRDT docs for large data

Suggested architecture:
- gossip for control plane
- docs/automerge for replicated app state
- blobs for large objects

## What index should store

Keep index small and explicit. Store enough for discovery, not full app state.

Recommended per-node record:

- `node_name` (human label, may change)
- `node_id` (stable identity, required)
- `ticket` (latest serialized endpoint ticket, required)
- `updated_at_unix` (heartbeat time, required)
- `capabilities` (optional: `["gossip","blobs","docs"]`)
- `version` (optional: app/protocol version)
- `meta` (optional: display fields)

Example:

```json
{
  "node_name": "gold",
  "node_id": "47f2720c4108acd6ae54da066521dd628e8ce90e112f9f96fe2413234ecac168",
  "ticket": "endpointab...",
  "updated_at_unix": 1772594000,
  "capabilities": ["gossip", "blobs"],
  "version": "mesh-v3"
}
```

## What index should NOT do

- Do not be a permanent source of truth for collaborative app data.
- Do not proxy peer traffic.
- Do not replace peer identity verification.

Index is discovery/bootstrap only.

## Peer discovery flow (recommended)

1. Node starts endpoint.
2. Node builds fresh ticket from current endpoint.
3. Node registers to index with `node_id + ticket + heartbeat`.
4. Node fetches peer list.
5. Node filters:
   - skip self `node_id`
   - skip stale entries
   - optional capability/version checks
6. Node joins peers (gossip) using ticket-derived ids.
7. Node continues heartbeat + periodic refresh.

If index goes down:
- existing connected peers can keep gossiping
- new discovery and rejoin quality degrades until index returns

## Identity and trust

For a real app, verify identity against node id:
- tie permissions to `node_id`, not `node_name`
- treat `node_name` as UI label only
- consider signed metadata if you need stronger trust guarantees

## Practical guidance for mesh/v3

Today in this repo:
- `node` mode runs endpoint + ping + gossip + auto-register
- `index` mode runs HTTP/WebSocket peer registry
- `hub` mode runs both in one process

To keep it robust:
- keep heartbeat at 30-60s
- expire peers if stale (for example > 2 heartbeat windows)
- include `node_id` in index API response explicitly
- keep one canonical ticket per node id (latest write wins)

## Point-to-point video streaming (best pattern)

Given you already run gossip, use a split design:

- **gossip = control plane**
  - stream announce
  - offer/answer metadata
  - receiver feedback (bitrate/loss/latency)
- **direct iroh stream (custom ALPN) = media plane**
  - actual live video bytes over QUIC streams

Suggested ALPN:

- `mesh-v3/video/1`

Why this is best:

- gossip is excellent for signaling and low-volume coordination
- live video needs low-latency ordered transport with backpressure
- iroh blobs are better for non-live objects (recordings/snapshots), not real-time frames

### Practical flow

1. Sender advertises stream on gossip (`stream.announce`).
2. Receiver sends subscribe/offer message on gossip.
3. Sender opens direct iroh stream with ALPN `mesh-v3/video/1`.
4. Sender streams encoded frames (H.264 or AV1) in small frame packets.
5. Receiver decodes and renders with a small jitter buffer.
6. Receiver periodically sends stats on gossip; sender adjusts bitrate/keyframe interval.

### Frame packet fields (minimal)

- `stream_id`
- `seq`
- `timestamp_ms`
- `codec`
- `keyframe` (bool)
- `payload_len`
- `payload`

### When to use blobs

Use `iroh-blobs` for:
- replay/VOD chunks
- recording upload/download
- thumbnails/snapshots

Do not use blobs for the live frame loop.
