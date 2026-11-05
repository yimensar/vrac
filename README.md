# Vrac

Vrac is a self-hosted workspace for notes, projects, tasks, meetings and
events, shared by you and your AI. You write in a daily journal with `#type`
and `[[reference]]`; Vrac gathers everything where it matters. Any AI client
connects through MCP and keeps it up to date. See [SPEC.md](SPEC.md).

## Run it locally

With Nix:

```bash
nix run . -- serve -timezone Europe/Paris
```

Open http://localhost:8080, choose the instance password, and start writing.
Data lives in `./data/vrac.sqlite` (change it with `-data DIR`).

## Develop

```bash
nix develop
```

Then, in two terminals:

```bash
go run ./cmd/vrac serve -timezone Europe/Paris
```

```bash
cd web && npm install && npm run dev
```

The Vite server (http://localhost:5173) proxies the API to the Go server.
To serve the built interface from the Go binary instead:

```bash
(cd web && npm run build) && go build -o bin/vrac ./cmd/vrac && ./bin/vrac
```

Checks:

```bash
gofmt -l . && go vet ./... && go test -race ./...
```

```bash
cd web && npm run typecheck && npm test && npm run build
```

`nix develop` sets `CGO_ENABLED=1` and `GOFLAGS=-tags=sqlite_fts5`; outside it,
pass `-tags sqlite_fts5` yourself.

## Connect an AI

Vrac exposes MCP over Streamable HTTP at `<url>/mcp` with a built-in OAuth
server (dynamic registration, PKCE). In Claude or ChatGPT, add a custom
connector with that URL and approve it in Vrac. Those services must reach the
instance over HTTPS, so a local instance needs a tunnel or a deployment with a
public URL (`-url https://vrac.example.com`).

Local clients such as Claude Code can use a personal token created in
Settings:

```bash
claude mcp add --transport http vrac http://localhost:8080/mcp --header "Authorization: Bearer vrac_…"
```

The [`vrac` skill](skills/vrac/SKILL.md) tells the AI when and how to write.

## Configuration

| Flag | Environment | Default |
|---|---|---|
| `-data` | `VRAC_DATA` | `data` |
| `-listen` | `VRAC_LISTEN` | `127.0.0.1:8080` |
| `-url` | `VRAC_PUBLIC_URL` | `http://localhost:<port>` |
| `-timezone` | `VRAC_TIMEZONE` | system time zone |

Maintenance: `vrac backup PATH`, `vrac export [PATH]`, `vrac import PATH`
(into an empty instance), `vrac check`.

## License

AGPL-3.0-only.
