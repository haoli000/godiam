# Diameter Edge Agent Integration Test

End-to-end test of godiam acting as a Diameter Edge Agent (DEA) in front of a
roaming interconnect. Everything runs in one container; no freeDiameter image is
required.

## Topology

```
            internal zone "core"          external zone "roaming"
                     :4868                        :5868
  hss.core.realm ──────────────  dea.edge.realm  ──────────────  cli.good.realm   (well behaved)
  (test_app answers)                                          ├──  cli.evil.realm   (disallowed application)
                                                              └──  cli.flood.realm  (bursts past its rate limit)
```

| Node | Identity | Role |
| --- | --- | --- |
| `hss.yaml` | `hss.core.realm` | Internal application node, answers every request |
| `dea.yaml` | `dea.edge.realm` | Edge agent with `dea_screening`, `dea_ratelimit`, `dea_topohide`, `dea_route`, `admin_api`, `prom_metrics` |
| `cli_good.yaml` | `cli.good.realm` | Partner `partner-good`, stateful topology hiding, sends one valid request |
| `cli_evil.yaml` | `cli.evil.realm` | Partner `partner-evil`, sends application `4` which it is not entitled to |
| `cli_flood.yaml` | `cli.flood.realm` | Partner `partner-flood`, sends 20 requests against a 1 req/s limit with burst 3 |

## What is asserted

- **Zones**: both zone listeners come up and the internal peer connects.
- **Relaying**: the well behaved partner's request reaches the internal node and
  is answered `DIAMETER_SUCCESS` (2001).
- **Topology hiding**: the answer's `Origin-Host` is a `th<hex>.edge.realm`
  pseudonym and neither the internal identity nor the internal realm ever
  appears on the partner side.
- **Screening**: the disallowed application is rejected with
  `DIAMETER_AUTHORIZATION_REJECTED` (5003) and never reaches the internal node.
- **Rate limiting**: the burst is partly answered `DIAMETER_TOO_BUSY` (3004)
  while traffic inside the burst is still relayed successfully.
- **Admin API**: `GET /api/v1/edge` reports the model and leaks no key material.
- **Metrics**: `diameter_edge_*` series are exported and peer metrics carry the
  `zone` label.

## Usage

```bash
make build && make test   # automated run
make manual               # keep the nodes up, ports 9001/9101 published
make clean
```

Logs for every node are written to `/tmp/<node>.log` inside the container.
