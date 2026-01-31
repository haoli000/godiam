# rt_load_balance Test

Tests the `rt_load_balance` extension which distributes requests across multiple
backend peers by preferring the least-loaded peer.

## Topology

```
client ──► gw (rt_load_balance + rt_randomize) ──► server1
                                                   ──► server2
```

- **client**: Client that sends 10 test requests
- **gw**: DRA relay with rt_load_balance and rt_randomize extensions
- **server1/2**: Two backend peers that handle requests

## Expected Behavior

With rt_load_balance enabled, the gateway should distribute requests across both
backend peers based on their current load (pending request count). Both backends
should receive at least some requests.

## Running

```bash
make build && make test
```
