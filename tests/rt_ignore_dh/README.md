# rt_ignore_dh Integration Test

This test validates a Diameter gateway that ignores Destination-Host on inbound requests using the `rt_ignore_dh` Go extension. When Destination-Host is dropped, routing falls back to Destination-Realm and the gateway relays to a responder in the target realm.

## Topology

- Gateway: `gw.backend.realm`
  - Relaying enabled, `rt_ignore_dh` active
  - Realm route for `backend.realm` to `server.backend.realm`
- Left peer: `peer11.access.realm`
  - Sends a `test_app` request to Destination-Host `server.backend.realm` in realm `backend.realm`
  - Routes to `gw.backend.realm` via host/realm routes
- Right responder: `server.backend.realm`
  - Handles requests via `test_app`

## How It Works

1. `client` sends a `test_app` request with:
   - Destination-Host = `server.backend.realm`
   - Destination-Realm = `backend.realm`
2. `client` routes the request to `gw.backend.realm` using its host/realm routes.
3. `gw` drops Destination-Host via `rt_ignore_dh`, then relays by Destination-Realm to `server.backend.realm` per `realm_routes`.
4. `server` handles the request via `test_app` and replies. `gw` forwards the answer back.

## Run

Using the Makefile:

```bash
cd tests/rt_ignore_dh
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
3. gw routed message (ignoring Destination-Host)
4. server received the request (proves Destination-Host was ignored)

## Expected Logs

- Gateway shows the AVP drop:
  - `rt_ignore_dh: Dropped Destination-Host for routing`
- Gateway debug:
  - `DEBUG: Incoming Diameter message: Code=272, App=9999, Request=true, DestHost=, DestRealm=backend.realm, OriginHost=peer11.access.realm, OriginRealm=access.realm`
- Responder handles:
  - `test_app request ...`
  - `test_app answer sent ...`

## Notes

- Startup order ensures `server` connects to `gw` before `client` sends; see entrypoint for timing.
- To narrow scope (e.g., only drop DH for certain apps), add simple filters to the `rt_ignore_dh` handler.
