# rt_deny_by_size Test

Tests the `rt_deny_by_size` extension which rejects Diameter messages exceeding
a configured maximum size.

## Topology

```
client ──► gw (rt_deny_by_size max=100) ──► server
```

- **client**: Client that sends 3 test requests
- **gw**: DRA relay with rt_deny_by_size configured to reject messages >100 bytes
- **server**: Backend peer (should not receive rejected messages)

## Expected Behavior

Standard Diameter messages with mandatory AVPs exceed 100 bytes, so the gateway
should reject them with a log entry indicating the size violation.

## Running

```bash
make build && make test
```
