# godiam + freeDiameter Integration Test

Integration test that combines freeDiameter and godiam implementations to verify interoperability between the two Diameter implementations.

## Overview

This test verifies that godiam and freeDiameter can interoperate as Diameter Routing Agents (DRAs) and peers. The setup uses:

- **Left Side (freeDiameter)**: DRA1, Peer11, Peer12
- **Right Side (godiam)**: DRA2, Peer2

## Architecture

```text
┌─────────────────────────────────────────────────────────────────┐
│                     Integration Test Setup                      │
├─────────────────────────────────┬───────────────────────────────┤
│         Left Side               │         Right Side            │
│      (freeDiameter)             │         (godiam)              │
│                                 │                               │
│  ┌──────────────┐               │                               │
│  │   peer11     │               │                               │
│  │  (answers)   │               │                               │
│  └──────┬───────┘               │                               │
│         │                       │                               │
│         v                       │                               │
│  ┌──────────────┐               │      ┌──────────────┐         │
│  │     dra1     │◄──────────────┼─────►│     dra2     │         │
│  │   (routes)   │               │      │  (rt_rewrite)│         │
│  └──────────────┘               │      └──────┬───────┘         │
│         ^                       │             │                 │
│         │                       │             v                 │
│  ┌──────┴───────┐               │      ┌──────────────┐         │
│  │   peer11/12  │               │      │   peer2      │         │
│  │  (answers)   │               │      │  (sender)    │         │
│  └──────────────┘               │      └──────────────┘         │
│                                 │                               │
│  Realm: access.realm               │  Realm: backend.realm            │
└─────────────────────────────────┴───────────────────────────────┘
```

## Test Flow

1. **godiam peer2** sends a test message with:
   - `Destination-Host: dra1.access.realm`
   - `Destination-Realm: access.realm`
   - Routes to **godiam dra2**

2. **godiam dra2** receives the message:
   - Uses `rt_rewrite` extension to **drop Destination-Host AVP**
   - Routes to **freeDiameter dra1** based on destination realm

3. **freeDiameter dra1** receives the message:
   - No Destination-Host specified (was dropped)
   - Routes to either **peer11** or **peer12** randomly

4. **freeDiameter peer11/peer12** receives and answers:
   - Sends answer back through the DRA chain
   - Answer arrives at **godiam peer2**

## Key Features Tested

### Interoperability

- ✅ godiam DRA ↔ freeDiameter DRA communication
- ✅ godiam peer ↔ godiam DRA communication
- ✅ freeDiameter DRA ↔ freeDiameter peer communication

### Routing Logic

- ✅ Cross-implementation message routing
- ✅ Realm-based routing between implementations
- ✅ AVP rewriting (rt_rewrite on godiam side)

### Protocol Compliance

- ✅ CER/CEA capability exchange between implementations
- ✅ DWR/DWA watchdog mechanism
- ✅ Message encoding/decoding compatibility

## Configuration Files

| Instance | Implementation | Config File        | Role                             |
| -------- | -------------- | ------------------ | -------------------------------- |
| dra1     | freeDiameter   | `conf/dra1.conf`   | Left side DRA                    |
| peer11   | freeDiameter   | `conf/peer11.conf` | Left side peer (server)          |
| peer12   | freeDiameter   | `conf/peer12.conf` | Left side peer (server)          |
| dra2     | godiam         | `conf/dra2.yaml`   | Right side DRA (drops Dest-Host) |
| peer2    | godiam         | `conf/peer2.yaml`  | Right side peer (client)         |

### IP Address Mapping

| Instance | Listen Address | Port  |
| -------- | -------------- | ----- |
| dra1     | 127.0.0.10     | 3868  |
| peer11   | 127.0.0.11     | 13868 |
| peer12   | 127.0.0.12     | 23868 |
| dra2     | 127.0.0.20     | 3869  |
| peer2    | 127.0.0.22     | 3872  |

## Building and Running

### Prerequisites

- Docker
- Access to `haoli1/freediameter:latest` base image (or build freeDiameter locally)

### Build from godiam root directory

```bash
cd /path/to/godiam
docker build -f tests/dra_integration/Dockerfile -t godiam-freediam-integration .
```

### Run Automated Test

```bash
docker run --rm godiam-freediam-integration
```

### Run in Manual Mode

```bash
docker run -it --rm godiam-freediam-integration --manual
```

In manual mode, all services start and logs are streamed. The container stays running for manual inspection.

## Expected Output

```console
==============================================
  freeDiameter + godiam Integration Test
  Left: freeDiameter | Right: godiam
==============================================

[1/5] Starting freeDiameter dra1 (left side)...
[2/5] Starting freeDiameter peer11 (left side)...
[3/5] Starting freeDiameter peer12 (left side)...
[4/5] Starting godiam dra2 (right side)...
[5/5] Starting godiam peer2 (right side)...

All services started:
  freeDiameter dra1:   PID 123
  freeDiameter peer11: PID 124
  freeDiameter peer12: PID 125
  godiam dra2:         PID 126
  godiam peer2:        PID 127

Running automated test...

=====================================
  Integration Test Verification
=====================================

[1/4] Verifying all processes are alive...
✓ All processes are running

[2/4] Checking peer connections...
✓ freeDiameter dra1 connected to peer11, peer12, and dra2
✓ godiam dra2 connected to peer2 and dra1

[3/4] Waiting for test message propagation...

[4/4] Verifying message routing...
✓ godiam peer2 sent test message
✓ godiam dra2 received message
✓ godiam dra2 applied rt_rewrite (dropped Destination-Host)
✓ freeDiameter dra1 routed message to peer11/peer12
✓ freeDiameter peer11 answered test message

=====================================
  Test Summary
=====================================

Test Results: 5/5 checks passed

✓ Integration test PASSED!
✓ freeDiameter and godiam interoperate successfully
```

## Debugging

### View logs

```bash
docker run -it --rm godiam-freediam-integration --manual

# In another terminal
docker ps  # Get container ID
docker exec -it <container_id> bash
tail -f /tmp/dra1.log
tail -f /tmp/dra2.log
tail -f /tmp/peer2.log
```

### Common Issues

1. **Connections not establishing**: Check firewall settings and ensure localhost aliases work
2. **Messages not routing**: Verify rt_rewrite is loaded on dra2 and dropping Destination-Host
3. **Test timeouts**: Increase `WAIT_FOR_CONNECTIONS` and `TIMEOUT` environment variables

## Differences from Pure Tests

| Aspect           | diameter-dra-test | godiam/tests/dra | This Integration Test |
| ---------------- | ----------------- | ---------------- | --------------------- |
| Left Side DRA    | freeDiameter      | godiam           | freeDiameter          |
| Right Side DRA   | freeDiameter      | godiam           | godiam                |
| Left Side Peers  | freeDiameter      | godiam           | freeDiameter          |
| Right Side Peers | freeDiameter      | godiam           | godiam                |
| **Validates**    | freeDiameter only | godiam only      | **Interoperability**  |

## Maintenance

This test should be run whenever:

- Changes are made to godiam's core diameter protocol implementation
- Changes are made to message encoding/decoding
- New extensions are added that affect message routing
- Protocol compliance updates are implemented

## License

Same as godiam project.
