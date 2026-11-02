"""Exercise the real CI readiness command against a transient TCP reset."""

from pathlib import Path
import shlex
import shutil
import socket
import socketserver
import struct
import subprocess
import threading
import unittest


@unittest.skipUnless(shutil.which("curl"), "curl is required for the container readiness check")
class ContainerReadinessTest(unittest.TestCase):
    def test_port_proxy_reset_is_retried_before_health_check_succeeds(self):
        class Handler(socketserver.BaseRequestHandler):
            def handle(self):
                self.server.requests += 1
                if self.server.requests == 1:
                    self.request.setsockopt(socket.SOL_SOCKET, socket.SO_LINGER, struct.pack("ii", 1, 0))
                    self.request.close()
                    return
                self.request.recv(4096)
                self.request.sendall(b'HTTP/1.1 200 OK\r\nContent-Length: 15\r\nConnection: close\r\n\r\n{"status":"ok"}')

        server = socketserver.TCPServer(("127.0.0.1", 0), Handler)
        server.requests = 0
        thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.05}, daemon=True)
        thread.start()
        try:
            workflow = Path(__file__).resolve().parents[1] / ".github/workflows/ci.yml"
            lines = workflow.read_text().splitlines()
            index = next(i for i, line in enumerate(lines) if line.strip().startswith("curl --retry"))
            parts = []
            while True:
                line = lines[index].strip()
                parts.append(line.rstrip("\\").strip())
                if not line.endswith("\\"):
                    break
                index += 1
            command = shlex.split(" ".join(parts))
            command[-1] = f"http://127.0.0.1:{server.server_address[1]}/healthz"
            result = subprocess.run(command, capture_output=True, text=True, timeout=8)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn('"status":"ok"', result.stdout)
            self.assertEqual(server.requests, 2)
        finally:
            server.shutdown()
            server.server_close()
            thread.join()


if __name__ == "__main__":
    unittest.main()
