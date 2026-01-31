# rt_session_bind - Session-Affinity Routing Extension

Session-affinity (sticky session) routing for the godiam Diameter Routing Agent. Ensures subsequent requests in the same session or for the same subscriber are routed to the same backend peer.

## Use Cases

- **Policy control (Gx/Rx)**: All requests for a subscriber's policy session must reach the same PCRF
- **Charging (Gy/Ro)**: Credit-control sessions must stay on the same OCS
- **Multi-session correlation**: Bind by Subscription-Id (IMSI) so related Gx and Rx sessions reach the same PCRF

## How It Works

1. When a request arrives at the DRA, the extension extracts a **binding key** from the message (Session-Id or Subscription-Id-Data)
2. If a binding already exists for that key, the bound peer's routing score is boosted by `score_boost` (default +80)
3. After the router selects a peer, the **PostRouteHandler** records or updates the binding
4. If the bound peer is no longer available (down/removed from candidates), normal routing proceeds and a new binding is created for the newly selected peer
5. Bindings expire after a configurable TTL and are cleaned up periodically

## Configuration

```yaml
extensions:
  - name: rt_session_bind
    config:
      binding_key: "session_id"       # "session_id" or "subscription_id"
      score_boost: 80                 # Score boost for bound peer (default: 80)
      ttl: "24h"                      # Binding time-to-live (default: 24h)
      max_bindings: 100000            # Maximum bindings in memory (default: 100000)
      cleanup_interval: "5m"          # Expired binding cleanup interval (default: 5m)
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `binding_key` | string | `session_id` | Key to extract from messages: `session_id` (AVP 263) or `subscription_id` (AVP 443 -> 444) |
| `score_boost` | int | `80` | Score added to the bound peer's routing score. Should be > 15 (realm) and < 100 (destination-host) |
| `ttl` | duration | `24h` | How long a binding stays valid. Refreshed on each matching request |
| `max_bindings` | int | `100000` | Maximum number of bindings. Oldest (by last-used) are evicted when full |
| `cleanup_interval` | duration | `5m` | How often expired bindings are removed from memory |

### Binding Key Options

**`session_id`**: Binds by the Diameter Session-Id (AVP code 263). Each unique session always routes to the same peer. Use this for single-session affinity.

**`subscription_id`**: Binds by the Subscription-Id-Data (inside grouped AVP code 443). All sessions for the same subscriber (IMSI/MSISDN) route to the same peer. Use this for multi-session correlation (e.g., Gx/Rx to same PCRF).

## Score Interaction

The default score boost of 80 is chosen to interact correctly with the existing scoring:

| Route Source | Score | vs. Session Bind |
|-------------|-------|-------------------|
| Destination-Host match | 1000 | Wins (explicit host routing takes priority) |
| DH fallback | 500 | Wins |
| App routes | 200 | Wins |
| **Realm + session-bind** | **100 + 80 = 180** | **Bound peer wins over unbound realm peers** |
| Realm route (unbound) | 100 | Loses to bound peer |
| Dynamic realm peer | 90 | Loses to bound peer |

## Interaction with Other Extensions

- **rt_ignore_dh**: Compatible. When Destination-Host is stripped, session binding provides the affinity that DH would have provided.
- **rt_hide_oh**: Compatible. Topology hiding does not affect session binding since binding keys are Session-Id or Subscription-Id, not Origin-Host.
- **rt_rewrite**: Compatible. If rt_rewrite modifies AVPs, ensure the binding key AVP is not dropped. rt_session_bind runs at priority 80, after rt_rewrite (priority 40).

## Example: DRA with Session Binding

```
client --> DRA (rt_session_bind) --> backend1
                                 --> backend2
```

```yaml
# DRA configuration
identity: "dra.example.com"
realm: "example.com"

routing:
  relay_enabled: true
  realm_routes:
    "backend.realm":
      - "backend1.backend.realm"
      - "backend2.backend.realm"

extensions:
  - name: rt_session_bind
    config:
      binding_key: "subscription_id"
      score_boost: 80
      ttl: "24h"
```
