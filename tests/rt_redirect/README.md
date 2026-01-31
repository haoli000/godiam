# rt_redirect Test

Tests the `rt_redirect` extension which caches DIAMETER_REDIRECT_INDICATION
(result code 3006) answers and boosts redirect targets in future routing
decisions per RFC 6733 §6.12.

## Topology

```
client ──► gw (rt_redirect) ──► server
```

- **client**: Client that sends 3 test requests
- **gw**: DRA relay with rt_redirect extension (score_boost=100, TTL=1h)
- **server**: Backend peer that handles requests

## Expected Behavior

The test verifies that:
1. The rt_redirect extension initializes correctly
2. Normal message flow is unaffected (non-redirect answers pass through)
3. The InHandler and OutHandler are registered and active

Note: Full redirect caching is tested in unit tests where REDIRECT_INDICATION
responses can be simulated. This functional test validates integration.

## Running

```bash
make build && make test
```
