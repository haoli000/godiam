# godiam DRA Performance Test

Measures throughput and latency of godiam DRA message routing.

## Overview

This test measures how many Diameter messages per second a godiam DRA can relay.

```plantext
diameterbench (client) -> DRA (relay) -> Server (app_cc)
                       <-            <-
```

- **diameterbench**: Generates CCR (Credit-Control-Request) messages at a target rate
- **DRA**: Relays messages between client and server
- **Server**: Handles CCRs and returns CCAs using `app_cc` extension

## Quick Start

```bash
cd tests/dra_perf
make build
make test
```

## Benchmark Parameters

| Parameter         | Default | Description                      |
| ----------------- | ------- | -------------------------------- |
| BENCH_CONNECTIONS | 10      | Number of concurrent connections |
| BENCH_DURATION    | 10s     | Test duration                    |
| BENCH_RATE        | 1000    | Target requests per second       |

### Custom Parameters

```bash
# Higher throughput test
BENCH_RATE=5000 BENCH_CONNECTIONS=20 make test

# Quick validation
make quick

# Stress test
make stress
```

## Output Metrics

The test reports:

- **Total Requests Sent**: Number of CCRs sent
- **Total Answers Recv**: Number of CCAs received
- **Throughput**: Achieved requests per second
- **Latency**: Avg, Min, Max, P50, P95, P99

Example output:

```console
--- Benchmark Results ---
Total Requests Sent: 10000
Total Answers Recv: 10000
Throughput: 1000.00 req/sec
Avg Latency: 2.5ms
Min Latency: 1.2ms
Max Latency: 15.3ms
P50 Latency: 2.3ms
P95 Latency: 4.8ms
P99 Latency: 8.1ms
```

After the benchmark, Prometheus metrics are printed for both the DRA and backend server:

```
--- DRA (dra.test.realm) ---
diameter_routing_requests_total 2495
diameter_routing_relayed_total 2495
diameter_routing_answers_relayed_total 2495
diameter_routing_dispatched_total 0
diameter_routing_errors_total 0
diameter_peer_up{peer="server.backend.realm",realm="backend.realm"} 1
diameter_peer_messages_sent_total{peer="server.backend.realm",realm="backend.realm"} 2496
diameter_peer_messages_received_total{peer="server.backend.realm",realm="backend.realm"} 2496
...

--- Backend Server (server.backend.realm) ---
diameter_routing_requests_total 2495
diameter_routing_dispatched_total 2495
diameter_routing_relayed_total 0
diameter_peer_up{peer="dra.test.realm",realm="test.realm"} 1
...
```

Key things to verify from the metrics:
- **DRA**: `relayed_total` and `answers_relayed_total` should match `requests_total`; `dispatched_total` should be 0
- **Server**: `dispatched_total` should match `requests_total`; `relayed_total` should be 0
- **Both**: `errors_total` and `loops_detected_total` should be 0

## Architecture

### Configuration Files

| File               | Role                               |
| ------------------ | ---------------------------------- |
| `conf/server.yaml` | Backend server with app_cc handler |
| `conf/dra.yaml`    | DRA relay configuration            |

### Message Flow

1. `diameterbench` opens connections to DRA (port 3869)
2. Sends CCR messages with `Destination-Realm: backend.realm`
3. DRA relays to backend server (port 3868)
4. Server responds with CCA
5. DRA relays CCA back to diameterbench

## Tuning

To achieve higher throughput:

- Increase `BENCH_CONNECTIONS` for more parallel requests
- Ensure the container has sufficient CPU resources
- Consider running on bare metal for accurate measurements

## Troubleshooting

If throughput is lower than expected:

1. Check if all answers are received (Total Requests == Total Answers)
2. Increase test duration for more stable measurements
3. Run `docker logs` to check for errors
