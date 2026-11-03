# NixOS homelab, NetBird and Pocket ID

Yan's deployment target is `maxilasoft-vps`, managed in
[Maxilasoft-dev/maxilasoft-vps](https://github.com/Maxilasoft-dev/maxilasoft-vps).
The selected public domain is `vrac.netbird.bomal.cloud`; NetBird routes to
`http://100.124.243.151:8092`. The infrastructure repository pins an immutable
Vrac revision and owns the actual service configuration and deployment status.

## NixOS service

Import `inputs.vrac.nixosModules.default`, provision the runtime token through
your existing secret manager, and configure:

```nix
services.vrac = {
  enable = true;
  # Same-host proxy: loopback. A remote NetBird peer needs a reachable private
  # address instead, restricted to the intended proxy peer by mesh policy.
  listenAddress = "0.0.0.0:8092";
  tokenFile = "/var/lib/vrac-secrets/token";
  timezone = "Europe/Paris";
  oauthIssuer = "https://auth.netbird.bomal.cloud";
  oauthAudience = "https://vrac.netbird.bomal.cloud/mcp";
  oauthScope = "vrac";
  webOIDCClientID = "<your registered public PKCE client ID>";
  webOIDCProviderName = "Pocket ID";
};
```

The module keeps canonical data in `/var/lib/vrac`, loads the token with a
systemd credential and selects the complete UI package by default. Set
`enableWeb = false` to select the headless package. The UI uses the runtime
token for recovery or the configured Pocket ID sign-in for its own session.
Vrac does not treat NetBird identity headers as an application login. Add
`https://vrac.netbird.bomal.cloud/auth/callback` to the public client's allowed
callbacks, retaining ChatGPT's callback. Both browser and MCP access require
the Vrac API permission. The existing public client can serve both callbacks;
a separate public client with the same owner/API restrictions also works.

## Pocket ID for the ChatGPT MCP client

Use a Pocket ID release supporting API resources/permissions and the chosen
client registration mechanism. Verify the actual instance's discovery
metadata before enabling the connection.

For the selected Pocket ID instance, discovery on 2026-11-02 advertises PKCE
S256 and token endpoint method `none`, but neither a registration endpoint nor
enabled CIMD. Use an explicitly registered public ChatGPT client for this
deployment. The host's pinned Pocket ID 2.18.0 supports API resources. Exact
setup steps are in the infrastructure repository's `docs/vrac.md`.

Create an API whose resource is the exact Vrac HTTPS MCP URL, with a permission
key `vrac`. Grant user-delegated access for the ChatGPT client and restrict
allowed users/groups to the knowledge-base owner. The issued access token
must contain that resource in `aud` and `vrac` in its scope. Pocket ID's
[API resource guide](https://pocket-id.org/docs/guides/apis) describes this setup.

For CIMD, allow the exact ChatGPT metadata-document URL shown by the actual
connection settings in Pocket ID's OIDC configuration. An empty allowlist
blocks CIMD. Pocket ID supports public metadata clients with token endpoint
method `none` and automatic PKCE; ChatGPT must select that compatible method.
Configure the API grant for the intended metadata client(s), and verify the
user/group policy. Verify actual Pocket ID/OpenAI method negotiation: OpenAI
also advertises private_key_jwt, while Pocket ID public CIMD clients require
none. Older Pocket ID metadata parsing may need the manual-client fallback. See [Pocket ID CIMD](https://pocket-id.org/docs/guides/client-id-metadata-documents)
and [OpenAI authentication](https://developers.openai.com/plugins/build/auth).

Package the actual deployed connection with:

```sh
python3 scripts/package-plugin.py \
  --url "$VRAC_PUBLIC_MCP_URL" \
  --oauth-client-mode cimd \
  --oauth-scope vrac \
  --verify --output ./tmp/vrac-plugin.zip
```

If CIMD is incompatible with the installed Pocket ID version, register an
actual public OIDC client using the exact redirect URI shown by ChatGPT,
enable PKCE and grant its user-delegated Vrac API permission. Package with
`--oauth-client-mode provided --client-id "$VRAC_CHATGPT_CLIENT_ID"` instead.
This method needs no client secret in the archive.

Discovery verification uses a valid token from the private environment when
needed. No token is included in the archive. Complete consent in ChatGPT and
test read-only discovery before a disposable capture.

## NetBird reverse proxy

Route the public domain to the Vrac peer and preserve paths and the original
Host. HTTPS must reach both the Vrac MCP resource and Pocket ID from the
ChatGPT service; a mesh-only endpoint cannot be reached by an ordinary remote
ChatGPT connection.

`/mcp` and `/.well-known/oauth-protected-resource` (including its `/mcp`
suffix) must reach Vrac without an interactive proxy login. Vrac validates the
OAuth bearer token itself. If the NetBird service gates the UI with SSO, use
narrow per-target authentication bypasses for those MCP/discovery paths,
preserving their full paths. Service-level IP/country restrictions still
apply and must permit the client. NetBird documents these distinctions in
[reverse-proxy authentication](https://docs.netbird.io/manage/reverse-proxy/authentication).
The browser UI retains its own Vrac session requirement.

Use mesh policy/firewall rules to limit backend access to the intended proxy.
Do not expose the runtime token through proxy custom headers or public paths.
Check `/healthz`, protected-resource metadata, the OAuth flow, MCP discovery,
a disposable capture, restart persistence, and snapshot restoration on the
actual host before switching daily use from Trilium.
