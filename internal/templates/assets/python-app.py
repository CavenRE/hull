"""Minimal Hull Python app. Serves a page on $PORT with the standard library.
Bring your own framework (add it to requirements.txt) or grow this file.
Run scripts in the container with `hull python <script.py>` and install
packages with `hull pip install <pkg>`. Restart to pick up code changes:
`hull restart`.
"""
import os
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

PORT = int(os.environ.get("PORT", "8000"))


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = f"<h1>Your Python app is live</h1><p>Python {sys.version.split()[0]}</p>"
        if os.environ.get("DATABASE_URL"):
            body += "<p>DATABASE_URL is set, ready to connect.</p>"
        body += "<p>Edit <code>app.py</code>, then <code>hull restart</code>.</p>"
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.end_headers()
        self.wfile.write(body.encode())

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    print(f"listening on :{PORT}", flush=True)
    HTTPServer(("0.0.0.0", PORT), Handler).serve_forever()
