"""Opt-in end-to-end checks: the real server process, the real `multica` CLI and a real Multica backend.

Skipped unless MULTICA_MCP_IT_BIN, MULTICA_MCP_IT_API_URL, MULTICA_MCP_IT_PAT and MULTICA_MCP_IT_WORKSPACE are set.
Only read commands are sent to the backend. Optional:
  MULTICA_MCP_IT_OTHER_WORKSPACE  a workspace the PAT can see but the MCP key does not allow
  MULTICA_MCP_IT_OTHER_ISSUE      an issue UUID from that workspace; it must stay unreachable through the allowed one
The MCP Python SDK client check runs when the `mcp` package is importable.
"""
import asyncio
import http.client
import json
import os
import socket
import subprocess
import sys
import tempfile
import time
import unittest
import uuid
from pathlib import Path

from support import PKG

IT = {name: os.environ.get(f'MULTICA_MCP_IT_{name}', '') for name in
      ('BIN', 'API_URL', 'PAT', 'WORKSPACE', 'OTHER_WORKSPACE', 'OTHER_ISSUE')}
KEY = 'it-key-' + 'x' * 40
READER_KEY = 'it-reader-' + 'r' * 40
STRANGER_KEY = 'it-stranger-' + 's' * 40


def free_port():
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0))
        return s.getsockname()[1]


