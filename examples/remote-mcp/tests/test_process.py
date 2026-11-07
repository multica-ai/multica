"""The real `python3 -m multica_mcp` process: startup, health, SIGTERM, and refusal on bad configuration."""
import http.client
import json
import signal
import socket
import subprocess
import sys
import time
import unittest
from pathlib import Path

from support import ALICE_KEY, ALICE_PAT, KEY, PAT, PKG, McpCase, can_listen_tcp


def free_port():
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0))
        return s.getsockname()[1]


@unittest.skipUnless(can_listen_tcp(), 'no TCP in this sandbox')
class Process(McpCase):
    def env(self, **extra):
        clients = Path(self.tmp.name) / 'clients.json'
        clients.write_text(json.dumps([{'name': 'alice', 'key': ALICE_KEY, 'token': ALICE_PAT, 'workspaces': ['acme']}]))
        clients.chmod(0o600)
        return {'PATH': '/usr/bin:/bin', 'MULTICA_MCP_KEY': KEY, 'MULTICA_PAT': PAT, 'MULTICA_API_URL': 'http://127.0.0.1:8080',
                'MULTICA_MCP_WORKSPACES': 'acme', 'MULTICA_MCP_STATE_DIR': str(self.state), 'MULTICA_MCP_PORT': str(self.port),
                'MULTICA_MCP_CLIENTS': str(clients), 'MULTICA_BIN': str(self.cli.bin), **extra}

    def spawn(self, **extra):
        return subprocess.Popen([sys.executable, '-m', 'multica_mcp'], cwd=PKG, env=self.env(**extra),
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

    @staticmethod
    def reap(proc):
        if proc.poll() is None:
            proc.kill()
        proc.communicate()

    def mcp(self, key, method, params=None, status=200):
        conn = http.client.HTTPConnection('127.0.0.1', self.port, timeout=10)
        try:
            conn.request('POST', '/mcp', json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params or {}}),
                         {'Authorization': f'Bearer {key}', 'Host': '127.0.0.1', 'Content-Type': 'application/json'})
            resp = conn.getresponse()
            self.assertEqual(resp.status, status)
            return json.loads(resp.read())
        finally:
            conn.close()

    def get(self, path):
        conn = http.client.HTTPConnection('127.0.0.1', self.port, timeout=2)
        try:
            conn.request('GET', path)
            return conn.getresponse().status
        finally:
            conn.close()

    def test_start_health_and_clean_shutdown(self):
        self.port = free_port()
        proc = self.spawn()
        self.addCleanup(self.reap, proc)
        for _ in range(100):
            try:
                self.assertEqual(self.get('/healthz'), 200)
                break
            except OSError:
                time.sleep(0.05)
        else:
            self.fail('server did not start: ' + proc.stderr.read())
        self.assertEqual([t['name'] for t in self.mcp(KEY, 'tools/list')['result']['tools']], ['multica'])
        call = {'name': 'multica', 'arguments': {'args': ['issue', 'get', 'MUL-1']}}
        result = self.mcp(ALICE_KEY, 'tools/call', call)['result']
        self.assertEqual(json.loads(result['content'][0]['text'])['exit_code'], 0)
        denied = {'name': 'multica', 'arguments': {'args': ['agent', 'update', 'x']}}
        self.assertEqual(self.mcp(ALICE_KEY, 'tools/call', denied)['error']['code'], -32602)
        self.assertEqual(self.mcp(KEY[:-1] + 'z', 'tools/list', status=401), {'error': 'unauthorized'})
        self.assertEqual([c['env']['MULTICA_TOKEN'] for c in self.cli.calls if c['argv'][:2] == ['issue', 'get']], [ALICE_PAT])
        audit = [json.loads(line) for line in (self.state / 'audit.jsonl').read_text().splitlines()]
        self.assertEqual([(line['client'], bool(line['denied'])) for line in audit], [('alice', False), ('alice', True)])
        proc.send_signal(signal.SIGTERM)
        self.assertEqual(proc.wait(timeout=10), 0)
        logs = proc.stderr.read()
        for secret in (KEY, PAT, ALICE_KEY, ALICE_PAT):
            self.assertNotIn(secret, logs)

    def test_bad_configuration_exits_2_without_secrets(self):
        self.port = free_port()
        proc = self.spawn(MULTICA_MCP_KEY='short')
        _, err = proc.communicate(timeout=10)
        self.assertEqual(proc.returncode, 2)
        self.assertIn('MULTICA_MCP_KEY', err)
        self.assertNotIn(PAT, err)


if __name__ == '__main__':
    unittest.main()
