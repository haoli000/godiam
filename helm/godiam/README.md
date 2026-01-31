# godiam Helm Chart

Deploy godiam as a Diameter Routing Agent (DRA) on Kubernetes.

## Architecture

```
                     K8s Service              K8s Service
Diameter Clients --> godiam-dra:3868 -------> godiam-server:3868
                 <-- (Deployment)    <------- (Deployment, optional)
                                              runs app_cc handler

diameterbench Job (optional) --> godiam-dra:3868
```

- **DRA** (always deployed): Relays Diameter messages between clients and backend servers. Accepts unknown peers by default.
- **Server** (optional): Backend Diameter server handling Credit-Control Requests via the `app_cc` extension.
- **diameterbench** (optional): Benchmark client that generates CCR load against the DRA, runs as a Kubernetes Job.

## Quick Start

```bash
# Deploy DRA + backend server
helm install godiam helm/godiam/

# DRA only (no backend server)
helm install godiam helm/godiam/ --set server.enabled=false

# With benchmark job
helm install godiam helm/godiam/ --set bench.enabled=true
```

## Values

### Image

| Key                | Default        | Description                              |
| ------------------ | -------------- | ---------------------------------------- |
| `image.repository` | `godiam`       | Container image repository               |
| `image.tag`        | `""`           | Image tag (defaults to Chart appVersion) |
| `image.pullPolicy` | `IfNotPresent` | Image pull policy                        |

### DRA

| Key                            | Default                 | Description                                              |
| ------------------------------ | ----------------------- | -------------------------------------------------------- |
| `dra.replicas`                 | `1`                     | Number of DRA replicas                                   |
| `dra.identity`                 | `dra.test.realm`        | Diameter identity (FQDN)                                 |
| `dra.realm`                    | `test.realm`            | Diameter realm                                           |
| `dra.port`                     | `3868`                  | Diameter listen port                                     |
| `dra.transport`                | `[tcp]`                 | Transport protocols to enable: `tcp`, `sctp`, or both    |
| `dra.peersPolicy.allowUnknown` | `true`                  | Accept connections from unknown peers                    |
| `dra.routing.relayEnabled`     | `true`                  | Enable message relaying                                  |
| `dra.routing.defaultRealm`     | `backend.realm`         | Default routing realm                                    |
| `dra.local.vendorId`           | `0`                     | Vendor ID                                                |
| `dra.local.productName`        | `godiam-dra`            | Product name in CER/CEA                                  |
| `dra.local.watchdogInterval`   | `30s`                   | DWR interval                                             |
| `dra.local.connectTimeout`     | `10s`                   | Peer connection timeout                                  |
| `dra.local.requestTimeout`     | `30s`                   | Request timeout                                          |
| `dra.applications`             | `[{id: 4, type: auth}]` | Supported Diameter applications                          |
| `dra.extensions`               | `[]`                    | Additional extensions (prom_metrics added automatically) |
| `dra.metrics.enabled`          | `true`                  | Enable Prometheus metrics endpoint                       |
| `dra.metrics.port`             | `9090`                  | Prometheus metrics port                                  |
| `dra.resources`                | `{}`                    | CPU/memory resource requests/limits                      |
| `dra.nodeSelector`             | `{}`                    | Node selector                                            |
| `dra.tolerations`              | `[]`                    | Tolerations                                              |
| `dra.affinity`                 | `{}`                    | Affinity rules                                           |

### Server (optional)

