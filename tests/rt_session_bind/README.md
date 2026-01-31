# rt_session_bind Integration Test

Tests session-affinity routing via the `rt_session_bind` extension.

## Architecture

```
client (client) --> gw (DRA + rt_session_bind) --> server1 (backend1)
                                                  --> server2 (backend2)
```

The client sends 5 requests with the same Session-Id. The DRA routes them using `rt_session_bind` which should bind the session to one backend. All 5 requests should land on the same backend.

## Quick Start

```bash
make build && make test
```

## Configuration

| Instance    | Config File             | Role                                                      |
| ----------- | ----------------------- | --------------------------------------------------------- |
| gw          | `conf/gw.yaml`          | DRA gateway with rt_session_bind (binding_key=session_id) |
| client   | `conf/client.yaml`   | Client sending 5 test requests                            |
| server1 | `conf/server1.yaml` | Backend server 1                                          |
| server2 | `conf/server2.yaml` | Backend server 2                                          |

## IP/Port Mapping

| Instance    | Address    | Port |
| ----------- | ---------- | ---- |
| gw          | 127.0.0.20 | 3869 |
| client   | 127.0.0.11 | 3870 |
| server1 | 127.0.0.21 | 3871 |
| server2 | 127.0.0.22 | 3872 |

## Validation

1. All 4 processes are running
2. Client sent test messages
3. rt_session_bind extension initialized on DRA
4. At least one backend received messages
5. All requests routed to a single backend (session binding consistency)
