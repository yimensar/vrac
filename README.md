# Vrac

A quiet home for small notes and useful context. Capture in Markdown, find the right fragment, and let your assistant reconstruct the bigger picture when you need it.

Go · SQLite FTS5 · React · shadcn/ui · native OAuth MCP

## The workspace

- Live visual Markdown with a source mode, code blocks, tables, task lists and a recoverable local draft.
- Timeline or collapsible outline, focused reading, keyboard shortcuts and layouts for desktop, tablet and phone.
- Hashtag completion, hierarchical tags, pinned notes, archives, recoverable trash and revision history.
- Ranked full-text search with accent normalization, prefixes, limited French/English query expansion and an explicitly labeled partial-match fallback.
- Five MCP tools for search, batch reading, capture, revision-safe updates and tag discovery. OAuth uses PKCE, scoped tokens, refresh rotation and revocation.
- Repeatable Memos migration preserving Markdown, source identifiers, dates, tags, archived state, relations, attachment bytes and original metadata.

There is no mandatory folder hierarchy or master document. The outline is a navigation view over independent notes. AI synthesis happens in your connected assistant; Vrac supplies the source notes. Search currently indexes note Markdown, titles and tags. It does not use embeddings or index attachment bodies.

## Run locally

Requirements: Go 1.26+, Node.js 24+ and pnpm 11.

```sh
make build
./bin/vrac
```

Open the printed URL, normally <http://127.0.0.1:5235>, and choose an owner password on first use. Data lives in your operating system's user configuration directory under `vrac`, outside the repository. Use `-db` and `-attachments` to select another private directory:

```sh
./bin/vrac \
  -db /private/path/vrac.db \
  -attachments /private/path/attachments \
  -web web/dist
```

The server listens only on loopback by default. The browser URL's host must match the printed canonical URL. Local note content and credentials are not part of the public source repository.

For development, run these in separate terminals:

```sh
go run ./cmd/vrac -dev-origin http://127.0.0.1:5173
pnpm --dir web dev
```

The Vite proxy forwards API requests to Go on port 5235. Production uses the built static frontend and does not require Node.js at runtime.

## Bring your Memos notes

In Memos, open **Settings → Export & Import → Export memos**. The native ZIP includes active and archived notes, comments and attachments.

```sh
./bin/vrac-import \
  --input /private/path/memos-export.zip \
  --db /private/path/vrac.db \
  --attachments-dir /private/path/attachments
```

The importer validates file paths, declared sizes and attachment SHA256 before importing notes. The note write is transactional. Stable source IDs make repeated imports skip existing notes without overwriting subsequent Vrac edits. Original Memos attachment and note links remain usable in Vrac. A connector JSON export can optionally supplement relations omitted by the native format using `--metadata-json`.

Export current notes and source metadata:

```sh
./bin/vrac-export --db /private/path/vrac.db --output /private/path/vrac-export.json
python3 scripts/verify-import.py \
  --source /private/path/memos-export.zip \
  --export /private/path/vrac-export.json \
  --attachments /private/path/attachments
```

JSON export contains current notes and attachment metadata, not the attachment bytes or complete revision database. For a complete backup, stop Vrac, then back up the entire private data directory, including SQLite/WAL files and attachments. Keep the original migration ZIP as well. Do not copy a live SQLite database with an ordinary file copy.

## Connect your assistant

For ChatGPT or another remote client, host Vrac behind HTTPS and connect to `https://your-domain/mcp`. Vrac handles OAuth itself; the reverse proxy only forwards traffic and preserves the canonical Host. Read [OAuth and deployment](docs/OAUTH.md) for bootstrap, discovery, scopes and exact resource identifiers. A local HTTP test is not a verified live ChatGPT connection.

| Tool | Purpose |
| --- | --- |
| `search_notes` | Compact ranked matches, source URLs, filters and pagination |
| `get_notes` | Read up to 20 complete source notes |
| `create_note` | Capture Markdown with an idempotency key for retries |
| `update_note` | Change content or state using the current version |
| `list_tags` | Reuse existing tags |

The owner interface can revoke MCP access. Reading and writing require distinct scopes. Retrieved note content is data and must not be treated as executable assistant instructions.

Docker Compose is provided for an HTTPS reverse proxy setup:

```sh
cp .env.example .env
# Set your canonical HTTPS URL and a unique owner password in .env.
docker compose up -d --build
```

Compose exposes the backend only on host loopback. The image recipe is supplied but was not built during the macOS verification run; the native Go/React build was verified.

## Verification and measurements

```sh
make test
go run ./cmd/vrac-bench --sizes 1000,10000,100000 --iterations 20
```

See [measured results and limitations](docs/BENCHMARKS.md). Benchmarks use file-backed SQLite, real decoding and explicit cache/workload conditions. The comparison is against a substring SQL baseline on the same corpus, not an end-to-end benchmark of a deployed Memos server.

Repository fixtures and published benchmark data are synthetic. Private imports, credentials, databases and attachment files stay outside Git.

## Architecture

`internal/store` owns persistence and search. HTTP and MCP share its validation, transactions and version checks. `internal/auth` implements browser and OAuth authentication. `web` owns the React interface; all canonical note state lives in SQLite.

[API contract](docs/API.md) · [OAuth contract](docs/OAUTH.md) · [Benchmarks](docs/BENCHMARKS.md)

Licensed under [AGPL-3.0](LICENSE).
