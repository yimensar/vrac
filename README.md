# Vrac

A journal-centered knowledge base for people and AI. Capture in an ordered
outline, classify nodes with plain tags, connect subjects with stable
`[[references]]`, and retrieve current knowledge by subject, date or full text.

This branch deliberately replaces the Rust/TUI proof of concept preserved on
`main`. It is a fresh hosted application; it does not migrate the old Vrac
format or modify a Trilium instance.

## Run locally

With Go 1.26, a C compiler and Node 24 (or `nix develop`):

```sh
npm --prefix web ci
npm --prefix web run build
go run -tags sqlite_fts5 ./cmd/vrac serve --dev
```

Open http://127.0.0.1:8080. Development mode is unauthenticated and requires a
loopback listen address. The default database is `data/vrac.sqlite`.

```sh
nix run . -- serve --dev
```

Build a server without the web frontend:

```sh
go build -tags sqlite_fts5,headless -o bin/vrac ./cmd/vrac
bin/vrac serve --dev --no-web
```

The server exposes `/mcp` (Streamable HTTP), `/api/` and `/healthz`. Both
interfaces call the same synchronous Go engine. The web UI uses React,
TypeScript and shadcn/ui. It supports journal capture, nested outlines,
tags, subject references, filtered search and revision history.

## Knowledge model

Every node has a stable ID and one ordered location in the tree. Journal days
are real protected nodes. Tags classify their own node; references in an
ancestor give context to its descendants. Thus a decision inside a meeting
referencing a project is discoverable through that project.

An evolving decision keeps ONE current node. Updates preserve earlier
revisions and add a linked activity entry to the current journal day.
Journal-placement dates and activity dates are separate search filters.
Historical revisions do not become duplicate current search results.

See [model and format](docs/model.md) and [HTTP/MCP API](docs/api.md).

## Production and ChatGPT

Production requires a random access token in a runtime file and HTTPS at the
reverse proxy. The browser exchanges it for an HttpOnly session; generic MCP
clients can use bearer authentication. ChatGPT uses an external OAuth/OIDC
provider, with issuer, dedicated audience and scope validation in Vrac.

The flake supplies a NixOS module, UI/headless packages and a Linux OCI image.
See [deployment](docs/deployment.md), [ChatGPT plugin setup](docs/chatgpt.md),
and the [NixOS/NetBird/Pocket ID profile](docs/homelab.md).
Plugin source and a package generator are included. Installing a live ChatGPT
plugin requires the deployed HTTPS MCP URL and configured identity provider.

## Recovery

```sh
vrac check --db /var/lib/vrac/vrac.sqlite
vrac backup --db /var/lib/vrac/vrac.sqlite --output /backups/vrac.sqlite
vrac export --db /var/lib/vrac/vrac.sqlite --output /backups/vrac.json
vrac import --db /recovery/vrac.sqlite --input /backups/vrac.json
vrac reindex --db /var/lib/vrac/vrac.sqlite
```

Imports require an empty Vrac database. Export and backup destinations are
never silently overwritten. Keep backups outside the application server and
test restoration. Derived search indexes can be rebuilt from canonical data.

## Development checks

```sh
gofmt -w cmd internal web/*.go
go vet -tags sqlite_fts5 ./...
go test -tags sqlite_fts5 ./...
go test -race -tags sqlite_fts5 ./...
npm --prefix web run check
npm --prefix web test
npm --prefix web run build
python3 -m unittest discover -s scripts -p '*_test.py'
nix flake check
```

The file-backed five-million-node scenario is documented with its measured
results in [performance](docs/performance/README.md). Correctness tests alone do not
establish scalability.

Vrac is licensed under AGPL-3.0-only; see [LICENSE](LICENSE).
