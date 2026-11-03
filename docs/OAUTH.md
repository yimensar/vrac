# MCP authentication and deployment

Vrac exposes a native OAuth-protected Streamable HTTP MCP endpoint at `/mcp`. It uses the official MCP Go SDK and the maintained `go-oauth2/oauth2/v4` authorization server library. Browser sessions and MCP access tokens are separate credentials.

## Local development

Build the frontend, then start the application on the default loopback address:

```sh
pnpm --dir web install
pnpm --dir web build
go run ./cmd/vrac -web web/dist
```

Visit `http://127.0.0.1:5235`. On first run, create an owner password containing 12–72 bytes. The password is stored as a bcrypt hash. Sessions last 30 days and use random opaque cookies stored as hashes. The default data directory is the platform's user configuration directory followed by `vrac`; set `-db` and `-attachments` explicitly to select another private directory.

For a Vite development server, use a same-origin API proxy with `changeOrigin: true` and start Go with `-dev-origin http://127.0.0.1:5173` (or the exact loopback origin used by Vite).

The browser and proxy must preserve the canonical public Host header. Accessing `localhost` when the configured public URL uses `127.0.0.1` is intentionally rejected; either use the printed URL or configure `-public-url` explicitly.

## HTTPS deployment

Remote MCP clients such as ChatGPT need an HTTPS URL reachable from their environment. A local HTTP endpoint and successful local protocol tests do not establish a live ChatGPT connection.

1. Build the Go binary and frontend, and place private data outside the source checkout.
2. Configure an HTTPS reverse proxy that preserves the canonical Host and forwards `/mcp`, `/oauth/*`, and `/.well-known/*` as well as normal application routes. The proxy does not inject authentication credentials.
3. Set `-public-url https://notes.example.com`. The MCP resource identifier is exactly `https://notes.example.com/mcp`.
4. Before exposing a fresh instance remotely, supply `VRAC_OWNER_PASSWORD` through your service's secret environment. It is only used to initialize an owner when none exists. Unset it after initialization. Remote first-run binding without an owner is refused.
5. Connect the client to `https://notes.example.com/mcp`, choose OAuth, authenticate on Vrac's consent page and approve the requested scopes. Register the exact callback shown by the client; the dynamic registration endpoint handles this automatically for compatible clients.

Vrac validates the configured canonical Host and browser Origin. Do not compensate for rejected requests by disabling those checks; align the public URL, proxy Host, and client resource parameter.

## OAuth contract

- `GET /.well-known/oauth-protected-resource/mcp` (also available without the `/mcp` suffix) advertises the resource and authorization server.
- `GET /.well-known/oauth-authorization-server` advertises endpoints and supported features.
- `POST /oauth/register` supports OAuth Dynamic Client Registration for public clients with `token_endpoint_auth_method=none`. Redirect URIs must exactly match registration and use HTTPS, with HTTP permitted only for loopback callbacks. CIMD and confidential-client authentication are not implemented.
- `GET/POST /oauth/authorize` accepts authorization code flow with mandatory PKCE S256, OAuth state, an exact resource URL, and explicit owner consent. Consent uses a separate CSRF token.
- `POST /oauth/token` accepts form-encoded authorization code and refresh grants. Code exchange requires `resource=<canonical MCP URL>`. Refresh may omit `resource`, as standard OAuth TokenSource implementations do: the stored refresh token remains bound to its original resource and client. An explicit different resource is rejected. Authorization codes expire after five minutes and cannot be replayed. A failed verifier consumes the authorization code and requires a new authorization.
- Access tokens expire after one hour. Refresh tokens expire after 30 days, rotate on use and invalidate the preceding access/refresh pair. Requests cannot refresh another registered client's token or expand its approved scopes.
- `POST /oauth/revoke` revokes an access/refresh pair belonging to the supplied public `client_id`.
- Authenticated `POST /api/connections/revoke` revokes all MCP tokens from the owner interface.
- `notes:read` is required for connecting. Add `notes:write` to create or modify notes. Scope checks are enforced on the server; catalog annotations are only client hints.

Tokens are opaque and tied to the canonical resource in persistent SQLite storage. Only the Authorization Bearer header is accepted for MCP calls; query-string tokens are rejected. An unauthenticated request returns a `401` challenge pointing to protected-resource metadata. Read-only connections attempting a write receive an MCP error with the standard OAuth reauthorization metadata.

Client registration and login have bounded in-memory rate limiting. Registration has a 512-client capacity limit. Each registration attempt first removes expired or fully revoked token records and registrations older than 24 hours with no live authorization code or token. Active clients and recent pending registrations are retained. The application is designed as a single-owner, single-process personal service; these limits are not a distributed abuse-prevention system. Keep the database, WAL files, imported attachments, and exports private and include them in a consistent backup procedure.

## Verification

`go test -race ./internal/auth ./internal/mcp ./internal/server` covers browser authentication, CSRF/Host protection, protocol discovery, consent, PKCE failure and code replay, client/resource binding, scope escalation rejection, token expiry and audience checks, refresh rotation and reuse rejection, revocation, MCP read/write permissions, and ChatGPT catalog extension metadata.

The tests use synthetic data and real SQLite files. They do not claim live authentication against a remote ChatGPT account or verify a deployment-specific TLS/proxy configuration.
