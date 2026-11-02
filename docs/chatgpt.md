# Connect Vrac to ChatGPT

Vrac exposes a remote MCP endpoint at `/mcp`. The optional browser interface and
MCP tools use the same knowledge engine. The private plugin source in `plugin/`
adds a workflow skill for finding project context and maintaining journal notes.

## Authentication

ChatGPT remote plugins require a supported OAuth flow for private data. A static
Vrac bearer token is useful for the browser login and generic MCP clients, but
is not the ChatGPT connection method. Vrac verifies tokens from an external
OAuth/OIDC provider; it does not implement an identity provider.

Configure the service with:

```text
VRAC_OAUTH_ISSUER=<your provider's actual HTTPS issuer>
VRAC_OAUTH_AUDIENCE=<the audience issued for your Vrac MCP resource>
VRAC_OAUTH_SCOPE=vrac
```

Use the corresponding `services.vrac.oauthIssuer`, `oauthAudience`, and
`oauthScope` options on NixOS. The issuer must expose its real discovery metadata
and signing keys. Configure access tokens with the expected audience and scope.
Your provider must support the MCP client's connection flow: authorization code
with PKCE S256 and either dynamic client registration (DCR) or client ID metadata
documents (CIMD), according to the chosen client configuration. The public HTTPS
MCP resource URL and reverse-proxy routing must agree with the configured audience.
Keep access policy limited to the intended knowledge-base owner.

For the Pocket ID and NetBird homelab deployment, follow the concrete provider
and reverse-proxy setup in [Homelab deployment](homelab.md). Use the CIMD plugin
mode for that setup and request both `openid` and the Vrac API permission scope.

Vrac exposes protected-resource metadata for OAuth discovery. Serve those routes
through the same HTTPS proxy as `/mcp`; do not place an interactive proxy login
in front of MCP requests. Browser login remains available through the private
runtime token independently of OAuth.

See the current [OpenAI plugin authentication documentation](https://developers.openai.com/plugins/build/auth)
when configuring the provider. Provider availability and ChatGPT account features
must be verified on the actual deployment.

## Create a private plugin package

The source intentionally contains no invented server URL and no credentials.
After deployment, supply the actual MCP endpoint and the OAuth client mode:

```sh
python3 scripts/package-plugin.py \
  --url "$VRAC_PUBLIC_MCP_URL" \
  --oauth-client-mode cimd \
  --oauth-scope vrac \
  --output ./tmp/vrac-plugin.zip \
  --verify
```

For discovery on an authenticated endpoint, set `VRAC_PLUGIN_VERIFY_TOKEN` in the
process environment to a valid access token. The script uses it only for the
read-only MCP initialization and tool discovery, never writes it to the archive,
and never prints it. Prefer a secret manager or a private interactive shell;
keep credentials out of command arguments and shell history.

The output uses Agent Plugins 1.0: `plugin.json`, `mcp.json`, a workflow skill,
and a local SVG icon. OAuth is declared in the OpenAI extension of the MCP server
configuration. The package requests `openid` and `--oauth-scope` (default `vrac`)
as both base and default scopes. Set the same permission scope in the service and
identity provider. Use `--oauth-client-mode dcr` for a provider/client flow that
uses dynamic registration instead of CIMD. Packaging without `--verify` is possible, but its output
has not demonstrated live MCP connectivity.

When the installed Pocket ID version cannot negotiate ChatGPT's CIMD token
authentication methods, register a public OAuth client manually using the exact
callback shown by ChatGPT. Enable PKCE and grant it the Vrac API permission as
described in [Homelab deployment](homelab.md), then package its actual client ID:

```sh
python3 scripts/package-plugin.py \
  --url "$VRAC_PUBLIC_MCP_URL" \
  --oauth-client-mode provided \
  --client-id "$VRAC_POCKET_CLIENT_ID" \
  --oauth-scope vrac \
  --output ./tmp/vrac-plugin.zip \
  --verify
```

This public-client configuration declares token endpoint authentication `none`;
there is no client secret in the package. The client ID is required only for
`provided` mode. The packaging script does not register or change the provider.

Install the generated archive as a private plugin through the supported Plugin
Creator or Developer Portal flow for your account, then complete OAuth consent.
Confirm that tools are discoverable before making a real capture. No private
plugin is uploaded by this repository or the packaging script. Building the
archive does not deploy Vrac or prove that ChatGPT can reach your server.

## Validate the connection

Start with a read-only request to find existing tags or a known project, read
one returned node, and verify its ancestry and links. Then create a disposable
journal entry with an explicit request ID and confirm it appears in the browser.
Test a revision conflict and a repeated identical request before using the
connection for daily knowledge capture.

The skill asks the assistant to search before creating a subject, use stable
identities, preserve original notes, update a decision's existing node, and
follow pagination when completeness matters. Tool contracts remain authoritative;
the skill does not grant extra permissions or treat stored notes as instructions.
