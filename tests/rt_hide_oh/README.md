# rt_hide_oh Integration Test

This test validates a Diameter gateway that hides the original Origin-Host on inbound requests using the `rt_hide_oh` Go extension. The gateway replaces Origin-Host with `gw.backend.realm` and strips the Proxy-Info marker before forwarding, while relaying by Destination-Realm to the responder in `backend.realm`.

## Topology

- Gateway: `gw.backend.realm`
  - Relaying enabled, `rt_hide_oh` active
  - Realm route for `backend.realm` to `server.backend.realm`
- Left peer: `peer11.access.realm`
  - Sends a `test_app` request into realm `backend.realm`
  - Routes to `gw.backend.realm` via host/realm routes
- Right responder: `server.backend.realm`
  - Handles requests via `test_app`

## How It Works

1. `client` sends a `test_app` request with Destination-Realm `backend.realm`.
2. `client` routes the request to `gw.backend.realm` using its host/realm routes.
3. `gw` rewrites Origin-Host via `rt_hide_oh` to `gw.backend.realm`, strips Proxy-Info marker, then relays by Destination-Realm to `server.backend.realm` per `realm_routes`.
4. `server` handles the request via `test_app` and replies. `gw` forwards the answer back.

## Run

Using the Makefile:

```bash
cd tests/rt_hide_oh
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

1. All 3 processes are running (gw, server, client)
2. client sent test message
3. Origin-Host was replaced with gateway identity (gw.backend.realm)
4. client received answer

## Expected Logs

```t
[client] 2026-01-31 19:34:19.701 | test_app send request: code=272 app=9999 hbh=2 e2e=582655355 from=peer11.access.realm/access.realm to=/backend.realm next=gw.backend.realm avps=5
[gw] 2026/01/31 19:34:19 rt_hide_oh: Replaced Origin-Host peer11.access.realm with gw.backend.realm
[client] 2026/01/31 19:34:19 DEBUG: Incoming Diameter message: Code=272, App=9999, Request=false, DestHost=, DestRealm=, OriginHost=server.backend.realm, OriginRealm=backend.realm
[gw] 2026/01/31 19:34:19 DEBUG: Incoming Diameter message: Code=272, App=9999, Request=true, DestHost=, DestRealm=backend.realm, OriginHost=gw.backend.realm, OriginRealm=access.realm
[client] 2026-01-31 19:34:19.703 | test_app answer: code=272 app=9999 hbh=2 e2e=582655355 from=server.backend.realm/backend.realm avps=5 rr=[]
[gw] 2026/01/31 19:34:19 rt_hide_oh: Stripped Proxy-Info marker before forwarding
[client] 2026-01-31 19:34:19.703 | test_app result: 2001
[gw] 2026/01/31 19:34:19 DEBUG: Incoming Diameter message: Code=272, App=9999, Request=false, DestHost=, DestRealm=, OriginHost=server.backend.realm, OriginRealm=backend.realm
[server] 2026/01/31 19:34:19 DEBUG: Incoming Diameter message: Code=272, App=9999, Request=true, DestHost=, DestRealm=backend.realm, OriginHost=gw.backend.realm, OriginRealm=access.realm
[server] 2026-01-31 19:34:19.703 | test_app request: code=272 app=9999 hbh=2 e2e=582655355 from=gw.backend.realm/access.realm to=/backend.realm avps=6 rr=[gw.backend.realm]
[server] 2026-01-31 19:34:19.703 | test_app answer sent: code=272 app=9999 hbh=2 e2e=582655355 to=gw.backend.realm avps=5
```

## Notes

- Startup order ensures `server` connects to `gw` before `client` sends; see entrypoint for timing.
- `rt_hide_oh` rewrites Origin-Host and removes Proxy-Info markers; responder observes `OriginHost=gw.backend.realm`.
- Test client reminder: `rt_hide_oh test: client will auto-send a test message on start.`
