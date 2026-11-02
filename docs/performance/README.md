# Five-million-node engine scenario

Measurements on 2026-11-02 used an Apple M5 Mac with 24 GiB RAM, Darwin 25.6.0
arm64, Go 1.26.5 and bundled SQLite 3.53.4 (`mattn/go-sqlite3` v1.14.52).
The engine retained foreign keys, WAL and FULL synchronization throughout.
The connection page cache is bounded at 16 MiB. These are local engine timings;
HTTP/MCP transport, model latency and UI rendering are not included.

## Reproduce

Use local storage and an absent database path. The scenario refuses populated
non-fixture data. Generation is a background operation and includes canonical
revision snapshots and the live FTS/branch-maintenance triggers.

```sh
go build -trimpath -tags sqlite_fts5 -o /tmp/vrac-bench ./cmd/vrac-bench
/tmp/vrac-bench -nodes 5000000 -iterations 1000 /local/path/new-benchmark.sqlite
```

The fixture has five million historical nodes grouped under 1,000 dated
meetings, plus current plain and metadata-rich pages, multiple tags, stable
references, a 104-node path, a project covering approximately 50,000 historical
leaf nodes, and a sparse project. Measurements include creation, content/tag
edits, target renames, sibling moves, text search, exact project/date filters,
and untagged project browsing. Each operation reports p95 and independently
checks the unchanged 2 ms reference-machine budget. A failed gate exits nonzero.

To repeat against that same generated fixture:

```sh
/tmp/vrac-bench -reuse -iterations 1000 /local/path/benchmark.sqlite
```

Reuse mutates benchmark notes and appends their canonical activity/revision
history. It never optimizes away that history. Run timing-sensitive checks
without simultaneous builds, test suites or other backup/write workloads.
No warmup operations are skipped, no FTS optimize is run just for measurement,
and the benchmark never changes durability settings.

## Recorded results and limits

- `baseline-pure-go.json`: initial modernc-based implementation, 5,002,807
  nodes and 100 iterations. Text search and a selective tag in a large project
  failed the budget. This predates the final query and activity-context model;
  it is engineering evidence, not an identical-workload language comparison.
- `native-fresh.json`: fresh final-schema fixture, 5,007,310 nodes and 1,000
  iterations. Fifteen of sixteen gates passed. Reference-target rename p95
  was 4.064 ms; the failure is retained rather than omitted.
- `native-steady.json`: immediate quiescent repeat on the same fixture,
  5,012,310 nodes and 1,000 iterations, without data cleanup or index optimize.
  All sixteen reported gates passed, ranging from 0.041 to 1.483 ms.
- `native-final.json`: required post-fix repeat after distinguishing omitted
  versus explicitly empty reference lists in mutation receipt identities.
  At 5,017,310 nodes and 1,000 iterations, all sixteen gates passed, ranging
  from 0.039 to 1.893 ms. No schema, index or durability setting changed.

The passing repeat demonstrates the measured steady scenario, not a guarantee
about cold-cache or first-write latency. The initial rename spike remains an
explicit limitation to monitor before a broader release. Linux and Windows
baselines, deployed MCP round-trip timings and browser rendering performance
are not established by this Mac-only engine scenario.

The chosen optimizations address observed costs: a native SQLite driver,
bounded page cache, one batched hydration of shared context paths, an indexed
set of internal branches instead of descendant-leaf traversal, and bounded
per-parent streams for untagged subject queries. The branch index is derived,
transactionally maintained and rebuildable; moves update no descendants.

## Query-plan evidence

`query-plans.txt` records EXPLAIN QUERY PLAN from bundled SQLite 3.53.4 on the
final fixture. Tagged lookup starts at `tags_by_tag`, uses the covering node
context index, and materializes only the internal-branch scope. Untagged browse
seeks reverse references and `(parent_id,deleted,id)` child ranges; its final
sort merges bounded streams. Work still depends on the number of relevant
internal branches; these plans do not establish constant-time arbitrary-tree
search.

A read-only diagnostic on the earlier same five-million-node fixture compared
the identical branch-scope query through both drivers (200 iterations): native
SQLite raw p95 was 0.854 ms, versus approximately 1.8 ms through pure Go. The
previous per-candidate ancestor query still took 2.399 ms with native SQLite,
before hydration. This justified retaining the small derived branch index as
well as the native driver, rather than assuming the driver alone solved it.

## Full integrity validation

`integrity.txt` records a successful full check at 5,012,310 nodes with zero
issues. It verifies SQLite integrity and foreign keys, tree reachability,
protected identities, derived branch membership, revision identity/current
snapshots, canonical tags, and FTS consistency with canonical content. This
background operation took 148.83 seconds while final build work was also
allowed; it is outside the interactive latency budget. The subsequent final
benchmark added 5,000 ordinary transactional mutations, not another bulk load.
