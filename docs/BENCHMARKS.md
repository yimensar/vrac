# Measured behavior

Measured on 3 November 2026 on macOS arm64, Apple M5 (10 logical CPUs), 24 GiB RAM, Go 1.26.5. These measurements describe this machine and these workloads.

## Search at increasing volume

The reproducible synthetic corpus has ten technical topics, hierarchical tags, exact identifiers, frequent tokens and misses. Seven single-token probes run 20 times each after warmup. Both implementations return a total count and decode the first 50 complete notes. SQLite is file-backed, WAL, synchronous FULL, with one connection and warm OS/SQLite caches. Calls are serial and in-process; network, browser, embedding/model and cold-start latency are excluded.

| Notes | Ranked FTS search p95 | Substring SQL baseline p95 | Timeline p95 |
| ---: | ---: | ---: | ---: |
| 1,000 | 1.05 ms | 1.29 ms | 0.18 ms |
| 10,000 | 5.23 ms | 17.17 ms | 0.64 ms |
| 100,000 | 48.50 ms | 184.10 ms | 7.84 ms |

At 100,000 notes, an individual read was 0.028 ms p95; capture 0.336 ms; edit 0.329 ms. The largest observed search took 322.42 ms: p95 is not a worst-case guarantee. The synthetic database occupied approximately 189 MB.

A measured optimization removed duplicate BM25 scoring from the total-count query. Under the identical workload, 100k search p95 changed from 64.76 to 48.50 ms (25% lower). Both [before](benchmark-before-count-optimization.json) and [after](benchmark-synthetic.json) reports are retained.

The substring baseline is a case-insensitive SQL `contains` workload on the same database, with equivalent result counts for these seven probes and the same decoded fields. It has no relevance scoring or query expansion. This is not a deployed Memos comparison, and it does not establish equivalent recall for arbitrary queries.

Reproduce:

```sh
go run ./cmd/vrac-bench --sizes 1000,10000,100000 --iterations 20 --output /tmp/vrac-benchmark.json
```

## Imported corpus

The private migration contains 81 notes: 57 active and 24 archived, with 73 attachments. Verification compared native export contents, original creation/update times, states, tags, and all attachment SHA256 values. Repeating import created zero additional notes. The source notes and attachments were not changed in Memos.

On this small corpus, seven engine probes with 30 iterations measured 0.425 ms p95 for ranked search and 0.241 ms for the substring baseline. The simpler baseline is faster at this size; ranking, better retrieval and scaling are the reason to use FTS.

An independently prepared set of 18 manually judged queries includes identifiers, multiword searches, missing accents and several questions. The expected source appeared first for 17/18 queries (94.4%) and within the first five for 18/18 (100%); MRR@20 was 0.972. This is a small curated evaluation, not a general semantic-search accuracy claim.

The final authenticated loopback HTTP run measured 540 requests after 18 excluded warmups: 0.529 ms p50 and 0.789 ms p95, including request handling, JSON encoding and client decoding. The store portion was 0.507 ms p95. The private query file and reports are retained outside Git. The reusable [HTTP benchmark](../scripts/benchmark-http.py) emits aggregate metrics without note content or query text.

Search indexes Markdown, titles and tags. Attachment bodies, OCR and semantic embeddings are not indexed in this version. No model or external AI API is required for capture or search.

## Functional and UI checks

Go regression tests cover durable reopen, explicit migrations, import rollback, ZIP corruption/traversal, version conflicts, idempotency, history, case/accent handling, cursor completeness, filters and index rebuild. HTTP/MCP tests cover browser authentication and the OAuth lifecycle, including a real OAuth client refresh path.

The React production build and interaction tests are part of CI. Visual verification covers desktop, tablet and phone compositions, readable Markdown in light/dark themes, search, capture and focused reading. Browser frame-rate, real mobile hardware, network-constrained clients, concurrent multiuser load and a live remote ChatGPT connection are not benchmarked here.