@unittest.skipUnless(all(IT[k] for k in ('BIN', 'API_URL', 'PAT', 'WORKSPACE')), 'set MULTICA_MCP_IT_* to run')
class EndToEnd(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.TemporaryDirectory()
        root = Path(cls.tmp.name)
        clients = root / 'clients.json'
        clients.write_text(json.dumps([
            {'name': 'reader', 'key': READER_KEY, 'token': IT['PAT'], 'workspaces': [IT['WORKSPACE']], 'read_only': True},
            # a workspace id the PAT is not a member of: only the backend can refuse it
            {'name': 'stranger', 'key': STRANGER_KEY, 'token': IT['PAT'], 'workspaces': [str(uuid.uuid4())]},
        ]))
        clients.chmod(0o600)
        cls.port = free_port()
        env = {'PATH': os.environ.get('PATH', '/usr/bin:/bin'), 'MULTICA_BIN': IT['BIN'], 'MULTICA_API_URL': IT['API_URL'],
               'MULTICA_MCP_KEY': KEY, 'MULTICA_PAT': IT['PAT'], 'MULTICA_MCP_WORKSPACES': IT['WORKSPACE'],
               'MULTICA_MCP_CLIENTS': str(clients), 'MULTICA_MCP_STATE_DIR': str(root / 'state'),
               'MULTICA_MCP_PORT': str(cls.port)}
        cls.proc = subprocess.Popen([sys.executable, '-m', 'multica_mcp'], cwd=PKG, env=env,
                                    stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, text=True)
        for _ in range(100):
            try:
                conn = http.client.HTTPConnection('127.0.0.1', cls.port, timeout=2)
                conn.request('GET', '/healthz')
                if conn.getresponse().status == 200:
                    conn.close()
                    return
            except OSError:
                time.sleep(0.05)
        cls.tearDownClass()
        raise RuntimeError('server did not start')

    @classmethod
    def tearDownClass(cls):
        cls.proc.terminate()
        cls.proc.communicate(timeout=10)
        cls.tmp.cleanup()

    def rpc(self, method, params, key=KEY):
        conn = http.client.HTTPConnection('127.0.0.1', self.port, timeout=60)
        try:
            conn.request('POST', '/mcp', json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params}),
                         {'Authorization': f'Bearer {key}', 'Content-Type': 'application/json'})
            resp = conn.getresponse()
            self.assertEqual(resp.status, 200)
            return json.loads(resp.read())
        finally:
            conn.close()

    def run_cli(self, args, key=KEY, **extra):
        body = self.rpc('tools/call', {'name': 'multica', 'arguments': {'args': args, **extra}}, key=key)
        if 'error' in body:
            return None, body['error']
        return body['result']['isError'], json.loads(body['result']['content'][0]['text'])

    def test_reads_in_the_allowed_workspace(self):
        for args in (['version'], ['workspace', 'list'], ['issue', 'list', '--limit', '1'], ['project', 'list']):
            with self.subTest(args=args):
                is_error, out = self.run_cli(args)
                self.assertFalse(is_error, out)
        _, out = self.run_cli(['issue', 'list', '--limit', '1'])
        json.loads(out['stdout'])

    def test_help_comes_from_the_real_cli(self):
        is_error, out = self.run_cli(['issue', 'create', '--help'])
        self.assertFalse(is_error, out)
        self.assertIn('--description-stdin', out['stdout'])

    def test_denied_command_and_flag_never_reach_the_cli(self):
        for args in (['issue', 'comment', 'delete', 'x'], ['agent', 'update', 'x'], ['issue', 'list', '--server-url', 'http://x']):
            with self.subTest(args=args):
                _, err = self.run_cli(args)
                self.assertEqual(err['code'], -32602)

    def test_read_only_key_cannot_create(self):
        _, err = self.run_cli(['issue', 'create', '--title', 'must not exist'], key=READER_KEY)
        self.assertEqual(err['code'], -32602)
        is_error, out = self.run_cli(['issue', 'list', '--limit', '1'], key=READER_KEY)
        self.assertFalse(is_error, out)

    def test_backend_refuses_a_workspace_the_pat_is_not_a_member_of(self):
        is_error, out = self.run_cli(['issue', 'list', '--limit', '1'], key=STRANGER_KEY)
        self.assertTrue(is_error, out)
        self.assertNotEqual(out['exit_code'], 0)

    @unittest.skipUnless(IT['OTHER_WORKSPACE'], 'set MULTICA_MCP_IT_OTHER_WORKSPACE')
    def test_workspace_outside_the_key_is_refused_before_the_cli(self):
        _, err = self.run_cli(['issue', 'list'], workspace=IT['OTHER_WORKSPACE'])
        self.assertEqual(err['code'], -32602)

    @unittest.skipUnless(IT['OTHER_ISSUE'], 'set MULTICA_MCP_IT_OTHER_ISSUE')
    def test_issue_from_another_workspace_is_not_readable_through_the_allowed_one(self):
        is_error, out = self.run_cli(['issue', 'get', IT['OTHER_ISSUE']])
        self.assertTrue(is_error, out)
        self.assertNotIn(IT['OTHER_ISSUE'], out['stdout'])

    def test_official_python_sdk_client(self):
        try:
            from mcp import ClientSession
            from mcp.client import streamable_http
        except ImportError:
            self.skipTest('pip install mcp to run the SDK client check')
        url = f'http://127.0.0.1:{self.port}/mcp'
        headers = {'Authorization': f'Bearer {KEY}'}

        def transport():
            if hasattr(streamable_http, 'streamablehttp_client'):  # mcp 1.x
                return streamable_http.streamablehttp_client(url, headers=headers)
            import httpx2  # mcp 2.x
            return streamable_http.streamable_http_client(url, http_client=httpx2.AsyncClient(headers=headers, timeout=60))

        async def session():
            async with transport() as streams:
                async with ClientSession(streams[0], streams[1]) as client:
                    init = await client.initialize()
                    tools = await client.list_tools()
                    result = await client.call_tool('multica', {'args': ['workspace', 'list']})
                    return init, tools, result

        init, tools, result = asyncio.run(session())
        self.assertEqual(init.serverInfo.name if hasattr(init, 'serverInfo') else init.server_info.name, 'multica')
        self.assertEqual([t.name for t in tools.tools], ['multica'])
        is_error = result.isError if hasattr(result, 'isError') else result.is_error
        self.assertFalse(is_error)
        self.assertEqual(json.loads(result.content[0].text)['exit_code'], 0)


if __name__ == '__main__':
    unittest.main()
