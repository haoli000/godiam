# Diameter Edge Agent (DEA) Reference

godiam can run as a **Diameter Edge Agent**: the node an operator places at the
border of its Diameter network, facing roaming partners and IPX carriers. A DEA
is a DRA plus a trust boundary — it decides which peers may talk to the network,
what they are allowed to send, how fast they may send it, and how much of the
internal topology they are allowed to see.

This document describes the edge model, the four `dea_*` extensions that
implement it, and how to operate them. See [dra-reference.md](dra-reference.md)
for the underlying routing agent behaviour.

## Table of contents

- [Concepts](#concepts)
- [Quick start](#quick-start)
- [Configuration reference](#configuration-reference)
  - [Zones](#zones)
  - [Partners](#partners)
  - [Screening policy](#screening-policy)
  - [Topology hiding policy](#topology-hiding-policy)
  - [Rate limit policy](#rate-limit-policy)
- [Extensions](#extensions)
  - [dea_screening](#dea_screening)
  - [dea_ratelimit](#dea_ratelimit)
  - [dea_topohide](#dea_topohide)
  - [dea_route](#dea_route)
- [Transport security](#transport-security)
- [Observability](#observability)
- [Operational guidance](#operational-guidance)
- [Compatibility notes](#compatibility-notes)
- [Differences from freeDiameter](#differences-from-freediameter)

## Concepts

**Zone** — a network-facing side of the agent. Each zone owns its listeners, its
TLS material and its peer admission policy. A zone is `internal` (the operator's
own network) or `external` (untrusted partners). A node without a `zones:` block
behaves exactly as before: godiam synthesises a single internal zone named
`default` from the node-level `listen`, `tls` and `peers_policy` settings.

**Partner** — a roaming partner or IPX carrier reachable through an external
zone. A partner owns realms, peers, optionally PLMN identifiers, and carries the
three policies that make the edge safe: screening, topology hiding and rate
limiting.

**Served realm** — a realm this agent terminates rather than relays onward: the
local realm, realms declared on an internal zone, realms of peers in an internal
zone, and realm routes whose next hops are all internal. Traffic from a partner
toward a served realm is normal termination; traffic toward any other realm is
transit and is refused unless the partner has `allow_transit: true`.

**Internal host** — this node's identity, any peer in an internal zone, or any
host inside a served realm. Internal hosts are exactly what topology hiding
masks.

Everything above lives in `pkg/core/edge.Registry`, a read-mostly index rebuilt
on configuration reload. The extensions only read it, so the router itself stays
generic.

## Quick start

```yaml
identity: "dea.edge.example.com"
realm: "edge.example.com"

zones:
  - name: core
    role: internal
    realms: ["core.example.com"]
    listen:
      addresses: ["10.0.0.1"]
      port: 3868
      enable_tcp: true
  - name: roaming
    role: external
    listen:
      addresses: ["198.51.100.1"]
      secure_port: 5868
      enable_tls: true
    tls:
      enabled: true
      cert_file: /etc/diameter/edge-cert.pem
      key_file: /etc/diameter/edge-key.pem
      ca_file: /etc/diameter/partner-ca.pem
      verify_peer: true
      require_client_cert_identity: true

partners:
  - name: partner-a
    zone: roaming
    realms: ["partnera.com", "*.partnera.com"]
    plmn_ids: ["24001"]
    peers:
      - identity: "dea1.partnera.com"
        priority: 0
        weight: 10
      - identity: "dea2.partnera.com"
        priority: 1
    applications: [16777251]
    commands: [316, 318]
    topology_hiding:
      mode: stateful
      ttl: 30m
    rate_limit:
      enabled: true
      requests_per_second: 500
      burst: 750

extensions:
  - name: dea_screening
  - name: dea_ratelimit
  - name: dea_topohide
  - name: dea_route
```

A runnable version lives in [`examples/configs/dea.yaml`](../examples/configs/dea.yaml),
and a full test bed with a hostile partner in [`tests/dea_integration/`](../tests/dea_integration/).

## Configuration reference

### Zones

```yaml
zones:
  - name: roaming              # required, unique
    role: external             # internal (default) | external
    realms: ["core.example.com"]   # realms served behind an internal zone
    listen:
      addresses: ["198.51.100.1"]
      port: 3868               # default 3868
      secure_port: 5868        # default 5868
      enable_tcp: true         # listen on port; speaks TLS when tls.enabled
      enable_tls: true         # listen on secure_port, always TLS
      enable_sctp: false
    tls:
      enabled: true
      cert_file: /etc/diameter/edge-cert.pem
      key_file: /etc/diameter/edge-key.pem
      ca_file: /etc/diameter/partner-ca.pem   # required when verify_peer is true
      verify_peer: true
      min_version: "1.2"       # default "1.2"
      require_client_cert_identity: false
    peers_policy:
      allow_unknown: false     # rejected outright on an external zone
      allow_identities: []
```

`enable_tcp` and `enable_tls` are independent. `enable_tcp` binds `port`, which
speaks TLS when the zone enables it, preserving the historical single-port
behaviour; `enable_tls` binds `secure_port` and always speaks TLS. Setting both
gives a zone two endpoints. A zone with `tls.enabled` and no enabled transport
is a configuration error rather than a silently dead listener.

`peers_policy.allow_unknown` is refused on an external zone, and so is a `peers:`
entry that names an external zone without belonging to a partner. A peer
admitted that way would belong to no partner, so no screening, rate limit or
topology hiding policy would apply to it. For the same reason `dea_screening`
rejects a request from an external-zone peer that resolves to no partner with
`DIAMETER_AUTHORIZATION_REJECTED` and the rule name `unknown_partner`. On an
external zone the partner is resolved from the peer identity alone: honouring
the attacker-controlled `Origin-Realm` would let a peer choose which partner's
policy, PLMN list and rate-limit budget it is judged against.

`realms` matters only on internal zones: it declares realms the agent terminates
on behalf of nodes behind it, so partner traffic toward them is not mistaken for
transit and identities inside them are treated as internal. Entries may use a
`*.example.com` suffix pattern.

### Partners

```yaml
partners:
  - name: partner-a            # required, unique
    zone: roaming              # must reference an external zone
    realms: ["partnera.com", "*.partnera.com"]
    peers:                     # either a plain identity or a mapping
      - "dea1.partnera.com"
      - identity: "dea2.partnera.com"
        priority: 1            # failover tier, lower is preferred
        weight: 5              # share inside the tier, default 1
    plmn_ids: ["24001", "240002"]
    applications: [16777251]   # empty means any
    commands: [316, 318]       # empty means any
    allow_transit: false
    screening: {...}
    topology_hiding: {...}
    rate_limit: {...}
    tls: {...}                 # overrides the zone TLS material for this partner
```

PLMN identifiers may be given with a two- or three-digit MNC; `24001` and
`240001` are the same PLMN, while `240010` is a different one.

### Screening policy

```yaml
screening:
  enabled: true                # defaults to true on external zones
  default_action: reject       # reject | drop | log
  reject_result_code: 5003
  max_route_records: 0         # 0 = unlimited
  check_origin_realm: true
  check_origin_host: true
  check_peer_identity: true
  check_destination_realm: true
  check_plmn: true
  check_applications: true
  check_commands: true
  deny_avps:
    - {code: 1234, vendor_id: 10415}
  allow_avps: []               # non-empty turns this into an allowlist
  rule_actions:
    plmn: log                  # per-rule override of default_action
```

All `check_*` flags default to `true`. Set one to `false` to disable that rule,
or use `rule_actions` to downgrade it to `log` while you tune a new partner.

### Topology hiding policy

```yaml
topology_hiding:
  mode: stateful               # none (default) | static | stateful
  edge_identity: "dea.edge.example.com"   # defaults to the node identity
  pseudonym_prefix: "th"
  pseudonym_realm: "edge.example.com"     # defaults to the node realm
  ttl: 30m
  max_entries: 100000          # per partner; -1 = unlimited (not recommended)
  hide_origin_realm: true
  hide_route_records: true
  hide_proxy_info: true
  hide_error_reporting_host: true
  hide_host_ip_address: true
  hide_origin_state_id: true
  scrub_avps:
    - {code: 318, vendor_id: 10415}
```

All `hide_*` flags default to `true`; they only take effect once `mode` is not
`none`.

### Rate limit policy

```yaml
rate_limit:
  enabled: true
  requests_per_second: 500     # the partner aggregate rule is inline
  burst: 750                   # defaults to max(1, rate)
  action: reject               # reject | drop
  result_code: 3004
  per_peer:
    requests_per_second: 200
  per_application:
    16777251: {requests_per_second: 300, burst: 400}
  per_command:
    316: {requests_per_second: 100}
```

## Extensions

The edge model is inert on its own; enabling the extensions turns it into
behaviour. They slot into the existing handler priority scale, lowest first.

| Extension | Hook | Priority | Role |
| --- | --- | --- | --- |
| `dea_screening` | In | 8 | FS.19 ingress validation |
| `dea_ratelimit` | In | 12 | Per-partner throttling |
| `dea_topohide` | In + PostRoute | 30 | Topology hiding both ways |
| `dea_route` | Out | 55 | Partner-aware routing and failover |

Each extension is inert when no zone/partner model is configured, so adding it to
an existing DRA changes nothing until you declare zones and partners.

### dea_screening

Validates every request arriving on an external zone against its partner's
policy, in GSMA FS.19 categories.

| Rule | Category | Checks |
| --- | --- | --- |
| `origin_realm` | Cat 1 | Origin-Realm is one of the partner's realms |
| `origin_host` | Cat 1 | Origin-Host lies inside Origin-Realm |
| `peer_identity` | Cat 1 | Origin-Host matches the connected peer, or at least its realm |
| `destination_realm` | Cat 2 | Destination-Realm is served by this agent, unless `allow_transit` |
| `application` | Cat 2 | Application-Id is in the partner's list |
| `command` | Cat 2 | Command-Code is in the partner's list |
| `plmn` | Cat 3 | Visited-PLMN-Id and the IMSI in User-Name match the partner's PLMNs |
| `avp_denied` | Cat 3 | No AVP from `deny_avps`, including inside grouped AVPs |
| `avp_not_allowed` | Cat 3 | Every top-level AVP is in `allow_avps` |
| `route_records` | Cat 3 | Route-Record depth is within `max_route_records` |

Actions are `reject` (error answer with `reject_result_code`), `drop` (silent)
and `log`. Error answers never carry an `Error-Reporting-Host`, so a rejection
cannot leak an internal identity.

Extension options: `enabled` (bool), `audit_log` (bool).

### dea_ratelimit

Sharded token buckets, one per scope. A request debits exactly **one** bucket:
the most specific matching scope wins, in the order command → application →
peer → partner aggregate. Answers are never throttled. Buckets rescale live on
reconfiguration and idle buckets are reclaimed after ten minutes.

Extension options: `enabled` (bool), `audit_log` (bool),
`sweep_interval_seconds` (int).

### dea_topohide

Two modes:

- **static** — every internal identity becomes `edge_identity`. Cheap and
  stateless, but a partner cannot address an internal host afterwards.
- **stateful** — every internal host gets an unguessable pseudonym
  (`th<16 hex chars>.<pseudonym_realm>`, from `crypto/rand`), scoped to the
  Session-Id so a partner cannot enumerate the internal network. A later request
  carrying that pseudonym in `Destination-Host` is translated back before
  routing.

On egress to a partner the extension masks `Origin-Host` (and `Origin-Realm`
with it), masks `Error-Reporting-Host`, strips internal `Route-Record` and
`Proxy-Info` entries, `Host-IP-Address`, `Origin-State-Id` and any `scrub_avps`,
then re-adds the edge identity as a Route-Record so loop detection still works.
Answers relayed back to a partner are hidden on the inbound hook. The partner an
answer is headed for is taken from the router's in-flight relay transaction: the
hop-by-hop ID the answer echoes was minted by this node when it relayed the
request, so a partner can neither omit it (as it can omit Session-Id) nor forge
it to point at a laxer policy. The same lookup tells the extension when an answer
is travelling the other way, from a partner back to an internal client, so
operator traffic is left intact. No Session-Id state is kept: an answer with no
relay transaction is not being forwarded to a partner at all, so there is
nothing to hide, and the store's whole budget stays available for pseudonyms.

Extension options: `enabled` (bool), `max_entries` (int),
`sweep_interval_seconds` (int).

The pseudonym map sits behind a small `Store` interface with a TTL/LRU in-memory
implementation, so a shared backend can be added later without touching the
hiding logic.

### dea_route

Builds outgoing candidates from the partner model:

- a realm owned by a partner is served by that partner's peers, grouped into
  priority tiers, with a weighted winner inside the preferred tier and the rest
  of the tier kept available for `rt_busypeers` style failover;
- a message from one partner is never handed to another partner unless the
  source partner has `allow_transit: true`; a refused transit is failed closed,
  so the router answers `DIAMETER_UNABLE_TO_DELIVER` rather than delivering the
  message to an internal peer;
- an external peer never receives a message it does not own the destination of.

Partner routes score above application and realm routes but below an explicit
`Destination-Host` route, so operator overrides still win.

Extension options: `enabled` (bool), `base_score` (int, default 300).

Its counters describe outcomes, so in a purely external-to-internal deployment
`routed_total`, `transit_refused_total` and `foreign_candidates_dropped_total`
all legitimately stay at zero: partner peer lists are only consulted for traffic
heading *toward* a partner. Use `evaluated_total`, which counts every outgoing
message the direction checks ran on, to confirm the control is active.

## Transport security

- TLS material is per zone, so the internal side and the partner side can use
  different CAs and certificates; a partner may override the zone material with
  its own `tls:` block.
- `verify_peer: true` requires `ca_file`; the CA pool is loaded and client
  certificates are verified against it.
- `require_client_cert_identity: true` binds the TLS client certificate (CN or
  DNS SAN) to the `Origin-Host` in the CER and to the partner that owns the
  zone. A mismatch is answered `DIAMETER_UNKNOWN_PEER` (3010).
- External zones deny unknown peers by default.

## Observability

Admin API:

```
GET /api/v1/edge
```

returns the zones and partners as the agent sees them — roles, realms, peers,
allowed applications and commands, which policies are active — and never any key
material.

Prometheus (`prom_metrics`):

| Metric | Labels | Meaning |
| --- | --- | --- |
| `diameter_edge_zones` | – | Number of configured zones |
| `diameter_edge_partners` | – | Number of configured partners |
| `diameter_edge_partner_peers_up` | `partner`, `zone` | Connected peers per partner |
| `diameter_peer_*` | now also `zone`, `partner` | Existing peer metrics, edge aware |

Each extension also exports its own counters through `extension.MetricsProvider`
(screened, rejected and dropped per rule and partner; allowed and limited per
scope; pseudonyms allocated, restored and scrubbed) and reports store sizes and
limiter utilisation through `HealthCheck`.

Screening and rate-limit decisions are logged as single structured lines
carrying the partner, rule or scope, peer, Origin-Host, application and command.

## Operational guidance

1. **Onboard a partner in log mode.** Set `screening.default_action: log` (or
   `rule_actions` for individual rules) and watch the audit lines before
   enforcing. Real interconnects routinely violate at least one rule.
2. **Declare internal realms.** If nodes behind the agent live in a realm other
   than the agent's own, list it under the internal zone's `realms:`, otherwise
   legitimate partner traffic is screened as transit and internal identities are
   not masked.
3. **Size the buckets.** `burst` should cover a partner's normal peak second;
   too small a burst turns bursty-but-legitimate signalling into 3004 storms.
4. **Prefer stateful hiding** unless the partner never needs to address an
   internal host, and keep the TTL above the longest session lifetime you expect
   to see `Destination-Host` reuse for.
5. **Size the pseudonym store against session rate, not topology size.** A
   stateful partner holds one mapping per `(internal host, Session-Id)` pair for
   the whole TTL, so the steady-state requirement is roughly
   `new sessions per second x ttl`. At 1000 sessions/s with the default 30m TTL
   that is 1.8M entries, far above the 100000 default: leave it and mappings are
   recycled long before the TTL, which turns a partner's later
   `Destination-Host` into a restore miss. Either raise `max_entries` to cover
   the product or shorten `ttl` to match real session lifetimes.
   `max_entries` bounds each partner separately, so a flood from one partner
   recycles its own entries instead of evicting a quiet partner's live mappings;
   the health check reports the current size and eviction count.
6. **Do not run `rt_hide_oh` alongside `dea_topohide`.** `dea_topohide`
   supersedes it; running both double-rewrites Origin-Host.
7. **Watch for peers stuck in Suspect.** A watchdog timeout moves a peer to
   `Suspect`, which removes it from routing (`IsOpen()` is false) while the TCP
   connection stays up, so the agent answers `UNABLE_TO_DELIVER` locally with no
   error logged anywhere. Until the fix described below, a peer that entered
   Suspect never rearmed its watchdog and so stayed there permanently. Alert on
   `diameter_peer_up` rather than on connection counts, and treat
   `no routes available after exclusions` as a peer-health signal, not a
   routing-config one.

### The Suspect watchdog defect

Entering `StateSuspect` sent a DWR but did not restart the watchdog timer.
Because the timer is a one-shot `time.AfterFunc`, nothing rescheduled it, so the
`EventTimeout` case in `handleStateSuspect` — which retires the connection and
schedules a reconnect — was unreachable. A single lost DWA therefore stranded
the peer forever.

The defect is in core peer handling, not in any `dea_*` extension, so it
affected plain DRA deployments identically; a relay with no zones configured
reproduces it. It went unnoticed because it needs an *idle* peer: any continuous
traffic resets the watchdog before it can fire. `tests/dea_soak`, whose first
phase is a deliberate idle baseline, is what surfaced it.

Entering Suspect now rearms the watchdog, so a lost DWA closes the connection
and — for persistent peers — reconnects.

## Compatibility notes

Existing configurations are unaffected: without `zones:` or `partners:` the
registry reports itself unconfigured and every `dea_*` extension returns its
input untouched.

Changes worth noting when upgrading:

- `extension.InitContext` gained `GetEdgeRegistry()`. Out-of-tree extensions
  that implement the interface must add it.
- `diameter_peer_*` Prometheus metrics gained `zone` and `partner` labels.
  Dashboards that match on exact label sets need updating.
- `verify_peer: true` now requires `ca_file`; previously the CA pool was never
  loaded, so verification silently used the system pool.

## Differences from freeDiameter

freeDiameter has no edge model. Its `rt_default` routing, `peers` ACLs and
`rt_hide_oh` cover parts of what a DEA does, but:

- there is no zone concept, so one node cannot present different admission
  policies and TLS material to the internal and the partner side;
- topology hiding is a single in-place Origin-Host rewrite with no reverse
  mapping, no Route-Record or Proxy-Info scrubbing and no pseudonyms;
- there is no ingress screening, no PLMN or IMSI validation and no per-partner
  application or command allowlist;
- there is no per-partner throttling;
- client certificates are never bound to the CER identity.

godiam keeps the freeDiameter-compatible behaviour available — the legacy
extensions are unchanged — and layers the edge model on top as opt-in
configuration.

Overload control (DOIC, RFC 7683) is deliberately out of scope; `dea_ratelimit`
handles ingress protection, and DOIC remains a candidate for future work.
