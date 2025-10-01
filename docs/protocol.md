# Secure Tunnel Protocol (STP)

This document describes the Go implementation that ships with `veildeploy`. The
runtime establishes an encrypted overlay between clients and servers and now
includes a pluggable dataplane for routing packets to one or more logical
peers.

## Architectural Priorities

1. **Authenticated channel** 每 mutual PSK-backed X25519 handshake, TLS-looking
   framing, replay protection, deterministic padding, and periodic rekeying.
2. **Operational safety** 每 adaptive keepalives, per-session statistics,
   structured logging, and graceful shutdown of clients and servers.
3. **Multi-peer routing** 每 declarative peer definitions, CIDR-based forwarding,
   and a dataplane abstraction that can be mapped to loopback queues today or a
   system TUN/TAP device in the future.

## Protocol Flow

1. **ClientHello / ServerHello** 每 parties exchange ephemeral X25519 keys,
   authenticate via the pre-shared secret, and negotiate keepalive/padding
   parameters.
2. **Transport Bind** 每 the client acknowledges the session so the server can
   lock the tuple to the negotiated secrets.
3. **Data Phase** 每 encrypted frames carry a peer identifier plus the opaque
   payload. Keepalives obey the negotiated cadence and each frame advances the
   monotonically increasing counter for replay resistance.
4. **Rekey** 每 when counters or timers overflow, either side can rotate keys via
   the rekey exchange without collapsing the tunnel.

## Record Layout

```
+-----------+-----------+-----------+------------------+
| Preamble  | Header    | Counter   | Ciphertext       |
+-----------+-----------+-----------+------------------+
```

- **Preamble (5B)** 每 TLS application-data wrapper (`0x17 0x03 0x03` + length).
- **Header (2B)** 每 masked flag bits (`data`, `keepalive`, `rekey`, `bind`) and
  padding length.
- **Counter (8B)** 每 big-endian per-direction sequence number; retrograde
  counters are rejected.
- **Ciphertext** 每 ChaCha20-Poly1305 over a length-prefixed payload that embeds
  the peer identifier and user data.

## Dataplane Integration

- The device layer consumes `packet.Packet` frames and hands them to a pluggable
  dataplane (`internal/dataplane`).
- The default implementation is an in-memory loopback used for tests and local
  development. Deployments can select `tunnel.type = "udp-bridge"` to relay
  frames through a UDP socket bound to `tunnel.listen`, forwarding traffic to the
  per-peer `endpoint` declared in the configuration.
- Peers are configured in JSON with a name and a list of `allowedIPs`. The
  device builds a routing table from these prefixes to choose the correct peer
  when injecting outbound traffic.

## Configuration & Management

- `config/config.json` (or an alternate path via CLI) now requires at least one
  peer definition and exposes a `tunnel.type` selector (currently `loopback`).
- Runtime state is exposed via the local HTTP management endpoint (default
  `127.0.0.1:7777`) which returns session metadata, peer snapshots, limiter
  statistics, and last-activity timestamps. A Prometheus-compatible `/metrics`
  endpoint surfaces key counters for automation.
- Management traffic is gated by a CIDR-based ACL (`management.acl`, defaulting
  to loopback). Changes to the configuration file are detected at runtime and
  automatically applied to the management ACL and connection limiter budgets.
- Connection admission is protected by a token-bucket limiter and a configurable
  maximum concurrent session count.

## Testing Notes

The repository primarily targets integration testing via the loopback dataplane.
Future work includes adding automated coverage for the new routing pipeline and
additional dataplane implementations.
