# DEA Soak Test

Long-running resource test for the Diameter Edge Agent. Where `dea_perf`
answers "what do the edge controls cost per message", this answers "what does
the agent hold on to, and does it give it back".

```
                    ┌─────────────────┐
  diameterbench ───▶│   DEA :3870     │───▶ backend server :3868 (app_cc)
   (partner realm)  └─────────────────┘
                            │
                    sampled every 2s:
                    CPU, RSS, heap, goroutines,
                    pseudonym store, limiter buckets
```

## Running

```sh
make build
make test     # 120s phases, ~8 min
make quick    # short run, for validating the harness itself
make long     # 600s phases, ~19 min
make clean
```

Variables: `PHASE_DURATION`, `BENCH_RATE`, `BENCH_CONN`, `BURST_RATE`,
`CHURN_CYCLES`, `SAMPLE_INTERVAL`, `DRAIN_SECONDS`.

## Phases

Steady load alone will not distinguish memory that is *in use* from memory that
is *leaked*, so the run is staged:

| Phase | What it is | What it catches |
| --- | --- | --- |
| `baseline` | idle after startup | what an empty process costs, so later growth has a reference |
| `steady` | sustained load | unbounded growth, visible as a slope that never plateaus |
| `churn` | repeated connect/disconnect | per-peer leaks: goroutines, registry bindings, limiter buckets |
| `burst` | a rate spike | retention of work the agent shed |
| `drain` | long idle | anything merely in use is released here; anything leaked is not |

The drain deliberately outlasts **two** clocks: the pseudonym TTL, so the
sweeper has to run, and Go's two-minute forced GC period. The second matters as
much as the first — an idle Go process allocates nothing, so nothing triggers a
GC, and `heap_inuse` would sit at its peak for reasons that have nothing to do
with a leak. An earlier 90-second drain reported exactly that false positive.

## What is asserted

Resource checks:

- the pseudonym store never exceeds `max_entries` — the cap is what stops a
  partner's session rate from sizing the agent's heap;
- RSS does not grow **while idle** (growth while the store fills toward its cap
  is expected work, and is reported rather than failed);
- the TTL sweeper reclaims, and the store actually drains;
- goroutines return to baseline after churn;
- at least half the load-induced heap growth is given back.

Plus the same guard `dea_perf` uses, because a soak that quietly stopped doing
the work would look excellent in every resource graph: the controls must have
run, rejected nothing, throttled nothing, and — measured at the backend — every
request must have been **relayed to the internal peer** rather than answered at
the edge.

## What this test found

**A peer that lost a watchdog answer was stranded forever.** On a watchdog
timeout the peer moves to `Suspect` and sends a DWR, but nothing rearmed the
timer, so the `EventTimeout` case that retires the connection and schedules a
reconnect was unreachable. A peer in `Suspect` reports `IsOpen() == false`, so
it silently left routing: the agent kept answering `UNABLE_TO_DELIVER` locally
while its upstream sat there, connected at the TCP level, never recovering.

It is a core defect, not a DEA one — a plain relay with no zones and no
`dea_*` extensions reproduces it — and it needed exactly this test's shape to
surface. Every other suite either keeps traffic flowing continuously, which
resets the watchdog, or finishes before the first timeout. Only an idle period
followed by load exposes it, and only a delivery check distinguishes it from
healthy traffic: the clients still counted an answer for every request, because
a locally generated error answer is still an answer.

The fix rearms the watchdog on entry to `Suspect`, so a lost DWA now closes the
connection and reconnects. Regression tests are in
`pkg/core/peer/watchdog_suspect_test.go`, and the soak asserts the agent logs no
routing errors.

## Results

Measured on darwin/arm64 (Apple M4 Pro), Docker, all nodes in one container.
`make long` — 600s phases, 2000 req/s over 20 connections, 8000 req/s burst,
20 churn cycles, `max_entries: 20000`, `ttl: 30s`. 2,996,370 requests sent, all
screened, all relayed to the internal peer, none rejected or throttled:

| phase | cpu% | RSS MB | heap MB | goroutines | pseudonyms |
| --- | ---: | ---: | ---: | ---: | ---: |
| baseline | 0.1 | 15.5 | 4.6 | 17 | 0 |
| steady | 11.2 | 71.3 | 57.9 | 98 | 20000 |
| churn | 9.0 | 70.8 | 52.8 | 97 | 20000 |
| burst | 31.7 | 150.9 | 132.4 | 258 | 20000 |
| drain | 0.1 | 150.6 | 105.6 | 18 | 20000 → 0 |

CPU is proportional to offered load and returns to idle. Ten minutes of
sustained traffic does not move RSS (slope −0.04 MB/min) because the store is at
its cap and everything else is recycled; the only growth is the burst, which is
concurrency, and it is given back. 90% of the load-induced heap is reclaimed.
Goroutines return to 16 after 20 churn cycles, so nothing is retained per
connection. RSS itself stays high after the burst — that is the Go allocator
holding freed spans, not the agent holding data, which is why the check is on
the idle *slope* rather than the absolute figure.

`make quick` (30s phases, 500 req/s) for comparison:

| phase | cpu% | RSS MB | heap MB | goroutines | pseudonyms |
| --- | ---: | ---: | ---: | ---: | ---: |
| baseline | 0.1 | 15.4 | 4.5 | 17 | 0 |
| steady | 6.7 | 32.9 | 18.7 | 37 | 13891 |
| churn | 5.2 | 33.7 | 20.2 | 38 | 14846 |
| burst | 18.5 | 49.2 | 37.4 | 77 | 20000 |
| drain | 0.1 | 54.0 | 41.3 | 18 | 20000 → 0 |