| Key                               | Default                 | Description                                                         |
| --------------------------------- | ----------------------- | ------------------------------------------------------------------- |
| `server.enabled`                  | `true`                  | Deploy backend server                                               |
| `server.replicas`                 | `1`                     | Number of server replicas                                           |
| `server.identity`                 | `server.backend.realm`  | Diameter identity                                                   |
| `server.realm`                    | `backend.realm`         | Diameter realm                                                      |
| `server.port`                     | `3868`                  | Diameter listen port                                                |
| `server.transport`                | `[tcp]`                 | Transport protocols to enable: `tcp`, `sctp`, or both               |
| `server.peersPolicy.allowUnknown` | `true`                  | Accept unknown peers                                                |
| `server.local.*`                  | *(same as DRA)*         | Local configuration                                                 |
| `server.applications`             | `[{id: 4, type: auth}]` | Supported applications                                              |
| `server.extensions`               | `[]`                    | Additional extensions (app_cc and prom_metrics added automatically) |
| `server.metrics.enabled`          | `true`                  | Enable Prometheus metrics                                           |
| `server.metrics.port`             | `9090`                  | Prometheus metrics port                                             |
| `server.resources`                | `{}`                    | CPU/memory resources                                                |

### Benchmark (optional)

| Key                      | Default         | Description                        |
| ------------------------ | --------------- | ---------------------------------- |
| `bench.enabled`          | `false`         | Run benchmark job                  |
| `bench.connections`      | `10`            | Concurrent Diameter connections    |
| `bench.duration`         | `10s`           | Test duration                      |
| `bench.rate`             | `1000`          | Target requests per second         |
| `bench.transport`        | `tcp`           | Outbound protocol: `tcp` or `sctp` |
| `bench.connectInterval`  | `10ms`          | Delay between starting each worker |
| `bench.connectRetries`   | `3`             | Max retry attempts per worker      |
| `bench.connectTimeout`   | `10s`           | Per-attempt connect timeout        |
| `bench.destinationRealm` | `backend.realm` | Destination-Realm in CCR messages  |
| `bench.resources`        | `{}`            | CPU/memory resources               |

## Examples

### High-throughput benchmark

```bash
helm install godiam helm/godiam/ \
  --set bench.enabled=true \
  --set bench.rate=5000 \
  --set bench.connections=20 \
  --set bench.duration=30s
```

### DRA with custom identity and realm

```bash
helm install godiam helm/godiam/ \
  --set dra.identity=dra.production.example.com \
  --set dra.realm=example.com \
  --set server.identity=server.backend.example.com \
  --set server.realm=backend.example.com \
  --set dra.routing.defaultRealm=backend.example.com
```

### DRA only (relay to external servers)

```bash
helm install godiam helm/godiam/ --set server.enabled=false
```

In this mode the DRA accepts connections from any peer (`allowUnknown: true`) but has no preconfigured backend. Configure additional peers and routing via custom extensions or by overriding the ConfigMap.

### With resource limits

```bash
helm install godiam helm/godiam/ \
  --set dra.resources.requests.cpu=500m \
  --set dra.resources.requests.memory=256Mi \
  --set dra.resources.limits.cpu=1 \
  --set dra.resources.limits.memory=512Mi
```

## SCTP Transport

godiam supports SCTP as an alternative to TCP for Diameter transport (RFC 6733 mandates SCTP as the primary transport). The `transport` field is a list, so you can enable TCP, SCTP, or both:

```bash
# Enable both TCP and SCTP listeners, bench uses SCTP
helm install godiam helm/godiam/ \
  --set 'dra.transport={tcp,sctp}' \
  --set 'server.transport={tcp,sctp}' \
  --set bench.enabled=true \
  --set bench.transport=sctp

# SCTP only (no TCP listener)
helm install godiam helm/godiam/ \
  --set 'dra.transport={sctp}' \
  --set 'server.transport={sctp}' \
  --set bench.enabled=true \
  --set bench.transport=sctp
```

Or using the Makefile:

```bash
make stress BENCH_TRANSPORT=sctp
```

When both protocols are enabled, outbound peer connections prefer SCTP (per RFC 6733). TCP is always used for init container readiness checks.

SCTP requires Linux nodes with the `sctp` kernel module loaded. The container image includes `libsctp1` for SCTP runtime support.

## Stress Test Results

Benchmark results from a 3-node kind cluster (1 control-plane, 2 workers) with a single DRA and single server replica. The test topology is:

```
diameterbench (Job) --[N conns]--> godiam-dra --[1 conn]--> godiam-server (app_cc)
```

All tests ran for 30 seconds. The DRA relays CCR/CCA messages to the backend server.

