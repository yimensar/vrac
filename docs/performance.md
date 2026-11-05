# Performance

`go run ./scripts/bench -nodes N` builds a realistic workspace and measures
the engine operations behind the interface and MCP, in process (HTTP
excluded). The workspace has 60 projects with uneven activity (the busiest
receives about one reference in eight), 300 people, and journal days of
meetings with notes, decisions and tasks (nine in ten done) plus loose
notes.

Measured on 2026-11-05 on an Apple M-series laptop, release Go build,
SQLite in WAL with FULL synchronization.

## 20 000 nodes (about two years of heavy use)

| operation | p50 | p95 |
|---|---|---|
| write a journal line | 0.36 ms | 0.58 ms |
| edit a line | 0.31 ms | 0.48 ms |
| read a journal day | 0.14 ms | 0.20 ms |
| search words | 0.79 ms | 1.00 ms |
| open tasks of the busiest project | 0.22 ms | 0.34 ms |
| tasks of a project and a person | 0.15 ms | 0.20 ms |
| sections of the busiest project | 3.9 ms | 4.3 ms |
| complete a name | 0.22 ms | 0.26 ms |
| complete words | 0.62 ms | 0.77 ms |
| project table | 7 ms | 8.3 ms |

## 200 000 nodes (decades of heavy use)

| operation | p50 | p95 |
|---|---|---|
| write a journal line | 0.37 ms | 1.4 ms |
| edit a line | 0.34 ms | 0.79 ms |
| read a journal day | 0.27 ms | 0.44 ms |
| search words | 3.4 ms | 5.2 ms |
| open tasks of the busiest project | 1.7 ms | 2.0 ms |
| decisions of the busiest project | 0.29 ms | 0.42 ms |
| tasks of a project and a person | 0.85 ms | 1.0 ms |
| sections of the busiest project | 10.3 ms | (10 runs) |
| complete a name | 0.21 ms | 0.24 ms |
| complete words | 2.4 ms | 2.7 ms |
| project table | 60 ms | (10 runs) |

## How it got there

The first measurement at 200 000 nodes computed context at query time and
took 30 ms for a project's open tasks, 137 ms for its sections and 254 ms
for the project table; completion scanned names with `LIKE '%x%'` (17 ms).
Context is now a maintained index read in date order, completion uses
prefix ranges and full text, and the project table reads the index once.

## Known costs

- The project table grows with the number of projects and open tasks: about
  60 ms for 60 projects at 200 000 nodes. It is a page view, not typing.
- Searching very common words has a p99 around 30 ms at 200 000 nodes.
- Moving a node rewrites the context of what references it. Moving the
  busiest project takes about 80 ms at 20 000 nodes and grows with the
  number of nodes referencing it; it is rare, and a set-based rewrite can
  replace the per-node one if it matters.
