#!/usr/bin/env python3
"""Sample a Prometheus endpoint into a CSV for the DEA soak test.

Runs for the whole test and tags every sample with the phase the driver script
is currently in, so resource behaviour can be attributed to a scenario rather
than to wall-clock position.
"""

import re
import sys
import time
import urllib.error
import urllib.request

# metric name -> column name. Sums all label permutations of a metric.
WANTED = {
    "process_resident_memory_bytes": "rss_bytes",
    "process_cpu_seconds_total": "cpu_seconds",
    "go_goroutines": "goroutines",
    "go_memstats_heap_inuse_bytes": "heap_inuse_bytes",
    "go_memstats_heap_objects": "heap_objects",
    "go_memstats_alloc_bytes_total": "alloc_bytes_total",
    "diameter_ext_dea_topohide_store_pseudonyms": "pseudonyms_held",
    "diameter_ext_dea_topohide_store_evictions_total": "store_evictions",
    "diameter_ext_dea_topohide_store_expired_total": "store_expired",
    "diameter_ext_dea_topohide_pseudonyms_allocated_total": "pseudonyms_allocated",
    "diameter_ext_dea_ratelimit_buckets": "limiter_buckets",
    "diameter_ext_dea_screening_screened_total": "screened",
    "diameter_edge_partner_peers_up": "partner_peers_up",
}

COLUMNS = ["ts", "phase"] + list(WANTED.values())

SAMPLE_RE = re.compile(r"^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{[^}]*\})?\s+(-?[0-9.eE+]+)$")


def scrape(url):
    """Return {column: float} for the metrics we care about, or None."""
    try:
        with urllib.request.urlopen(url, timeout=2) as resp:  # noqa: S310
            body = resp.read().decode("utf-8", "replace")
    except (urllib.error.URLError, OSError, ValueError):
        return None

    totals = {}
    for line in body.splitlines():
        if not line or line[0] == "#":
            continue
        m = SAMPLE_RE.match(line.strip())
        if not m:
            continue
        col = WANTED.get(m.group(1))
        if col is None:
            continue
        try:
            totals[col] = totals.get(col, 0.0) + float(m.group(3))
        except ValueError:
            continue
    return totals


def main():
    url, csv_path, phase_path, interval = sys.argv[1], sys.argv[2], sys.argv[3], float(sys.argv[4])

    with open(csv_path, "w", encoding="utf-8") as out:
        out.write(",".join(COLUMNS) + "\n")
        out.flush()
        start = time.time()
        while True:
            try:
                with open(phase_path, encoding="utf-8") as pf:
                    phase = pf.read().strip() or "unknown"
            except OSError:
                phase = "unknown"
            if phase == "done":
                return

            values = scrape(url)
            if values is not None:
                row = ["%.2f" % (time.time() - start), phase]
                row += ["%.4f" % values.get(c, 0.0) for c in WANTED.values()]
                out.write(",".join(row) + "\n")
                out.flush()
            time.sleep(interval)


if __name__ == "__main__":
    main()
