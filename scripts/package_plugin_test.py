import importlib.util
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
from pathlib import Path
import tempfile
import threading
import unittest
from urllib.error import HTTPError
import zipfile


SCRIPT = Path(__file__).with_name("package-plugin.py")
spec = importlib.util.spec_from_file_location("package_plugin", SCRIPT)
packager = importlib.util.module_from_spec(spec)
spec.loader.exec_module(packager)


class PluginPackageTest(unittest.TestCase):
    def test_endpoint_rejects_credentials_and_insecure_remote(self):
        for value in ("https://user:secret@example.test/mcp", "https://example.test/mcp?token=secret", "http://example.test/mcp", "https://example.test/dashboard"):
            with self.subTest(value=value), self.assertRaises(ValueError):
                packager.endpoint(value)
        self.assertEqual(packager.endpoint("http://127.0.0.1:8080/mcp", True), "http://127.0.0.1:8080/mcp")

    def test_archive_contains_only_plugin_components(self):
        source = SCRIPT.resolve().parents[1] / "plugin"
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / "plugin.zip"
            # This reserved example endpoint is test data, never a delivered package.
            packager.package(source, archive, "https://example.test/mcp", "cimd")
            with zipfile.ZipFile(archive) as z:
                self.assertEqual(set(z.namelist()), {"vrac/plugin.json", "vrac/mcp.json", "vrac/skills/vrac/SKILL.md", "vrac/assets/icon.svg"})
                config = json.loads(z.read("vrac/mcp.json"))
                self.assertEqual(config["mcpServers"]["vrac"]["extensions"]["com.openai"]["auth"], {"type": "oauth", "client": {"mode": "cimd"}, "baseScopes": ["openid", "vrac"], "defaultScopes": ["openid", "vrac"]})
                self.assertNotIn("Authorization", z.read("vrac/mcp.json").decode())
                manifest = json.loads(z.read("vrac/plugin.json"))
                self.assertLessEqual(len(manifest["extensions"]["com.openai"]["interface"]["shortDescription"]), 30)
                self.assertTrue(z.read("vrac/skills/vrac/SKILL.md").startswith(b"---\nname: vrac\n"))

    def test_custom_permission_scope(self):
        source = SCRIPT.resolve().parents[1] / "plugin"
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / "plugin.zip"
            packager.package(source, archive, "https://example.test/mcp", "cimd", "notes:write")
            with zipfile.ZipFile(archive) as z:
                auth = json.loads(z.read("vrac/mcp.json"))["mcpServers"]["vrac"]["extensions"]["com.openai"]["auth"]
                self.assertEqual(auth["baseScopes"], ["openid", "notes:write"])
                self.assertEqual(auth["defaultScopes"], ["openid", "notes:write"])
            with self.assertRaises(ValueError):
                packager.package(source, archive, "https://example.test/mcp", "cimd", "two scopes")

    def test_registered_public_client(self):
        source = SCRIPT.resolve().parents[1] / "plugin"
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / "plugin.zip"
            packager.package(source, archive, "https://example.test/mcp", "provided", "vrac", "actual-test-client-id")
            with zipfile.ZipFile(archive) as z:
                auth = json.loads(z.read("vrac/mcp.json"))["mcpServers"]["vrac"]["extensions"]["com.openai"]["auth"]
                self.assertEqual(auth["client"], {"mode": "provided", "clientId": "actual-test-client-id", "tokenEndpointAuthMethod": "none"})
                self.assertEqual(auth["baseScopes"], ["openid", "vrac"])
            for value in (None, "", "   "):
                with self.subTest(client_id=value), self.assertRaises(ValueError):
                    packager.package(source, archive, "https://example.test/mcp", "provided", client_id=value)
            with self.assertRaises(ValueError):
                packager.package(source, archive, "https://example.test/mcp", "cimd", client_id="unused-id")

    def test_json_and_streaming_discovery_results(self):
        payload = {"jsonrpc": "2.0", "id": 2, "result": {"tools": []}}
        self.assertEqual(packager.response_json(json.dumps(payload)), payload)
        self.assertEqual(packager.response_json("event: message\ndata: " + json.dumps(payload) + "\n\n"), payload)
        self.assertEqual(packager.response_json("event: message\r\ndata: " + json.dumps(payload) + "\r\n\r\n"), payload)

    def test_discovery_does_not_forward_credentials_through_redirects(self):
        received = []

        class Target(BaseHTTPRequestHandler):
            def do_POST(self):
                received.append(self.headers.get("Authorization"))
                self.send_response(200)
                self.end_headers()

            def log_message(self, *args):
                pass

        target = HTTPServer(("127.0.0.1", 0), Target)

        class Redirect(BaseHTTPRequestHandler):
            def do_POST(self):
                self.send_response(302)
                self.send_header("Location", f"http://127.0.0.1:{target.server_port}/mcp")
                self.end_headers()

            def log_message(self, *args):
                pass

        redirect = HTTPServer(("127.0.0.1", 0), Redirect)
        servers = (target, redirect)
        threads = [threading.Thread(target=s.serve_forever, daemon=True) for s in servers]
        for thread in threads:
            thread.start()
        try:
            with self.assertRaises(HTTPError):
                packager.discover(f"http://127.0.0.1:{redirect.server_port}/mcp", "test-private-token")
            self.assertEqual(received, [])
        finally:
            for server in servers:
                server.shutdown()
                server.server_close()
            for thread in threads:
                thread.join()


if __name__ == "__main__":
    unittest.main()
