#!/usr/bin/env python3
"""Analyse the DEA soak samples and judge them.

Prints a per-phase resource table, then applies the checks that distinguish
"used memory" from "leaked memory": the pseudonym store must respect its cap,
the sweeper must actually reclaim, and goroutines and heap must come back down
once the load stops.
"""

import csv
import sys

MB = 1024.0 * 1024.0


def load(path):
    with open(path, encoding="utf-8") as fh:
        rows = []
        for row in csv.DictReader(fh):
            rows.append({k: (v if k == "phase" else float(v)) for k, v in row.items()})
    return rows


def phase_rows(rows, phase):
    return [r for r in rows if r["phase"] == phase]


def slope_per_min(rows, col):
    """Least-squares slope of col against time, per minute."""
    n = len(rows)
    if n < 3:
        return 0.0
    mx = sum(r["ts"] for r in rows) / n
    my = sum(r[col] for r in rows) / n
    num = sum((r["ts"] - mx) * (r[col] - my) for r in rows)
    den = sum((r["ts"] - mx) ** 2 for r in rows)
    if den == 0:
        return 0.0
    return (num / den) * 60.0


def cpu_percent(rows):
    if len(rows) < 2:
        return 0.0
    wall = rows[-1]["ts"] - rows[0]["ts"]
    if wall <= 0:
        return 0.0
    return (rows[-1]["cpu_seconds"] - rows[0]["cpu_seconds"]) / wall * 100.0


def median(values):
    s = sorted(values)
    n = len(s)
    if n == 0:
        return 0.0
    return s[n // 2] if n % 2 else (s[n // 2 - 1] + s[n // 2]) / 2.0


GREEN, RED, YELLOW, BLUE, NC = "\033[0;32m", "\033[0;31m", "\033[1;33m", "\033[0;34m", "\033[0m"


def main():
    path, max_entries, phases = sys.argv[1], float(sys.argv[2]), sys.argv[3].split(",")
    rows = load(path)
    if not rows:
        print("no samples collected")
        return 1

    failures = []

    print("")
    print(f"{BLUE}--- Resource profile by phase ---{NC}")
    print("")
    hdr = ("phase", "samples", "cpu%", "rss MB", "heap MB", "goroutines", "pseudonyms")
    print("  %-12s %8s %8s %10s %10s %12s %12s" % hdr)
    print("  %-12s %8s %8s %10s %10s %12s %12s"
          % ("-" * 12, "-" * 8, "-" * 8, "-" * 10, "-" * 10, "-" * 12, "-" * 12))
    for ph in phases:
        pr = phase_rows(rows, ph)
        if not pr:
            continue
        print("  %-12s %8d %8.1f %10.1f %10.1f %12d %12d" % (
            ph, len(pr), cpu_percent(pr),
            max(r["rss_bytes"] for r in pr) / MB,
            max(r["heap_inuse_bytes"] for r in pr) / MB,
            int(max(r["goroutines"] for r in pr)),
            int(max(r["pseudonyms_held"] for r in pr)),
        ))
    print("")
    print("  (cpu%% is over the phase; the rest are phase maxima)")

    base = phase_rows(rows, "baseline")
    steady = phase_rows(rows, "steady")
    drain = phase_rows(rows, "drain")

    print("")
    print(f"{BLUE}--- Leak checks ---{NC}")

    def check(ok, msg):
        if ok:
            print(f"  {GREEN}\u2713{NC} {msg}")
        else:
            print(f"  {RED}\u2717{NC} {msg}")
            failures.append(msg)

    # 1. The store cap must hold. Exceeding it is a memory bug with a security
    #    face: the cap is what stops a partner's session rate from sizing the
    #    agent's heap.
    peak_held = max(r["pseudonyms_held"] for r in rows)
    check(peak_held <= max_entries * 1.02,
          "pseudonym store respected max_entries (peak %d of %d)" % (peak_held, max_entries))

    # 2. Under load the store is filling toward its cap, so RSS climbing during
    #    the steady phase is expected work, not a leak -- the cap check above is
    #    what bounds it. Growth while idle is the unambiguous signal, because
    #    nothing should be accruing at all.
    if steady:
        rss_slope = slope_per_min(steady[len(steady) // 3:], "rss_bytes") / MB
        print("  %s RSS slope while filling the store: %+.2f MB/min (expected; bounded by max_entries)"
              % ("\u00b7", rss_slope))

    if drain and len(drain) > 5:
        idle_slope = slope_per_min(drain[len(drain) // 3:], "rss_bytes") / MB
        check(idle_slope < 1.0,
              "RSS was not growing while idle (%+.2f MB/min)" % idle_slope)
    else:
        check(False, "drain phase produced too few samples to judge idle growth")

    # 3. The TTL sweeper must reclaim. Without this the cap is enforced only by
    #    eviction, which means live mappings are dropped to make room for new
    #    ones while expired ones sit in the map.
    if drain and steady:
        expired_gain = drain[-1]["store_expired"] - drain[0]["store_expired"]
        held_start, held_end = drain[0]["pseudonyms_held"], drain[-1]["pseudonyms_held"]
        check(expired_gain > 0,
              "TTL sweeper reclaimed while idle (%d entries expired)" % expired_gain)
        check(held_end < held_start * 0.5 or held_end == 0,
              "pseudonym store drained while idle (%d -> %d)" % (held_start, held_end))
    else:
        check(False, "drain phase produced no samples")

    # 4. Goroutines must come back. Peers churn throughout the run, so a peer
    #    whose goroutines outlive it shows up here and nowhere else.
    if base and drain:
        base_g = median([r["goroutines"] for r in base])
        tail_g = median([r["goroutines"] for r in drain[-5:]])
        allowed = max(base_g + 10, base_g * 1.25)
        check(tail_g <= allowed,
              "goroutines returned to baseline after load (%d -> %d, allowed %d)"
              % (int(base_g), int(tail_g), int(allowed)))

        # 5. Heap must be reclaimed, not merely stop growing.
        peak_h = max(r["heap_inuse_bytes"] for r in rows)
        base_h = median([r["heap_inuse_bytes"] for r in base])
        tail_h = median([r["heap_inuse_bytes"] for r in drain[-5:]])
        if peak_h > base_h:
            reclaimed = (peak_h - tail_h) / (peak_h - base_h)
            check(reclaimed >= 0.5,
                  "heap was reclaimed after load (%.0f%% of the load-induced growth, "
                  "peak %.1f MB -> %.1f MB, baseline %.1f MB)"
                  % (reclaimed * 100, peak_h / MB, tail_h / MB, base_h / MB))

    print("")
    if failures:
        print(f"{RED}  %d resource check(s) failed{NC}" % len(failures))
        return 1
    print(f"{GREEN}  All resource checks passed{NC}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
