# godiam Tests

Comprehensive test suite for godiam Diameter implementation.

## Test Types

### Unit Tests

Standard Go unit tests throughout the codebase:

```bash
go test ./...
```

### Integration Tests

#### 1. DRA Test (godiam only)

Location: `dra/`

Tests godiam as a Diameter Routing Agent with:

- Two interconnected DRA instances
- Multiple peers on each side
- AVP rewriting (rt_rewrite extension)
- Cross-realm routing

**Quick Start:**

```bash
cd dra
make build && make test
```

See [dra/README.md](dra/README.md) for details.

#### 2. DRA Integration Test (godiam + freeDiameter)

Location: `dra_integration/`

Tests interoperability between godiam and freeDiameter:

- **Left side**: freeDiameter (dra1, peer11, peer12)
- **Right side**: godiam (dra2, peer2)
- Cross-implementation message routing
- Protocol compliance verification

**Quick Start:**

```bash
cd dra_integration
make build && make test
```

See [dra_integration/README.md](dra_integration/README.md) for details.

#### 2b. DRA Performance Test

Location: `dra_perf/`

Measures throughput and latency of godiam DRA message routing.

#### 3. rt_hide_oh Test

Location: `rt_hide_oh/`

Tests the rt_hide_oh extension (Origin-Host hiding).

#### 4. rt_ignore_dh Test

Location: `rt_ignore_dh/`

Tests the rt_ignore_dh extension (Destination-Host ignoring).

#### 5. rt_session_bind Test

Location: `rt_session_bind/`

Tests the rt_session_bind extension (session-affinity routing). Validates that multiple requests with the same Session-Id are routed to the same backend peer.

#### 6. rt_load_balance Test

Location: `rt_load_balance/`

Tests the rt_load_balance extension (least-loaded peer selection). Sends 10 requests through a gateway with two backends and verifies load is distributed across both.

#### 7. rt_deny_by_size Test

Location: `rt_deny_by_size/`

Tests the rt_deny_by_size extension (message size enforcement). Configures a 100-byte limit and verifies that standard Diameter messages are rejected for exceeding it.

#### 8. rt_ereg Test

Location: `rt_ereg/`

Tests the rt_ereg extension (regex-based routing). Configures a regex rule to boost a specific peer and verifies that matching messages are routed to the boosted peer.

#### 9. dbg_msg_timings Test

Location: `dbg_msg_timings/`

Tests the dbg_msg_timings extension (request-to-answer latency tracking) and fifo_stats extension (per-peer queue statistics). Verifies timing entries are logged.

#### 10. rt_redirect Test

Location: `rt_redirect/`

Tests the rt_redirect extension (redirect indication caching per RFC 6733 §6.12). Verifies extension initialization and normal message flow with the extension active.

#### 11. Diameter Edge Agent Test

Location: `dea_integration/`

Tests godiam as a Diameter Edge Agent facing roaming partners:

- A DEA with an internal zone (`core`) and an external zone (`roaming`)
- An internal application node plus three partners: well behaved, hostile and flooding
- Asserts partner relaying, topology hiding (pseudonym Origin-Host, no internal
  identity or realm leaked), FS.19 ingress screening (5003), partner rate
  limiting (3004), the `/api/v1/edge` admin endpoint and the edge metrics

**Quick Start:**

```bash
cd dea_integration
make build && make test
```

#### 12. Diameter Edge Agent Performance Test

Location: `dea_perf/`

Measures the cost of the edge controls by benchmarking the same backend twice:
once through a plain relay agent and once through an edge agent running
screening, rate limiting, stateful topology hiding and edge routing. Clients
connect on the external zone as a roaming partner and are admitted by realm
match. Each run asserts that nothing was rejected, throttled or left unhidden,
so a throughput figure cannot come from work the agent skipped.

**Quick Start:**

```bash
cd dea_perf
make build && make test
```

#### 13. Diameter Edge Agent Soak Test

Location: `dea_soak/`

Runs an edge agent through a long run — idle baseline, sustained load,
connect/disconnect churn, a burst, then a long idle drain — sampling CPU, RSS,
heap and goroutines throughout, and judges it on resource behaviour rather than
throughput. The phases separate classes of resource bug: growth under load that
is bounded by a cache cap is expected work, growth while idle is not, and
goroutines and heap must come back down once traffic stops. The drain outlasts
both the pseudonym TTL and Go's forced GC period so that reclamation is
actually observable.

This test found the watchdog defect described in `docs/dea-reference.md`: a peer
that lost a DWA stayed in Suspect forever, silently leaving routing.

**Quick Start:**

```bash
cd dea_soak
make build && make test       # Default: 120s phases
make quick                    # Short run, for validating the harness
make long                     # Extended: 600s phases
```

#### 14. Endurance Test

