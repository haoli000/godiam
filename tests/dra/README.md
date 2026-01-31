# godiam DRA Test

Test godiam as a Diameter Routing Agent (DRA) with AVP rewriting capabilities.

## Overview

This setup tests DRA functionality using godiam, ported from the original freeDiameter DRA test. The test involves:

- Two interconnected DRA instances (`dra1` and `dra2`)
- Two client peers on each side (`peer11`/`peer12` on left, `peer2` on right)
- `rt_rewrite` extension to drop `Destination-Host` AVP on `dra2`
- Message routing across realms to verify proper DRA behavior

## Run

Using the Makefile:

```bash
cd tests/dra
make build
make test
```

For manual debugging with streaming logs:

```bash
make manual
```

For shell access:

```bash
make shell
```

## Test Validation

The automated test verifies:

1. All 5 processes are running (dra1, peer11, peer12, dra2, peer2)
2. peer2 sent test message
3. dra2 received and forwarded message
4. dra1 routed message to peer11/peer12
5. peer2 received answer from peer11/peer12

## Architecture

```t
 Left Side                        Right Side
┌─────────────┐                ┌─────────────┐
│  peer11/12  │                │   peer2     │
│  (client)   │                │  (client)   │
└─────────────┘                └─────────────┘
       ^                              ^
       │                              │
       v                              v
┌─────────────┐                ┌─────────────┐
│    dra1     │                │    dra2     │
│  (routes    │◄──────────────►│  (drops     │
│   randomly) │                │  Dest-Host) │
└─────────────┘                └─────────────┘
```

## Test Scenario

1. `peer2` sends a Credit-Control-Request (CCR, Code=272, App=4) with `Destination-Host=dra1.access.realm` and `Destination-Realm=access.realm`
2. `dra2` receives the CCR and uses `rt_rewrite` to drop `Destination-Host`
3. `dra2` relays the CCR to `dra1` based on `default_realm` and `realm_routes`
4. `dra1` processes the CCR (App 4) and generates a Credit-Control-Answer (CCA)
5. The CCA is relayed back through `dra2` to `peer2`, completing the flow

## Configuration Files

| Instance | Config File        | Role                                   |
| -------- | ------------------ | -------------------------------------- |
| dra1     | `conf/dra1.yaml`   | Left side DRA, routes to right side    |
| dra2     | `conf/dra2.yaml`   | Right side DRA, drops Destination-Host |
| peer11   | `conf/peer11.yaml` | Left side client                       |
| peer12   | `conf/peer12.yaml` | Left side client                       |
| peer2    | `conf/peer2.yaml`  | Right side client                      |

## Key Features Tested

### 1. AVP Rewriting (rt_rewrite)

`dra2.yaml` includes:

```yaml
extensions:
  - name: "rt_rewrite"
    config:
      rules:
        - drop: "Destination-Host"
```

**Expected behavior**: `dra2` removes `Destination-Host` AVP from all messages.

### 2. Inter-Realm Routing

- `dra1` knows peers in `access.realm` realm
- `dra2` has default routing to `access.realm` realm
- Messages flow between realms correctly

### 3. Message Routing

- `dra1` randomly routes CCR to `peer11` or `peer12` (within access.realm)
- `peer2` routes all messages to `dra2`

## Building and Running

### Build Docker Image

```bash
cd dra-test
docker build -t godiam-dra-test .
```

### Run Test

```bash
docker run -it --rm godiam-dra-test
```

### Monitor Logs

```bash
# In another terminal, after starting the container
docker exec -it <container_id> bash
tail -f /tmp/dra1.log &
tail -f /tmp/dra2.log &
tail -f /tmp/peer11.log &
tail -f /tmp/peer12.log &
tail -f /tmp/peer2.log &
```

### Trigger Test Message

```bash
# Preferred: use the helper script (peer2 auto-sends via test_app)
cd /root
./test_client.sh
```

## Current Setup

- Nodes: `dra1.access.realm` (port 3868), `dra2.backend.realm` (port 3869), `peer11.access.realm` (port 3870), `peer12.access.realm` (port 3871), `peer2.backend.realm` (port 3872)
- Applications: Credit-Control (AppId 4) and Base Accounting (AppId 3) enabled on DRA nodes; Credit-Control enabled on `peer2`
- Relaying: Enabled on both DRAs with explicit `realm_routes`; `dra2` uses `default_realm: access.realm` for CCR without `Destination-Host`
- Rewriting: `dra2` loads `rt_rewrite` with rule to drop `Destination-Host`

