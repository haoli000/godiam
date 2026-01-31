# dbg_msg_timings + fifo_stats Test

Tests the `dbg_msg_timings` extension (request-to-answer latency tracking) and
`fifo_stats` extension (per-peer queue depth and time-in-queue statistics).

## Topology

```
client ──► gw (dbg_msg_timings + fifo_stats) ──► server
```

- **client**: Client that sends 5 test requests
- **gw**: DRA relay with dbg_msg_timings (threshold=1ns to log all) and fifo_stats
- **server**: Backend peer that handles requests

## Expected Behavior

With log_threshold set to 1ns, all request-answer pairs should trigger timing
log entries showing latency measurements. The fifo_stats extension tracks
per-peer queue depth.

## Running

```bash
make build && make test
```