Location: `endurance/`

Production scenario simulation testing DRA resilience under sustained load. Runs 7 phases: baseline steady state, server failure & failover, server recovery & rebalancing, network partition (iptables packet drop), network recovery, overload burst (3000 req/s spike), and final metrics validation. Uses rt_load_balance, rt_busypeers, fifo_stats, dbg_msg_timings, rt_deny_by_size, and prom_metrics extensions.

**Quick Start:**

```bash
cd endurance
make build && make test       # Default: 20s phases, 500 req/s
make quick                     # Quick: 10s phases, 200 req/s
make stress                    # Stress: 60s phases, 2000 req/s
```

See [endurance/README.md](endurance/README.md) for details.

## Test Matrix

| Test             | Implementation        | Purpose                           |
| ---------------- | --------------------- | --------------------------------- |
| DRA              | godiam only           | Validate godiam DRA functionality |
| DRA Integration  | godiam + freeDiameter | Validate interoperability         |
| DRA Performance  | godiam only           | Measure routing throughput        |
| rt_hide_oh       | godiam only           | Test Origin-Host hiding           |
| rt_ignore_dh     | godiam only           | Test Destination-Host ignoring    |
| rt_session_bind  | godiam only           | Test session-affinity routing     |
| rt_load_balance  | godiam only           | Test load distribution            |
| rt_deny_by_size  | godiam only           | Test message size enforcement     |
| rt_ereg          | godiam only           | Test regex-based routing          |
| dbg_msg_timings  | godiam only           | Test timing & queue tracking      |
| rt_redirect      | godiam only           | Test redirect caching             |
| Endurance        | godiam only           | DRA resilience & failure recovery |
| DEA Integration  | godiam only           | Edge screening, hiding & limiting |
| DEA Performance  | godiam only           | Cost of edge controls vs a relay  |
| DEA Soak         | godiam only           | Long-run CPU, RSS & goroutines    |

## Running All Integration Tests

From the godiam root directory:

```bash
# Run DRA test (godiam only)
cd tests/dra && make test && cd ../..

# Run integration test (godiam + freeDiameter)
cd tests/dra_integration && make test && cd ../..

# Run rt_hide_oh test
cd tests/rt_hide_oh && make test && cd ../..

# Run rt_ignore_dh test
cd tests/rt_ignore_dh && make test && cd ../..

# Run rt_session_bind test
cd tests/rt_session_bind && make test && cd ../..

# Run rt_load_balance test
cd tests/rt_load_balance && make test && cd ../..

# Run rt_deny_by_size test
cd tests/rt_deny_by_size && make test && cd ../..

# Run rt_ereg test
cd tests/rt_ereg && make test && cd ../..

# Run dbg_msg_timings test
cd tests/dbg_msg_timings && make test && cd ../..

# Run rt_redirect test
cd tests/rt_redirect && make test && cd ../..

# Run Diameter Edge Agent test
cd tests/dea_integration && make build && make test && cd ../..

# Run Diameter Edge Agent performance test
cd tests/dea_perf && make build && make test && cd ../..

# Run Diameter Edge Agent soak test (CPU/memory/goroutine monitoring)
cd tests/dea_soak && make build && make test && cd ../..

# Run endurance test (requires --cap-add NET_ADMIN for iptables phases)
cd tests/endurance && make build && make test && cd ../..
```

## Test Requirements

### For godiam-only tests

- Docker
- Make

### For integration tests (godiam + freeDiameter)

- Docker
- Make
- Access to `haoli1/freediameter:latest` image (or build freeDiameter locally)

## Debugging Tests

### View logs in running container

```bash
docker ps  # Get container ID
docker exec -it <container_id> bash
ls /tmp/*.log
tail -f /tmp/dra1.log
```

### Run in manual mode (keeps container running)

```bash
cd <test_directory>
make manual
```

## CI/CD Integration

These tests can be integrated into CI/CD pipelines:

```yaml
# Example GitHub Actions
test:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v2
    - name: Run DRA test
      run: cd tests/dra && make build && make test
    - name: Run integration test
      run: cd tests/dra_integration && make build && make test
```

## Contributing

When adding new tests:

1. Create a new directory under `tests/`
2. Include Dockerfile, Makefile, README.md
3. Follow existing test structure
4. Document configuration and expected results
5. Add entry to this README

## Troubleshooting

### Connection timeouts

Increase wait times:

```bash
WAIT_FOR_CONNECTIONS=20 make test
```

### Container build failures

Ensure you're running from correct directory:

```bash
cd /path/to/godiam
docker build -f tests/<test_name>/Dockerfile -t <image_name> .
```

### Log analysis

All test logs are in `/tmp/*.log` within containers:

- `dra1.log`, `dra2.log` - DRA instances
- `peer*.log` - Peer instances
