# godiam

[![CI](https://github.com/haoli000/godiam/actions/workflows/ci.yml/badge.svg)](https://github.com/haoli000/godiam/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/haoli000/godiam.svg)](https://pkg.go.dev/github.com/haoli000/godiam)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue.svg)](LICENSE)

A Go implementation of the Diameter protocol (RFC 6733), ported from [freeDiameter](https://www.freediameter.net/).

## Overview

godiam provides a complete implementation of the Diameter base protocol with support for:

- Message encoding/decoding (RFC 6733)
- Dictionary subsystem for protocol definitions
- Peer state machine with connection management
- CER/CEA, DWR/DWA, DPR/DPA handling
- YAML-based configuration
- Dynamic extension management via REST API
- Credit-Control Application (RFC 4006)

## Installation

```bash
go get github.com/haoli000/godiam
```

## Quick Start

### Using the Library

```go
package main

import (
    "fmt"
    "log"

    "github.com/haoli000/godiam/pkg/proto/dictionary"
    "github.com/haoli000/godiam/pkg/proto/message"
    "github.com/haoli000/godiam/pkg/proto/types"
)

func main() {
    // Initialize dictionary
    dict := dictionary.New()
    dict.LoadBaseProtocol()
    dict.LoadCreditControl()

    // Create a CCR message
    msg := message.NewRequest(types.CmdCodeCreditControl, types.AppIdCreditControl)
    msg.AddAVP(message.NewUTF8StringAVP(types.AVPCodeSessionId, types.AVPFlagMandatory, "session123"))
    msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginHost, types.AVPFlagMandatory, "client.example.com"))
    msg.AddAVP(message.NewDiameterIdentityAVP(types.AVPCodeOriginRealm, types.AVPFlagMandatory, "example.com"))

    // Encode to wire format
    data, err := msg.Encode()
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Encoded message: %d bytes\n", len(data))

    // Decode from wire format
    decoded, err := message.DecodeMessage(data)
    if err != nil {
        log.Fatal(err)
    }
    originHost, _ := decoded.GetOriginHost()
    fmt.Printf("Origin-Host: %s\n", originHost)
}
```

### Running the Daemon

```bash
# Build the daemon
go build -o diameterd ./cmd/diameterd

# Generate example configuration
./diameterd --gen-config > diameter.yaml

# Edit the configuration as needed
# Then run the daemon
./diameterd --config diameter.yaml
```

### Configuration

godiam uses YAML configuration instead of the original flex/bison-based format:

```yaml
identity: "diameter.example.com"
realm: "example.com"

listen:
  port: 3868
  enable_tcp: true

local:
  vendor_id: 0
  product_name: "godiam"
  watchdog_interval: 30s

peers:
  - identity: "peer.example.com"
    addresses:
      - "192.168.1.100"
    connect_on_start: true
    persistent: true

applications:
  - id: 4
    type: auth
```

## Project Structure

```plaintext
godiam/
├── cmd/diameterd/          # Main daemon
├── examples/               # Usage examples
├── pkg/
│   ├── proto/              # Protocol library
│   │   ├── types/          # Core Diameter types
│   │   ├── dictionary/     # Dictionary subsystem
│   │   └── message/        # Message encoding/decoding
│   ├── core/               # Core framework
│   │   ├── config/         # Configuration
│   │   ├── extension/      # Extension manager and lifecycle
│   │   ├── routing/        # Message routing engine
│   │   ├── peer/           # Peer state machine
│   │   └── server/         # Server implementation
│   └── extensions/         # Built-in extensions
│       ├── admin_api/      # REST API for extension management
│       ├── app_cc/         # Credit-Control Application
│       ├── app_gx/         # 3GPP Gx (Policy and Charging)
│       ├── app_s6a/        # 3GPP S6a (HSS interface)
│       ├── dbg_msg_timings/ # Request-to-answer latency tracking
│       ├── fifo_stats/    # Queue depth & time-in-queue statistics
│       ├── prom_metrics/   # Prometheus metrics endpoint
│       ├── rt_busypeers/   # Busy peer retry (TOO_BUSY failover)
│       ├── rt_deny_by_size/ # Message size limit enforcement
│       ├── rt_ereg/        # Regex-based routing
│       ├── rt_load_balance/ # Least-loaded peer selection
│       ├── rt_randomize/   # Random tie-breaking for equal peers
│       ├── rt_redirect/    # Redirect indication caching (RFC 6733 §6.12)
│       ├── rt_rewrite/     # AVP rewriting
│       └── ...             # See pkg/extensions/ for full list
└── go.mod
```

## Supported RFCs

- **RFC 6733** - Diameter Base Protocol
- **RFC 4006** - Diameter Credit-Control Application

## Comparison with freeDiameter

| Feature       | freeDiameter     | godiam               |
| ------------- | ---------------- | -------------------- |
| Language      | C                | Go                   |
| Config Format | flex/bison       | YAML                 |
| Dictionary    | XML files        | Go code              |
| Transport     | TCP, SCTP        | TCP, SCTP (Linux)    |
| TLS           | OpenSSL          | crypto/tls           |
| Extensions    | Shared libraries | Go plugins (planned) |

## Dynamic Extension Management

godiam supports runtime extension management through a built-in REST API. All extensions are compiled into the binary and can be enabled, disabled, reconfigured, and restarted at runtime without restarting the daemon.

### Admin API

The `admin_api` extension starts an HTTP server (default `127.0.0.1:9001`) exposing management endpoints:

| Method | Path                                | Description                                  |
| ------ | ----------------------------------- | -------------------------------------------- |
| GET    | `/api/v1/extensions`                | List all extensions with state               |
| GET    | `/api/v1/extensions/{name}`         | Get single extension detail                  |
| POST   | `/api/v1/extensions/{name}/enable`  | Enable with optional config body             |
| POST   | `/api/v1/extensions/{name}/disable` | Disable (requires Stoppable)                 |
| PUT    | `/api/v1/extensions/{name}/config`  | Live reconfigure (requires Reconfigurable)   |
| POST   | `/api/v1/extensions/{name}/restart` | Disable + re-enable with optional new config |
| GET    | `/api/v1/extensions/{name}/health`  | Health check (requires HealthCheckable)      |

Add `?persist=true` to any mutating endpoint to save the updated extension configuration to disk.

### Example Usage

```bash
# List all extensions and their states
curl http://localhost:9001/api/v1/extensions

# Enable an extension with configuration
curl -X POST http://localhost:9001/api/v1/extensions/dbg_monitor/enable \
  -d '{"config": {"port": 9000}}'

# Disable an extension
curl -X POST http://localhost:9001/api/v1/extensions/dbg_monitor/disable

# Live-reconfigure an extension
curl -X PUT http://localhost:9001/api/v1/extensions/rt_rewrite/config \
  -d '{"config": {"rules": [...]}}'

# Restart with new config and persist to disk
curl -X POST "http://localhost:9001/api/v1/extensions/app_cc/restart?persist=true" \
  -d '{"config": {"key": "value"}}'
```

### Extension Lifecycle Interfaces

Extensions can opt into dynamic management by implementing optional interfaces:

- **Stoppable** (`Stop() error`) -- required for disable/restart
- **Reconfigurable** (`Reconfigure(config map[string]interface{}) error`) -- required for live reconfiguration
- **HealthCheckable** (`HealthCheck() (string, map[string]interface{})`) -- required for health endpoint

Extensions that only implement the base `Extension` interface can be enabled at startup but cannot be disabled or reconfigured at runtime. The API returns clear error messages indicating which capability is missing.

### Configuration

To enable the admin API, add it to your `diameter.yaml`:

```yaml
extensions:
  - name: "admin_api"
    config:
      port: 9001           # default: 9001
      bind_address: "127.0.0.1"  # default: 127.0.0.1
```

## Prometheus Metrics

The `prom_metrics` extension exposes a Prometheus-compatible `/metrics` endpoint with Diameter-specific metrics, Go runtime stats, and process metrics.

### Configuration

```yaml
extensions:
  - name: "prom_metrics"
    config:
      port: 9090           # default: 9090
```

### Exposed Metrics

| Metric | Type | Labels | Description |
| ------ | ---- | ------ | ----------- |
| `diameter_server_info` | gauge | identity, realm, version | Server identity (always 1) |
| `diameter_server_uptime_seconds` | gauge | | Seconds since server start |
| `diameter_peers_total` | gauge | | Number of configured peers |
| `diameter_peer_up` | gauge | peer, realm | 1 if peer connection is open |
| `diameter_peer_messages_sent_total` | gauge | peer, realm | Messages sent to peer |
| `diameter_peer_messages_received_total` | gauge | peer, realm | Messages received from peer |
| `diameter_peer_bytes_sent_total` | gauge | peer, realm | Bytes sent to peer |
| `diameter_peer_bytes_received_total` | gauge | peer, realm | Bytes received from peer |
| `diameter_peer_connection_duration_seconds` | gauge | peer, realm | Current connection duration |
| `diameter_routing_requests_total` | gauge | | Requests entering the router |
| `diameter_routing_relayed_total` | gauge | | Requests relayed to another peer |
| `diameter_routing_dispatched_total` | gauge | | Requests dispatched locally |
| `diameter_routing_errors_total` | gauge | | Routing errors |
| `diameter_routing_loops_detected_total` | gauge | | Loop detections |
| `diameter_routing_answers_relayed_total` | gauge | | Answers relayed back |
| `diameter_dictionary_vendors` | gauge | | Vendors in dictionary |
| `diameter_dictionary_applications` | gauge | | Applications in dictionary |
| `diameter_dictionary_avps` | gauge | | AVPs in dictionary |
| `diameter_dictionary_commands` | gauge | | Commands in dictionary |
| `diameter_extensions_total` | gauge | state | Extensions by state |

Standard Go runtime metrics (`go_*`) and process metrics (`process_*`) are also included.

### Example

```bash
curl http://localhost:9090/metrics
```

### Behavioral differences

- Local dispatch fallback: when a request is explicitly targeted to the local host (Destination-Host matches) but no dispatch handler is registered for its application, godiam returns `DIAMETER_APPLICATION_UNSUPPORTED` (3007). freeDiameter would forward such a request if relaying is enabled; this divergence is intentional to align with RFC 6733’s expectation that a final recipient either processes the request or answers with an error, rather than forwarding it.
- SCTP support is available on Linux builds (uses `github.com/ishidawataru/sctp`). On non-Linux platforms SCTP is stubbed and will return “SCTP not supported on this platform.”

## Testing

### Unit Tests

```bash
go test ./...
```

### Integration Tests

godiam includes comprehensive integration tests to verify functionality and interoperability:

#### DRA Test (godiam only)

Tests godiam as a Diameter Routing Agent with AVP rewriting:

```bash
cd tests/dra
make build && make test
```

#### DRA Performance Test

Measures DRA throughput and latency, with Prometheus metrics reported at the end:

```bash
cd tests/dra_perf
make build && make test
```

See [tests/dra_perf/README.md](tests/dra_perf/README.md) for details.

#### Integration Test (godiam + freeDiameter)

Tests interoperability between godiam and freeDiameter implementations:

- Left side: freeDiameter (dra1, peer11, peer12)  
- Right side: godiam (dra2, peer2)

```bash
cd tests/dra_integration
make build && make test
```

This validates that godiam can successfully interoperate with freeDiameter, ensuring protocol compliance and cross-implementation compatibility.

See [tests/dra_integration/README.md](tests/dra_integration/README.md) for details.

#### Extension Tests

Each routing/debugging extension has its own Docker-based functional test:

```bash
cd tests/<extension_name>
make build && make test
```

Available extension tests: `rt_hide_oh`, `rt_ignore_dh`, `rt_session_bind`,
`rt_load_balance`, `rt_deny_by_size`, `rt_ereg`, `dbg_msg_timings`, `rt_redirect`.

See [tests/README.md](tests/README.md) for the full test matrix.

## More Examples

### Basic Client/Server

A simple example demonstrating basic message exchange:

```bash
go run examples/basic/main.go
```

### HSS-Lite (S6a)

A simulated Home Subscriber Server (HSS) handling S6a Update-Location-Requests (ULR):

```bash
cd examples/hss_lite
go run main.go
```

This example simulates 3GPP S6a interface transactions with subscriber lookup and AVPs from TS 29.272.

## License

See LICENSE file.

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.
