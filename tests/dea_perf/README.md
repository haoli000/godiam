# DEA Performance Test

Measures what the edge controls cost.

The harness runs the **same benchmark twice against the same backend**: once
through a plain relay agent and once through a Diameter Edge Agent with every
`dea_*` control enabled. Both agents share a backend, a routing table and an
application, and differ only in the edge model, so the delta between the two
runs is attributable to the edge controls rather than to the environment.

```
                    ┌─────────────────┐
  diameterbench ───▶│ baseline :3869  │──┐   plain relay, no edge model
                    └─────────────────┘  │
                                         ├──▶ backend server :3868 (app_cc)
                    ┌─────────────────┐  │
  diameterbench ───▶│   DEA :3870     │──┘   screening + rate limiting +
   (partner realm)  └─────────────────┘      topology hiding + edge routing
```

## Running

```sh
make build
make test                  # 10 connections, 10s, 1000 req/s
make quick                 # 5 connections, 5s, 500 req/s
make stress                # 50 connections, 30s, 10000 req/s
make clean

BENCH_RATE=5000 BENCH_CONNECTIONS=20 make test
```

Variables: `BENCH_CONNECTIONS`, `BENCH_DURATION`, `BENCH_RATE`, `BENCH_WARMUP`.

## What the edge agent actually does to the traffic

The benchmark clients connect on the **external** zone in the partner realm
`perf.partner.net`, so every request pays for:

| Control | Work per request |
| --- | --- |
| `dea_screening` | origin realm/host consistency, application, command, transit |
| `dea_ratelimit` | token bucket for the partner (ceiling far above the test rate, so what is measured is the check and never the throttling) |
| `dea_topohide` | stateful pseudonym allocation and masking of every answer |
| `dea_route` | external-to-internal direction enforcement |

The partner deliberately lists **no** peers. Clients are admitted by realm
match, which is the path a real interconnect uses and the one that binds a peer
to its partner at connection time.

## Validity checks

Throughput means nothing if the agent did not do the work — an agent that
rejects every request posts excellent numbers. Each run therefore asserts that
no request was rejected or throttled, that topology hiding ran, that edge
routing evaluated the traffic, and that every partner peer was bound to its
partner *while traffic was flowing*. A failed check fails the test and dumps the
agent log.

The most important of these is that the traffic **reached the internal peer**
rather than terminating at the edge. Both arms are measured at the backend, per
agent and across their own run: the backend's received counter is labelled by
peer, so it is sampled for that agent's identity immediately before and after
the measured run and the delta must cover every request the client sent. At
10000 req/s the edge agent relays 1:1 (298,577 of 298,577). The test also
asserts the internal peer is *up and tagged* `zone=core`, because the direction
rule and topology hiding both key on the zone, not on mere reachability.

A weaker version of this check — a single backend total, asserted non-zero —
would pass even if the edge agent answered every request locally, because the
baseline arm alone keeps the total above zero. Sampling per agent, per run, is
what makes the comparison a comparison.

## Results

Measured on darwin/arm64 (Apple M4 Pro), Docker, all nodes in one container, so
absolute latency includes loopback and container overhead. The comparison is
what matters.

### 1000 req/s, 10 connections, 10s

| | baseline | DEA | delta |
| --- | ---: | ---: | ---: |
| throughput (req/s) | 994.00 | 995.30 | +0.1% |
| avg latency (ms) | 0.580 | 0.593 | +2.2% |
| p50 latency (ms) | 0.429 | 0.444 | +3.5% |
| p95 latency (ms) | 1.144 | 1.254 | +9.6% |
| p99 latency (ms) | 4.035 | 2.273 | -43.7% |

### 10000 req/s, 50 connections, 30s

| | baseline | DEA | delta |
| --- | ---: | ---: | ---: |
| throughput (req/s) | 9873.23 | 9910.60 | +0.4% |
| avg latency (ms) | 0.821 | 0.935 | +13.9% |
| p50 latency (ms) | 0.612 | 0.722 | +18.0% |
| p95 latency (ms) | 1.778 | 2.018 | +13.5% |
| p99 latency (ms) | 5.107 | 5.211 | +2.0% |

Both runs are offered-rate limited rather than capacity limited, so throughput
tracks the target and the cost shows up in latency: the full edge control set
adds roughly **0.015 ms at the median at 1000 req/s and 0.11 ms at 10000
req/s**, and the tail stays within the baseline's own run-to-run variation.

At 1000 req/s the median delta is *inside* that variation and should not be read
as a measurement: a repeat run of the same profile put the DEA 2.1% **faster**
than the baseline at p50 and 4.4% faster at p95. Only the 10000 req/s comparison
separates the controls from the noise, and it reproduces — a repeat stress run
measured +15.2% p50 and +12.6% p95 against +18.0% and +13.5% above. Quote the
high-rate figures.

Screening and rate limiting are
allocation-free (see the `-bench` suites in `pkg/extensions/dea_*`); the
measurable cost is dominated by stateful topology hiding, which allocates a
pseudonym and rewrites AVPs on every message.

## What this test found

**Stateful hiding state is proportional to session rate, not to topology size.**
The 10000 req/s run allocated 297,778 pseudonyms in 30 seconds. This is inherent
— a pseudonym is held per `(internal host, Session-Id)` for the whole TTL — but
it makes `max_entries` a capacity decision, not a safety valve: the steady-state
requirement is about `new sessions per second x ttl`, which the default 100000
does not cover at any serious rate. `docs/dea-reference.md` now says so.

**The store was spending half that budget on Session-Id bookkeeping.** With
`max_entries: 200000` the run held only 99,987 pseudonyms and evicted 395,588
entries, because session mappings were charged to the same budget. Those
mappings existed only as a fallback for resolving an answer's partner when no
relay transaction exists — but an answer with no relay transaction is not being
forwarded to a partner at all, so hiding it was never correct. The mapping was
removed outright, which restores the documented meaning of `max_entries`, halves
store memory, and drops a write from every partner request. Re-running the same
stress test afterwards holds the full 200,000 pseudonyms and evicts 97,330
entries instead of 395,588.
