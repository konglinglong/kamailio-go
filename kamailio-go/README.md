# Kamailio-Go

Open source SIP server implementation in Go, targeting 3GPP IMS (VoLTE/VoNR) deployments.

## Project Status

Active development. Core SIP stack, transport layers, and a wide catalogue
of Kamailio modules are ported to Go under `internal/`. See
[docs/superpowers/plans/2026-06-21-complete-sip-stack-roadmap.md](docs/superpowers/plans/2026-06-21-complete-sip-stack-roadmap.md)
for the current roadmap.

## Quick Start

```bash
make dev     # build
make test    # run tests
make bench   # benchmarks
```

## Architecture

- `cmd/kamailio` - Main entry point (`run`, `check-config`, `test`, `version`)
- `cmd/kamcmd`   - RPC client
- `internal/core/` - Core SIP server functionality
  - `parser/` - SIP message parsing
  - `transport/` - UDP/TCP/TLS transport
  - `proxy/` - Request pipeline and subsystem wiring
  - `app/` - Bootstrap / FullStack launchers
- `internal/ims/` - IMS (P-CSCF/I-CSCF/S-CSCF) functionality
- `internal/integration/` - End-to-end and pipeline tests
- `internal/modules/` - ~190 Kamailio module ports, including:
  - `tm/` - Transaction Management
  - `sctp/` - Real SCTP SOCK_SEQPACKET transport
  - `websocket/` - RFC 6455 / RFC 7118 SIP-over-WebSocket
  - `cdp/` - Diameter base protocol (peer state machine, CER/CEA, DWR/DWA,
    DPR/DPA, TCP transport, transaction table)
  - `cdp_avp/` - Diameter AVP builders/parsers for IMS

## Building

```bash
go build -o bin/kamailio ./cmd/kamailio
```

## License

GPL-2.0 (matching Kamailio original project)