## Ports & Addresses

- dra1.access.realm
  - listen: 127.0.0.10:3868
  - peers: 127.0.0.11:3870 (`peer11`), 127.0.0.12:3871 (`peer12`)
- dra2.backend.realm
  - listen: 127.0.0.21:3869
  - peers: 127.0.0.22:3872 (`peer2`), 127.0.0.10:3868 (`dra1`)
- peer2.backend.realm
  - listen: 127.0.0.22:3872

## Comparison with freeDiameter Test

| Aspect            | freeDiameter                 | godiam                |
| ----------------- | ---------------------------- | --------------------- |
| Config format     | .conf (freeDiameter syntax)  | .yaml                 |
| rt_rewrite syntax | `DROP = "Destination-Host";` | YAML rules            |
| Test extension    | test_app (custom)            | test_app (Go)         |
| Logging           | freeDiameter debug flags     | structured timestamps |
| Signal handling   | native signals               | auto-send on startup  |

## Troubleshooting

### Connection Issues

- Check that all instances are running
- Verify port mappings in Docker
- Check firewall settings

### rt_rewrite Not Working

- Verify `rt_rewrite` extension is loaded
- Check configuration syntax
- Look for "rt_rewrite:" log messages

### No Response

- Check network connectivity between containers
- Verify realm configurations match
- Monitor all instance logs

## Key Differences from freeDiameter Test

1. **YAML Configuration**: godiam uses YAML instead of freeDiameter's .conf format
2. **go log package**: Standard Go logging instead of freeDiameter's fd_log
3. **No test_app extension**: Uses separate test client instead of extension
4. **Built-in test signal**: Original uses SIGUSR1 to trigger, godiam test can run standalone

## Files

- `Dockerfile` - Multi-stage build for godiam
- `entrypoint.sh` - Starts all instances
- `conf/*.yaml` - Instance configurations
- `test_client.sh` - Helper script for test client

## Actual Test Logs

```t
[dra1] 2026/01/31 13:11:30 DEBUG: Incoming Diameter message: Code=272, App=9999, Request=true, DestHost=, DestRealm=access.realm, OriginHost=peer2.backend.realm, OriginRealm=backend.realm

[dra1] 2026/01/31 13:11:30 DEBUG: Incoming Diameter message: Code=272, App=9999, Request=false, DestHost=, DestRealm=, OriginHost=peer12.access.realm, OriginRealm=access.realm

[peer12] 2026/01/31 13:11:30 DEBUG: Incoming Diameter message: Code=272, App=9999, Request=true, DestHost=, DestRealm=access.realm, OriginHost=peer2.backend.realm, OriginRealm=backend.realm

[peer12] 2026-01-31 13:11:30.983 | test_app request: code=272 app=9999 hbh=2 e2e=1347846109 from=peer2.backend.realm/backend.realm to=/access.realm avps=7 rr=[dra2.backend.realm,dra1.access.realm]

[peer12] 2026-01-31 13:11:30.983 | test_app answer sent: code=272 app=9999 hbh=2 e2e=1347846109 to=dra1.access.realm avps=5

[dra2] 2026/01/31 13:11:30 DEBUG: Incoming Diameter message: Code=272, App=9999, Request=true, DestHost=, DestRealm=access.realm, OriginHost=peer2.backend.realm, OriginRealm=backend.realm

[dra2] 2026/01/31 13:11:30 DEBUG: Incoming Diameter message: Code=272, App=9999, Request=false, DestHost=, DestRealm=, OriginHost=peer12.access.realm, OriginRealm=access.realm

[peer2] 2026-01-31 13:11:30.981 | test_app send request: code=272 app=9999 hbh=2 e2e=1347846109 from=peer2.backend.realm/backend.realm to=/access.realm next=dra2.backend.realm avps=5

[peer2] 2026/01/31 13:11:30 DEBUG: Incoming Diameter message: Code=272, App=9999, Request=false, DestHost=, DestRealm=, OriginHost=peer12.access.realm, OriginRealm=access.realm

[peer2] 2026-01-31 13:11:30.983 | test_app answer: code=272 app=9999 hbh=2 e2e=1347846109 from=peer12.access.realm/access.realm avps=5 rr=[]

[peer2] 2026-01-31 13:11:30.983 | test_app result: 2001
```

Note: `Destination-Host` may be absent; relaying uses `Destination-Realm` plus configured routes. Route-Record chain helps trace the path across DRA nodes.

## License

Same as godiam project.
