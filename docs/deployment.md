# Deployment

Vrac runs one Go service over one local SQLite database. The web interface is
optional; disabling it preserves the HTTP API and MCP endpoint. Use a local disk
for application data. Access the service through HTTPS in production.

## Development with Nix

```sh
nix develop
cd web
npm ci
npm run build
cd ..
go run ./cmd/vrac serve --dev --listen 127.0.0.1:8080 --db ./tmp/dev.sqlite
```

Development mode is restricted to loopback. For frontend development, run the
Vite development server in `web/` alongside the Go service. The frontend uses
same-origin API paths and Vite proxies them to the local Go service.
The dev shell enables CGO and the `sqlite_fts5` build tag and provides the pinned
C compiler used by the native SQLite driver. Outside that shell, set
`CGO_ENABLED=1` and include `-tags sqlite_fts5` in Go build, test, and run commands.

## Reproducible packages

The checked-in flake lock pins Go, Node, the C compiler, SQLite tooling, and build support.
Go module and npm lockfiles pin application dependencies; the flake verifies
both dependency sets by content hash.

```sh
nix build .#vrac
./result/bin/vrac serve --help
nix build .#vrac-headless
nix flake check
```

The complete package builds the web assets and embeds them in the executable.
The headless package excludes those assets. `--no-web` also disables the web
interface at runtime on a complete package.

## NixOS module

Add this repository as a flake input, import its module, and select a runtime
secret file provisioned by your secret manager:

```nix
# flake.nix: use this feature branch while main contains the Rust prototype.
inputs.vrac.url = "github:yan-imensar/vrac?ref=feature/hosted-mcp-v0";
```

In the NixOS host configuration receiving `inputs`:

```nix
{
  imports = [ inputs.vrac.nixosModules.default ];
  services.vrac = {
    enable = true;
    listenAddress = "127.0.0.1:8080";
    stateDir = "/var/lib/vrac";
    tokenFile = "/run/secrets/vrac-token";
    timezone = "Europe/Paris";
    # Set enableWeb = false for the MCP/API-only package.
  };
}
```

Generate a strong token outside the Nix store, with restrictive permissions.
For example, provision `openssl rand -hex 32` through your secret manager.
`tokenFile` is a runtime filename, never token content. systemd loads it as a
service credential, so the service does not need permission to read the source
secret file. The `vrac` system user owns the state directory, mode `0700`.

The module uses a read-only system, restricted capabilities, a private temporary
directory, and explicit access to the state directory. It does not open firewall
ports or configure a public domain. Put your existing HTTPS reverse proxy in
front of `listenAddress`; forward `/api/`, `/mcp`, and the OAuth protected-resource
metadata routes to Vrac. Preserve the original HTTP Host header and keep MCP
streaming enabled in the proxy.

For ChatGPT, also configure `oauthIssuer`, `oauthAudience`, and optionally
`oauthScope`; see [ChatGPT connection](chatgpt.md).
For browser OIDC sign-in, additionally set `webOIDCClientID` and optionally
`webOIDCProviderName`. Register a public PKCE client with the public MCP
origin's `/auth/callback` as a redirect, and grant user-delegated access to
the configured resource and scope. The CLI equivalents are
`--web-oidc-client-id` and `--web-oidc-provider-name`; see [authentication](api.md).

## Container image from the same flake

Build on a Linux system or with a configured Linux Nix builder:

```sh
nix build .#docker
docker load < result
export VRAC_TOKEN_FILE=/absolute/path/to/your/runtime-token
docker compose up -d
```

The image is tagged `vrac:0.2.0`, uses UID/GID `10001`, and stores the database
in `/data`. The supplied Compose configuration creates a persistent named
volume, binds HTTP to the host's loopback interface, and mounts the token at
`/run/secrets/vrac-token`. The token mount must be readable by UID `10001` inside
the container; arrange ownership through your deployment secret mechanism.
The application container runs with a read-only root filesystem and no Linux
capabilities. Terminate HTTPS at the host reverse proxy.

`nix build .#packages.x86_64-linux.docker` selects an explicit Linux architecture.
A macOS developer needs a Linux builder to execute that build. The image is
assembled by `dockerTools` from the same Nix Go/web package, with pinned CA
certificates and timezone data; no separate Dockerfile dependency pipeline is
used.

## Backups and upgrades

Use Vrac's backup command or SQLite's online backup mechanism for a running
service. Do not copy a live database and WAL as unrelated files. Keep backups
outside the application server and periodically restore one into a separate
location to test integrity and recovery.

Before upgrading, retain a recoverable backup. Start the new version against a
restorable copy when validating a schema migration. Application rollback does
not roll back an already migrated database. Keep the old executable, its format
version, and the backup together until the upgrade is accepted.

The JSON export is a separate portability mechanism. It preserves stable node
identities and relations so long-term recovery does not depend on the current
web editor or search index. Rebuild derived search indexes from canonical data
when needed.
