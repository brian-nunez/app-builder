import hmac
import json
import os
import subprocess
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from .registry import PROTOCOL, catalog

SLOTS = threading.BoundedSemaphore(4)


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def respond(self, status, value):
        data = json.dumps(value).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path == '/healthz':
            self.respond(200, {'status': 'ready'})
        else:
            self.respond(404, {'error': 'not_found'})

    def do_POST(self):
        if self.path != '/execute':
            return self.respond(404, {'error': 'not_found'})
        if not hmac.compare_digest(self.headers.get('Authorization', ''), 'Bearer ' + os.environ['WORKFLOW_WORKER_TOKEN']):
            return self.respond(401, {'error': 'unauthorized'})
        try:
            length = int(self.headers.get('Content-Length', '0'))
        except ValueError:
            return self.respond(400, {'error': 'invalid_length'})
        if not 0 < length <= 8 * 1024 * 1024:
            return self.respond(413, {'error': 'request_too_large'})
        if not SLOTS.acquire(blocking=True, timeout=120):
            return self.respond(503, {'error': 'worker_busy'})
        try:
            self.connection.settimeout(15)
            body = self.rfile.read(length)
            # No platform database credentials or encryption keys enter the worker.
            allowed = ('PATH', 'LANG', 'SSL_CERT_FILE', 'WORKFLOW_PLUGIN_ROOT', 'WORKFLOW_ALLOWED_HOSTS')
            env = {k: v for k, v in os.environ.items() if k in allowed or k.startswith(('OTEL_', 'PLUGIN_SECRET_'))}
            result = subprocess.run([sys.executable, '-m', 'workflow_sdk.invoke'], input=body, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, env=env, timeout=120)
            if result.returncode != 0 or len(result.stdout) > 8 * 1024 * 1024:
                return self.respond(502, {'error': 'worker_failed'})
            self.respond(200, json.loads(result.stdout))
        except subprocess.TimeoutExpired:
            self.respond(200, {'protocol': PROTOCOL, 'outputs': {}, 'error': {'code': 'execution_timeout', 'message': 'Execution timed out', 'retryable': True}})
        except (ValueError, OSError):
            self.respond(400, {'error': 'invalid_invocation'})
        finally:
            SLOTS.release()


def main():
    if len(os.environ.get('WORKFLOW_WORKER_TOKEN', '')) < 32:
        raise RuntimeError('WORKFLOW_WORKER_TOKEN must have at least 32 characters')
    catalog(os.environ['WORKFLOW_PLUGIN_ROOT'])
    server = ThreadingHTTPServer(('0.0.0.0', 8090), Handler)
    server.daemon_threads = True
    server.serve_forever()


if __name__ == '__main__':
    main()