### TCP

| Target Rate | Connections | Throughput | Avg Latency | P99 Latency | Loss |
| ----------- | ----------- | ---------- | ----------- | ----------- | ---- |
| 5,000       | 20          | 4,973/s    | ~0.5ms      | ~2ms        | 0%   |
| 10,000      | 40          | 9,977/s    | ~1ms        | ~4ms        | 0%   |
| 20,000      | 50          | 19,799/s   | ~5ms        | ~33ms       | 0%   |
| 50,000      | 100         | 41,505/s   | 229ms       | ~1s         | ~1%  |

TCP throughput ceiling: **~40,000 req/s** on this environment.

### SCTP

| Target Rate | Connections | Throughput | Avg Latency | P99 Latency | Loss |
| ----------- | ----------- | ---------- | ----------- | ----------- | ---- |
| 5,000       | 20          | 4,996/s    | ~0.6ms      | ~4ms        | 0%   |
| 10,000      | 40          | 9,989/s    | ~2.8ms      | ~26ms       | 0%   |
| 20,000      | 50          | 19,803/s   | ~17ms       | ~227ms      | 0%   |
| 50,000      | 100         | 47,099/s   | ~37ms       | ~449ms      | ~0%  |

SCTP throughput ceiling: **~47,000 req/s** on this environment.

SCTP latency is higher than TCP at the same rate due to kube-proxy (iptables/IPVS) overhead in the kind cluster. Connection reliability at 100 concurrent SCTP associations is ensured by staggered worker starts (10ms interval) and automatic retry with backoff.

### Relay Backpressure at Saturation

When the DRA is pushed beyond its throughput ceiling, the DRA log shows:

```
Routing error (count=1): relay failed: no routes available after exclusions
```

This is the relay backpressure mechanism working as designed. The failure path is:

```
1. Bench sends request to DRA
2. DRA router selects the server peer as the relay target
3. DRA calls peer.Send(msg) to forward the message
4. peer.Send() tries a lock-free ring buffer (8192 slots),
   then falls back to a channel (1024 slots)
5. Both buffers are full -> Send() returns "send queue full"
6. Relay loop adds the server peer to the exclusion map
7. No other candidates exist -> "no routes available after exclusions"
8. DRA returns DIAMETER_UNABLE_TO_DELIVER (3002) to the client
```

The single DRA-to-server connection cannot drain messages as fast as 100 bench connections are producing them. This is a fundamental single-peer bottleneck -- the send buffers (8192 + 1024 = 9216 slots) act as the backpressure boundary.

The related bench-side error `"Worker N timed out connecting"` is a secondary effect: the DRA's goroutines are busy processing the relay backlog and cannot complete CER/CEA handshakes for new connections within the configured timeout.

Mitigation strategies for production deployments:
- Scale the server to multiple replicas with realm-based load distribution
- Increase DRA send buffer sizes (requires code change to `ringBufferSize` in `peer/writer.go`)
- Use direct peer connections (bypass the DRA relay for high-volume flows)

## Prometheus Metrics

When `metrics.enabled` is `true` (default), both the DRA and server expose a `/metrics` endpoint with Diameter-specific metrics:

- `diameter_routing_requests_total` - Requests entering the router
- `diameter_routing_relayed_total` - Requests relayed to another peer
- `diameter_routing_dispatched_total` - Requests dispatched locally
- `diameter_routing_errors_total` - Routing errors
- `diameter_peer_up{peer,realm}` - Peer connection status
- `diameter_peer_messages_sent_total{peer,realm}` - Messages sent per peer

Scrape targets:
- DRA: `<release>-dra:9090/metrics`
- Server: `<release>-server:9090/metrics`

## Service Discovery

When `server.enabled` is `true`, the DRA is automatically configured to peer with the backend server using Kubernetes DNS:

```
<release>-server.<namespace>.svc.cluster.local:3868
```

Realm routing is set so that messages with `Destination-Realm: backend.realm` are relayed to the server.

## Uninstall

```bash
helm uninstall godiam
```
