# rt_ereg Test

Tests the `rt_ereg` extension which routes Diameter messages based on regex
pattern matching against AVP values.

## Topology

```
client ──► gw (rt_ereg: Destination-Realm="backend\.realm" → server1) ──► server1 (boosted)
                                                                           ──► server2
```

- **client**: Client that sends 5 test requests with dest_realm="backend.realm"
- **gw**: DRA relay with rt_ereg boosting server1 by score 200 when
  Destination-Realm matches "backend\.realm"
- **server1**: Backend peer boosted by regex match
- **server2**: Backend peer without boost

## Expected Behavior

All requests with Destination-Realm matching "backend\.realm" should be routed
to server1 due to the +200 score boost from rt_ereg.

## Running

```bash
make build && make test
```
