#!/usr/bin/env python3
"""Benchmark authenticated HTTP search with a private, explicitly judged query set.

The input is [{"query":"...", "expected":["note-id"]}]. Query text and note
content are never emitted. Warmups are excluded. Run against an otherwise idle
local server; HTTP wall time includes JSON encoding, loopback and decoding.
"""
import argparse
import http.cookiejar
import json
import math
import os
from pathlib import Path
import statistics
import time
import urllib.parse
import urllib.request


def percentile(values, fraction):
    return sorted(values)[max(0, math.ceil(len(values) * fraction) - 1)]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default="http://127.0.0.1:5235")
    parser.add_argument("--queries", type=Path, required=True)
    parser.add_argument("--iterations", type=int, default=20)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    if args.iterations < 1:
        parser.error("iterations must be positive")
    password = os.environ.get("VRAC_BENCH_PASSWORD")
    if not password:
        parser.error("VRAC_BENCH_PASSWORD must be supplied through the environment")
    base = args.url.rstrip("/")
    if urllib.parse.urlsplit(base).hostname not in {"127.0.0.1", "localhost", "::1"}:
        parser.error("this benchmark only sends credentials to a loopback server")
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    request = urllib.request.Request(base + "/api/login", data=json.dumps({"password": password}).encode(), headers={"Content-Type": "application/json", "Origin": base}, method="POST")
    with opener.open(request, timeout=10) as response:
        response.read()
    queries = json.loads(args.queries.read_text())
    latencies = []
    server_times = []
    hit1 = hit5 = 0
    reciprocal_ranks = []
    modes = {}
    for item in queries:
        path = base + "/api/notes?" + urllib.parse.urlencode({"q": item["query"], "limit": 20})
        for iteration in range(args.iterations + 1):
            started = time.perf_counter_ns()
            with opener.open(path, timeout=30) as response:
                data = json.load(response)
            elapsed = (time.perf_counter_ns() - started) / 1_000_000
            if iteration:
                latencies.append(elapsed)
                server_times.append(data["tookMs"])
        ids = [note.get("sourceId") or note["id"] for note in data["notes"]]
        expected = set(item["expected"])
        rank = next((index + 1 for index, uid in enumerate(ids) if uid in expected or uid.removeprefix("memos/") in expected), None)
        hit1 += rank == 1
        hit5 += rank is not None and rank <= 5
        reciprocal_ranks.append(1 / rank if rank else 0)
        modes[data["mode"]] = modes.get(data["mode"], 0) + 1
    results = {"queries": len(queries), "requestsMeasured": len(latencies), "warmupRequests": len(queries), "hitAt1": hit1 / len(queries), "hitAt5": hit5 / len(queries), "mrrAt20": statistics.mean(reciprocal_ranks), "httpMs": {"p50": percentile(latencies, .5), "p95": percentile(latencies, .95), "max": max(latencies)}, "storeMs": {"p50": percentile(server_times, .5), "p95": percentile(server_times, .95)}, "modes": modes}
    text = json.dumps(results, indent=2)
    if args.output:
        args.output.write_text(text + "\n")
        args.output.chmod(0o600)
    print(text)


if __name__ == "__main__":
    main()
