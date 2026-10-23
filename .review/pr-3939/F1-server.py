# Serves SIZE bytes at any path with Content-Length and no checksum headers; logs each request.
import http.server, os, sys
SIZE = int(os.environ.get("SIZE", str(256 << 20)))
CHUNK = bytes(range(256)) * 4096  # 1 MiB
import hashlib
SHA = None
if os.environ.get("ADVERTISE") == "1":
    h = hashlib.sha256(); left = SIZE
    while left > 0:
        n = min(left, len(CHUNK)); h.update(CHUNK[:n]); left -= n
    SHA = h.hexdigest()
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        sys.stderr.write(f"REQ {self.command} {self.path}\n"); sys.stderr.flush()
        self.send_response(200)
        self.send_header("Content-Type", "application/octet-stream")
        self.send_header("Content-Length", str(SIZE))
        if SHA: self.send_header("X-Checksum-Sha256", SHA)
        self.end_headers()
        left = SIZE
        while left > 0:
            n = min(left, len(CHUNK)); self.wfile.write(CHUNK[:n]); left -= n
    def do_HEAD(self):
        sys.stderr.write(f"REQ HEAD {self.path}\n"); sys.stderr.flush()
        self.send_response(200); self.send_header("Content-Length", str(SIZE)); self.end_headers()
    def log_message(self, *a): pass
http.server.ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
