#!/usr/bin/env python3
"""Build the private Vrac plugin for an operator-supplied MCP endpoint."""

import argparse
import json
import os
from pathlib import Path
import sys
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener
import zipfile


SCHEMA = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        # MCP credentials belong to the supplied endpoint, never a redirect target.
        return None


def endpoint(value, allow_http=False):
    parsed = urlsplit(value)
    if parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise ValueError("The endpoint must not contain credentials, query parameters, or a fragment")
    if not parsed.hostname or not parsed.path.rstrip("/").endswith("/mcp"):
        raise ValueError("Supply the actual MCP endpoint, ending in /mcp")
    local = parsed.hostname in ("localhost", "127.0.0.1", "::1")
    if parsed.scheme != "https" and not (allow_http and local and parsed.scheme == "http"):
        raise ValueError("Use HTTPS; --allow-http permits loopback development only")
    return value.rstrip("/")


def response_json(body):
    body = body.replace("\r\n", "\n")
    try:
        return json.loads(body)
    except json.JSONDecodeError:
        for frame in body.split("\n\n"):
            data = "\n".join(line[5:].strip() for line in frame.splitlines() if line.startswith("data:"))
            if data:
                value = json.loads(data)
                if "result" in value or "error" in value:
                    return value
        raise ValueError("The MCP endpoint did not return a JSON-RPC result") from None


def discover(url, token):
    """Only initialize and list tools; never call a mutating tool."""
    headers = {"Content-Type": "application/json", "Accept": "application/json, text/event-stream"}
    if token:
        headers["Authorization"] = "Bearer " + token
    opener = build_opener(NoRedirect())

    def call(message):
        req = Request(url, json.dumps(message).encode(), headers, method="POST")
        with opener.open(req, timeout=20) as res:
            session = res.headers.get("Mcp-Session-Id")
            if session:
                headers["Mcp-Session-Id"] = session
            body = res.read(4 * 1024 * 1024).decode()
        return response_json(body) if body.strip() else None

    init = call({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
        "protocolVersion": "2025-11-25", "capabilities": {},
        "clientInfo": {"name": "vrac-plugin-packager", "version": "0.2.0"}}})
    if not init or "error" in init or "result" not in init:
        raise ValueError("MCP initialization failed")
    headers["MCP-Protocol-Version"] = init["result"]["protocolVersion"]
    call({"jsonrpc": "2.0", "method": "notifications/initialized"})
    result = call({"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}})
    if not result or "error" in result:
        raise ValueError("MCP tool discovery failed")
    tools = result.get("result", {}).get("tools", [])
    if not isinstance(tools, list) or not tools or any(not isinstance(t, dict) or not isinstance(t.get("name"), str) or not isinstance(t.get("inputSchema"), dict) for t in tools):
        raise ValueError("MCP discovery returned no valid tools")
    return len(tools)


def package(source, output, url, client_mode, oauth_scope="vrac", client_id=None):
    manifest = json.loads((source / "plugin.json").read_text())
    if manifest["$schema"] != "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json":
        raise ValueError("Unexpected plugin manifest schema")
    interface = manifest["extensions"]["com.openai"]["interface"]
    if len(interface["shortDescription"]) > 30:
        raise ValueError("Plugin shortDescription exceeds 30 characters")
    if not oauth_scope or any(ord(c) < 33 or ord(c) > 126 or c in ('"', "\\") for c in oauth_scope):
        raise ValueError("OAuth scope must be one nonempty scope token")
    if client_mode not in ("dcr", "cimd", "provided"):
        raise ValueError("Unknown OAuth client mode")
    client = {"mode": client_mode}
    if client_mode == "provided":
        if not isinstance(client_id, str) or not client_id.strip():
            raise ValueError("--client-id is required for provided OAuth client mode")
        client.update({"clientId": client_id, "tokenEndpointAuthMethod": "none"})
    elif client_id:
        raise ValueError("--client-id applies only to provided OAuth client mode")
    scopes = list(dict.fromkeys(("openid", oauth_scope)))
    config = {"$schema": SCHEMA, "mcpServers": {"vrac": {
        "type": "streamable-http", "url": url,
        "extensions": {"com.openai": {"auth": {"type": "oauth", "client": client, "baseScopes": scopes, "defaultScopes": scopes}}}}}}
    files = [source / "plugin.json"] + sorted((source / "skills").rglob("*")) + sorted((source / "assets").rglob("*"))
    output.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for path in files:
            if path.is_symlink():
                raise ValueError("Plugin source must not contain symbolic links")
            if path.is_file():
                name = manifest["name"] + "/" + path.relative_to(source).as_posix()
                archive.writestr(zipfile.ZipInfo(name, (1980, 1, 1, 0, 0, 0)), path.read_bytes())
        archive.writestr(manifest["name"] + "/mcp.json", json.dumps(config, indent=2) + "\n")
    with zipfile.ZipFile(output) as archive:
        json.loads(archive.read(manifest["name"] + "/plugin.json"))
        json.loads(archive.read(manifest["name"] + "/mcp.json"))
        if archive.testzip() is not None:
            raise ValueError("Plugin archive integrity check failed")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", required=True, help="Actual deployed MCP endpoint, ending in /mcp")
    parser.add_argument("--output", required=True, type=Path, help="Destination ZIP outside plugin/")
    parser.add_argument("--oauth-client-mode", choices=("dcr", "cimd", "provided"), default="dcr")
    parser.add_argument("--client-id", help="Actual registered public OAuth client ID; required only for provided mode")
    parser.add_argument("--oauth-scope", default="vrac", help="API permission scope required by Vrac; openid is also requested")
    parser.add_argument("--verify", action="store_true", help="Initialize MCP and inspect tools/list before packaging")
    parser.add_argument("--allow-http", action="store_true", help="Allow loopback HTTP for local verification")
    parser.add_argument("--force", action="store_true", help="Replace an existing output archive")
    args = parser.parse_args()
    source = Path(__file__).resolve().parents[1] / "plugin"
    try:
        url = endpoint(args.url, args.allow_http)
        output = args.output.resolve()
        if output.is_relative_to(source):
            raise ValueError("Write the ZIP outside the plugin source directory")
        if output.exists() and not args.force:
            raise ValueError("Output already exists; choose another path or use --force")
        if args.verify:
            count = discover(url, os.environ.get("VRAC_PLUGIN_VERIFY_TOKEN", ""))
            print(f"Verified MCP initialization and {count} tool definitions")
        package(source, output, url, args.oauth_client_mode, args.oauth_scope, args.client_id)
        print(f"Plugin archive: {output}")
        if not args.verify:
            print("MCP discovery was not verified; validate the deployed connection before installation")
    except (ValueError, KeyError, OSError, HTTPError, URLError) as exc:
        # Network exception strings can include URLs; do not expose credentials or bodies.
        message = str(exc) if isinstance(exc, (ValueError, KeyError)) else type(exc).__name__
        print("Packaging failed: " + message, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
