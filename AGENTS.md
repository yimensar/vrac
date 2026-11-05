# Vrac contribution rules

Read [SPEC.md](SPEC.md) before changing behavior, and keep it current.

## Priorities

Preserve data and integrity first, then simplicity, responsiveness and
long-term recoverability. Use concrete types and direct functions. Add
abstractions or dependencies only for an observed need. Code, comments,
documentation and product text are in English.

## Boundaries

- `internal/engine` owns the model, rules, SQLite, transactions, migrations
  and search. No HTTP, UI or network dependency; it never prints or exits.
- `internal/server` adapts HTTP, OAuth and MCP to the engine's public API.
- `cmd/vrac` composes engine and server and owns configuration and exits.
- `web` owns the React/TypeScript/shadcn interface and transient view state.
  It contains no business rules and uses only the public HTTP API.

## Data

Every mutation is one explicit transaction. Keep foreign keys, WAL and FULL
synchronization. Schema changes add a migration and increment user_version;
never modify a published migration. Refuse unrelated databases. Backups use
SQLite snapshots, never file copies of an open database. Node IDs are stable
and opaque; sibling positions are private. Mutations check expected
revisions and use durable request IDs. Nothing is created implicitly.

## Verification

Test the engine through its public API with real, file-backed SQLite. Cover
atomic failure, ordering, pagination, cycles, references across renames,
history, reopening and export/import. Add a regression test for each bug.

Run in `nix develop`: `gofmt -l .`, `go vet ./...`, `go test -race ./...`,
and in `web`: `npm run typecheck`, `npm test`, `npm run build`. Build
`nix build .#default` after dependency changes (update the hashes in
flake.nix). Do not perform visual UI inspection unless asked.

## Delivery

Develop on a branch; never push to main directly. Commits follow
Conventional Commits without AI attribution trailers. Open PRs after local
checks pass.
