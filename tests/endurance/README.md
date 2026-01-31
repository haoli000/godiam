# Endurance Test

Production scenario simulation testing DRA resilience under sustained load with server failures, network partitions, and overload bursts.

## Topology

```
diameterbench (10 connections, 500 req/s)
        │
    DRA (port 3869)
    ├── rt_load_balance
    ├── rt_busypeers
    ├── rt_deny_by_size
    ├── fifo_stats
    ├── dbg_msg_timings
    └── prom_metrics
     │           │
server1 (3870)  server2 (3871)
  app_cc          app_cc
  prom_metrics    prom_metrics
```

## Test Phases

| Phase | Scenario | Duration | What's Tested |
|-------|----------|----------|---------------|
| 1 | Baseline steady state | 20s | Throughput, load distribution, zero errors |
| 2 | Server failure | 20s | Kill server1 → failover to server2 |
| 3 | Server recovery | 20s | Restart server1 → traffic rebalances |
| 4 | Network partition | 20s | iptables DROP → DRA reroutes |
| 5 | Network recovery | 20s | Remove DROP → server2 resumes |
| 6 | Overload burst | 15s | Spike to 3000 req/s → no crashes |
| 7 | Metrics validation | — | Prometheus consistency, no loops |

## Usage

```bash
# Build
make build

# Run endurance test (default: 20s phases, 500 req/s)
make test

# Quick test (10s phases, 200 req/s)
make quick

# Extended stress test (60s phases, 2000 req/s)
make stress

# Custom parameters
PHASE_DURATION=30 BENCH_RATE=1000 BENCH_CONN=20 BURST_RATE=5000 make test

# Interactive debugging
make manual
make shell
```

**Note:** Phases 4-5 (network partition) require `--cap-add NET_ADMIN` for iptables. The Makefile includes this flag automatically. Without it, those phases are skipped gracefully.

## Extensions Under Test

- **rt_load_balance** — Distributes traffic across server1/server2 by least-loaded peer
- **rt_busypeers** — Avoids routing to peers with >50 pending requests
- **fifo_stats** — Tracks per-peer queue depth and processing stats
- **dbg_msg_timings** — Records per-message latency histograms
- **prom_metrics** — Exposes Prometheus metrics on all nodes
- **rt_deny_by_size** — Enforces 64KB message size limit
- **app_cc** — Credit-Control handler on backend servers

## Validation Checks

- Process survival across all phases
- Throughput within acceptable bounds
- Traffic distribution across both servers
- Failover routing when a server dies
- Rebalancing after server recovery
- Rerouting during network partition
- Recovery after partition heals
- No routing loops or crashes under overload
- Prometheus metric consistency
