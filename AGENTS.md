# Vrac contribution rules

Read the local, intentionally unversioned `ARCHITECTURE.md` before changing
architecture or behavior. This branch replaces the Rust/TUI POC with the
hosted Go product explicitly authorized by the repository owner.

## Priorities and simplicity

Preserve data and integrity first, then maintainability, responsiveness,
scalability, stable identities, and long-term recoverability. Use concrete
types and direct functions. Add abstractions or dependencies only for an
observed need. Keep code, comments, documentation and product output English.

## Boundaries

- `internal/engine` owns model, rules, SQLite, transactions, migrations and
  search. It has no UI, HTTP or network dependency and never prints or exits.
- `internal/server` adapts HTTP/MCP and authentication to engine public APIs.
- `cmd/vrac` composes engine/server and owns process configuration and exits.
- `web` owns React/TypeScript/shadcn UI and transient view state.
- Clients contain no SQL or business rules; evolve the engine API when needed.
- SQLite is the canonical source of truth; search indexes are rebuildable.

## Data and model

Every mutation uses an explicit atomic transaction. Preserve foreign keys,
WAL and FULL synchronization. Schema changes increment user_version and use
explicit tested migrations; never modify a published migration. Refuse
unrelated databases. Backups use SQLite snapshot facilities, never a direct
copy of an open database/WAL. Destructive transformations require a recoverable
copy and integrity checks. Keep runtime secrets outside source and Nix store.

Opaque node IDs survive moves, renames, exports and imports. Numeric sibling
positions are private. Clients express order with Placement. Return siblings
deterministically and paginate large collections with opaque cursors, never
OFFSET. Validate parents and cycles inside the mutation transaction. Moving
a subtree must not rewrite descendants; normal operations must not traverse
the entire tree. Tags classify their own node; ancestor references supply
context. One evolving decision retains its current node; revisions and journal
activity preserve its evolution. Concurrent edits check the expected revision.
Retries use durable request IDs and reject changed payloads.

## Verification

Test the engine through its public API with real SQLite. Use file-backed tests
for persistence, migrations and durability. Cover atomic failure, ordering,
pagination, missing parents, cycles, references across renames, history,
reopening and export/import recovery. Add regression tests for bugs.

Run Go formatting, vet, tests and race tests. Native SQLite requires CGO and
the sqlite_fts5 build tag; nix develop supplies the compiler and GOFLAGS, or
pass -tags sqlite_fts5 explicitly outside that shell. Run frontend typecheck, automated
behavior tests and production build. Evaluate/build affected Nix outputs.
After engine/schema changes run the five-million-node optimized scenario and
check every reported interactive p95 against the 2 ms reference budget.
Measure inherited subject/date searches, metadata-rich pages, tags, references,
renames, edits, moves and deep paths. Report unmet gates honestly. Do not claim
performance without measurement/query plans. Never weaken durability for speed.

Do not perform visual/manual UI inspection unless explicitly requested. Use
automated checks and leave appearance validation to the user by default.

## Change and delivery discipline

Preserve unrelated work. Record durable decisions in ARCHITECTURE.md and keep
public format/API docs current. Commit validated coherent work. Develop on a
feature/fix/docs/chore branch from current main; never push directly to main.
Open PRs after proportional local checks pass. Titles are imperative English;
bodies explain reason and validation. Merge only with required checks passing
and conversations resolved. Squash-merge and delete the branch only when
merging is authorized. Published version tags are immutable and must match
VERSION. Never change branch protection without an explicit request.
