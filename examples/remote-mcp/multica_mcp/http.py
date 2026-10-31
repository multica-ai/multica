"""Transport, the only public entry point. Rejection order: path, Host, Origin, rate limit, key, size, JSON."""
import hmac
import json
import logging
import threading
import time
from collections import defaultdict, deque
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from . import rpc

log = logging.getLogger('multica_mcp.http')
LOOPBACK = ('127.0.0.1', '::1')


class AuthLimiter:
    """Key guessing: after `limit` failures from one address within `window` seconds, answer 429 until the window ends."""

    def __init__(self, limit, window, clock=time.monotonic):
        self.limit, self.window, self.clock = limit, window, clock
        self._fails = defaultdict(deque)
        self._lock = threading.Lock()

    def blocked(self, ip):
        with self._lock:
            fails = self._fails[ip]
            while fails and fails[0] <= self.clock() - self.window:
                fails.popleft()
            return len(fails) >= self.limit

    def fail(self, ip):
        with self._lock:
            self._fails[ip].append(self.clock())


def _host_name(header):
    host = (header or '').strip().lower()
    if host.startswith('['):
        return host[1:host.find(']')] if ']' in host else host
    return host.rsplit(':', 1)[0] if host.count(':') == 1 else host


def make_handler(config, service, limiter):
    keys = [(c, c.key.encode()) for c in config.clients]

    class Handler(BaseHTTPRequestHandler):
        server_version = 'multica-mcp'
        sys_version = ''
        protocol_version = 'HTTP/1.1'
        timeout = 30

        def log_message(self, fmt, *args):  # structured logging happens in send_json
            pass

        def client_ip(self):
            peer = self.client_address[0]
            forwarded = self.headers.get('X-Forwarded-For', '')
            # Behind a reverse proxy on loopback the client is the last X-Forwarded-For entry; other peers cannot set it
            if peer in LOOPBACK and forwarded:
                return forwarded.split(',')[-1].strip()[:64] or peer
            return peer

        def send_json(self, status, body=None, headers=(), close=False):
            data = b'' if body is None else json.dumps(body, ensure_ascii=False).encode()
            self.send_response(status)
            self.send_header('Content-Type', 'application/json; charset=utf-8')
            self.send_header('Content-Length', str(len(data)))
            self.send_header('Cache-Control', 'no-store')
            self.send_header('X-Content-Type-Options', 'nosniff')
            for name, value in headers:
                self.send_header(name, value)
            if close:
                self.send_header('Connection', 'close')
                self.close_connection = True
            self.end_headers()
            if data and self.command != 'HEAD':
                self.wfile.write(data)
            log.info('request', extra={'ctx': {'method': self.command, 'path': self.path.split('?')[0],
                                               'status': status, 'ip': self.client_ip()}})

        def reject(self, status, message, headers=()):
            # The body was not read: close the connection, or its bytes would be parsed as the next request
            self.send_json(status, {'error': message}, headers, close=True)

        def authenticate(self, ip):
            """Client for the bearer key, or None. Compares against every key without an early exit, so timing does not tell which matched."""
            header = self.headers.get('Authorization', '')
            scheme, _, token = header.partition(' ')
            candidate = token.strip().encode()
            found = None
            for client, key in keys:
                if hmac.compare_digest(candidate, key) and scheme.lower() == 'bearer':
                    found = client
            if found is None:
                limiter.fail(ip)
            return found

        def route(self):
            path = self.path.split('?')[0]
            if path == '/healthz' and self.command == 'GET' and self.client_address[0] in LOOPBACK:
                return self.send_json(200, {'status': 'ok'})
            if path != '/mcp':
                return self.reject(404, 'not found')
            if _host_name(self.headers.get('Host')) not in config.allowed_hosts:
                return self.reject(403, 'host not allowed')
            origin = self.headers.get('Origin')
            if origin is not None and origin not in config.allowed_origins:
                return self.reject(403, 'origin not allowed')
            ip = self.client_ip()
            if limiter.blocked(ip):
                return self.reject(429, 'too many failed attempts', [('Retry-After', str(int(config.auth_fail_window)))])
            client = self.authenticate(ip)
            if client is None:
                return self.reject(401, 'unauthorized', [('WWW-Authenticate', 'Bearer')])
            if self.command != 'POST':
                return self.reject(405, 'use POST', [('Allow', 'POST')])
            version = self.headers.get('MCP-Protocol-Version')
            if version is not None and version not in rpc.PROTOCOLS:
                return self.reject(400, 'unsupported MCP protocol version')
            self.post_mcp(ip, client)

        def post_mcp(self, ip, client):
            length = self.headers.get('Content-Length')
            if length is None or not length.isdigit():
                return self.reject(411, 'Content-Length required')
            if int(length) > config.max_body:
                return self.reject(413, 'request body too large')
            try:
                message = json.loads(self.rfile.read(int(length)))
            except ValueError:
                return self.send_json(400, {'jsonrpc': '2.0', 'id': None, 'error': {'code': -32700, 'message': 'parse error'}})
            if isinstance(message, list):
                return self.send_json(400, {'jsonrpc': '2.0', 'id': None,
                                            'error': {'code': -32600, 'message': 'batch requests are not supported'}})
            response = rpc.handle(message, service, ip, client)
            if response is None:
                return self.send_json(202)
            self.send_json(200, response)

        do_GET = do_POST = do_PUT = do_DELETE = do_PATCH = do_HEAD = do_OPTIONS = route

    return Handler


def make_server(config, service, limiter=None):
    limiter = limiter or AuthLimiter(config.auth_fail_limit, config.auth_fail_window)
    server = ThreadingHTTPServer((config.host, config.port), make_handler(config, service, limiter))
    server.daemon_threads = True
    return server
