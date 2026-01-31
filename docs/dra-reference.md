# DRA (Diameter Routing Agent) Reference

This document describes how freeDiameter configures DRA mode and how the
equivalent works in godiam. It serves as a design reference for the routing
subsystem.

## Overview

A DRA acts as a message router between Diameter realms/peers without processing
application-specific logic.

## freeDiameter DRA Configuration

### 1. Enable Relay Application (Default: Enabled)

By default, freeDiameter acts as a relay. The configuration option `NoRelay;`
**disables** this:

```conf
# In freediameter.conf
# To enable DRA mode (default behavior):
# (do NOT include NoRelay - relay is enabled by default)
# To disable relay (act as endpoint only):
# NoRelay;
```

**What happens internally:**

- When relay is enabled, freeDiameter advertises application ID `0xffffffff`
  (Relay) in CER/CEA
- Messages not handled by local extensions are forwarded to appropriate peers
- Route-Record AVPs are added to track message path

### 2. Define Peer Connections

```conf
ConnectPeer = "hss.realm-a.com" { No_TLS; };
ConnectPeer = "mme.realm-b.com" { No_TLS; port = 3868; };
ConnectPeer = "dra2.realm-c.com" { ConnectTo = "10.0.0.1"; No_TLS; };
```

### 3. Routing Extensions

Essential extensions for DRA functionality:

```conf
LoadExtension = "dbg_msg_dumps.fdx" : "0x0080";
LoadExtension = "rt_default.fdx" : "/etc/freeDiameter/rt_default.conf";
LoadExtension = "rt_rewrite.fdx" : "/etc/freeDiameter/rt_rewrite.conf";
LoadExtension = "rt_busypeers.fdx" : "/etc/freeDiameter/rt_busypeers.conf";
```

### 4. Routing Rules (rt_default.conf)

```conf
dr="realm-a.com" : "hss.realm-b.com" += REALM ;
un=["^123.*"] : "hss1.example.com" += FINALDEST ;
un=["^456.*"] : "hss2.example.com" += FINALDEST ;
* : "upstream-dra.example.com" += DEFAULT ;
```

**Score Constants:**

| Constant      | Value | Meaning                        |
| ------------- | ----- | ------------------------------ |
| NO_DELIVERY   | -70   | Never route to this peer       |
| DEFAULT       | 5     | Basic routing preference       |
| DEFAULT_REALM | 10    | Peer matches Destination-Realm |
| REALM         | 15    | Strong realm match             |
| FINALDEST     | 100   | This is the final destination  |

### 5. Remove Destination-Host for Load Balancing

```conf
DROP = "Destination-Host";
```

When Destination-Host is set, freeDiameter gives that peer a score of
`FD_SCORE_FINALDEST = 100`. Removing it allows the DRA to select any peer that
matches Destination-Realm, enabling true load balancing.

## Routing Decision Flow

1. **Check Destination-Host**: If set and peer is connected → route to that peer
2. **Check Route-Record**: Skip peers that already forwarded the message
3. **Check Application Support**: Peer must support the Application-ID or Relay
4. **Check Destination-Realm**: Prefer peers in the target realm
5. **Apply routing extensions**: rt_ereg, rt_load_balance, rt_busypeers, etc.
6. **Select highest-scored peer**: Send to peer with best score

## Example: Two-DRA Setup

```text
peer11 ─┐                          ┌─ peer2
        ├─ dra1 ←───────────→ dra2 ┤
peer12 ─┘    (access.realm)     (backend.realm)
```

See `tests/dra/` and `tests/dra_integration/` for working examples.

## godiam DRA Features

godiam implements DRA mode with the following:

1. **Relay Application Advertisement**: App-ID `0xffffffff` in CER/CEA
2. **Message Forwarding**: Forward unhandled requests to appropriate peers
3. **Route-Record Handling**: Add Route-Record AVP, check for loops
4. **Priority-ordered routing callbacks**: InHandler / OutHandler chains
5. **Extensions**: rt_rewrite, rt_ereg, rt_load_balance, rt_busypeers, etc.

## Future Work

All planned extensions have been implemented.

## References

- freeDiameter source: <https://github.com/freeDiameter/freeDiameter>
- RFC 6733: Diameter Base Protocol
- RFC 3588: Diameter Base Protocol (obsolete, but referenced)
