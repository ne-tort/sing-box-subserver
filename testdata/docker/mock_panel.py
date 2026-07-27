#!/usr/bin/env python3
"""Minimal mock panel: serves static sing-box JSON for agent subscribe tests."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import hashlib
import json
import os

ROOT = os.environ.get("MOCK_CONFIG_DIR", "/configs")


class H(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        print("[mock-panel]", fmt % args)

    def do_GET(self):
        name = self.path.strip("/").split("?")[0] or "a.json"
        if ".." in name or name.startswith("/"):
            self.send_error(400)
            return
        path = os.path.join(ROOT, name)
        if not os.path.isfile(path):
            self.send_error(404, f"missing {name}")
            return
        with open(path, "rb") as f:
            body = f.read()
        etag = '"sha256:' + hashlib.sha256(body).hexdigest() + '"'
        inm = self.headers.get("If-None-Match", "").strip()
        if inm and inm == etag:
            self.send_response(304)
            self.send_header("ETag", etag)
            self.end_headers()
            return
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("ETag", etag)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "8080"))
    print(json.dumps({"serving": ROOT, "port": port}))
    ThreadingHTTPServer(("0.0.0.0", port), H).serve_forever()
