import hmac
import json
import os
import subprocess
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from .declare import PROTOCOL
from .registry import ArtifactError, catalog

MAX_BODY_BYTES = 8 * 1024 * 1024
INVOCATION_TIMEOUT = 120


def capacity():
    """Concurrent plugin invocations this worker will accept.

    The platform reads this through /catalog and bounds its own scheduling by it,
    so the limit is declared in exactly one place.
    """
    try:
        value = int(os.environ.get('WORKFLOW_WORKER_CAPACITY', '4'))
    except ValueError:
        raise RuntimeError('WORKFLOW_WORKER_CAPACITY must be an integer') from None
    if not 1 <= value <= 64:
        raise RuntimeError('WORKFLOW_WORKER_CAPACITY must be between 1 and 64')
    return value


CAPACITY = capacity()
SLOTS = threading.BoundedSemaphore(CAPACITY)


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def respond(self, status, value, headers=()):
        data = json.dumps(value).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(data)))
        for name, header_value in headers:
            self.send_header(name, header_value)
        self.end_headers()
        self.wfile.write(data)

    def authorized(self):
        expected = 'Bearer ' + os.environ['WORKFLOW_WORKER_TOKEN']
        return hmac.compare_digest(self.headers.get('Authorization', ''), expected)

    def do_GET(self):
        if self.path == '/healthz':
            return self.respond(200, {'status': 'ready', 'capacity': CAPACITY})
        if self.path == '/catalog':
            if not self.authorized():
                return self.respond(401, {'error': 'unauthorized'})
            try:
                installed = catalog(os.environ['WORKFLOW_PLUGIN_ROOT'], verify_digests=True)
            except (ArtifactError, OSError, ValueError) as error:
                return self.respond(500, {'error': 'catalog_unavailable', 'message': str(error)})
            plugins = [
                {'name': manifest['name'], 'version': manifest['version'], 'digest': manifest['digest']}
                for manifest, _ in installed.values()
            ]
            plugins.sort(key=lambda item: (item['name'], item['version']))
            return self.respond(200, {'protocol': PROTOCOL, 'capacity': CAPACITY, 'plugins': plugins})
        self.respond(404, {'error': 'not_found'})

    def do_POST(self):
        if self.path != '/execute':
            return self.respond(404, {'error': 'not_found'})
        if not self.authorized():
            return self.respond(401, {'error': 'unauthorized'})
        try:
            length = int(self.headers.get('Content-Length', '0'))
        except ValueError:
            return self.respond(400, {'error': 'invalid_length'})
        if not 0 < length <= MAX_BODY_BYTES:
            return self.respond(413, {'error': 'request_too_large'})
        # Refuse immediately when saturated. A queued caller that blocks past the
        # platform's client timeout looks like a failure; a 503 is backpressure.
        if not SLOTS.acquire(blocking=False):
            return self.respond(503, {'error': 'worker_busy', 'capacity': CAPACITY}, [('Retry-After', '1')])
        try:
            self.connection.settimeout(15)
            body = self.rfile.read(length)
            # No platform database credentials or encryption keys enter the worker.
            allowed = ('PATH', 'LANG', 'SSL_CERT_FILE', 'WORKFLOW_PLUGIN_ROOT', 'WORKFLOW_ALLOWED_HOSTS')
            env = {k: v for k, v in os.environ.items() if k in allowed or k.startswith(('OTEL_', 'PLUGIN_SECRET_'))}
            result = subprocess.run([sys.executable, '-m', 'workflow_sdk.invoke'], input=body, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, env=env, timeout=INVOCATION_TIMEOUT)
            if result.returncode != 0 or len(result.stdout) > MAX_BODY_BYTES:
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
    root = os.environ['WORKFLOW_PLUGIN_ROOT']
    installed = catalog(root, verify_digests=True)
    print(f'worker ready: {len(installed)} plugin(s) verified, capacity {CAPACITY}', file=sys.stderr, flush=True)
    server = ThreadingHTTPServer(('0.0.0.0', 8090), Handler)
    server.daemon_threads = True
    server.serve_forever()


if __name__ == '__main__':
    main()
