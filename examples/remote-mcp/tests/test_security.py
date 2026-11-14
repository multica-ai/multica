import inspect
import json
import unittest
from pathlib import Path

from support import ALICE_KEY, ALICE_PAT, KEY, PAT, McpCase
from multica_mcp import http
from multica_mcp.config import DEFAULT_API_URL, Client, Config, ConfigError


class Transport(McpCase):
    def setUp(self):
        super().setUp()
        self.start_http()

    def test_missing_or_wrong_key_is_401(self):
        self.assertEqual(self.rpc('tools/list', key=None)[0], 401)
        self.assertEqual(self.rpc('tools/list', key='x' * 40)[0], 401)
        self.assertEqual(self.rpc('tools/list', key=KEY[:-1])[0], 401)
        status, _, resp = self.http(body={'jsonrpc': '2.0', 'id': 1, 'method': 'ping'}, headers={'Authorization': f'Basic {KEY}'})
        self.assertEqual((status, resp.getheader('WWW-Authenticate')), (401, 'Bearer'))
        self.assertEqual(self.cli.calls, [])

    def test_keys_are_compared_in_constant_time(self):
        self.assertIn('hmac.compare_digest', inspect.getsource(http))

    def test_each_client_key_works_and_a_pat_is_not_a_key(self):
        self.assertEqual(self.rpc('tools/list', key=KEY)[0], 200)
        self.assertEqual(self.rpc('tools/list', key=ALICE_KEY)[0], 200)
        self.assertEqual(self.rpc('tools/list', key=PAT)[0], 401)
        self.assertEqual(self.rpc('tools/list', key=ALICE_KEY + 'x')[0], 401)

    def test_tools_list_has_exactly_one_tool(self):
        status, body = self.rpc('tools/list')
        self.assertEqual(status, 200)
        (tool,) = body['result']['tools']
        self.assertEqual(tool['name'], 'multica')
        self.assertEqual(tool['inputSchema']['required'], ['args'])
        self.assertEqual(set(tool['inputSchema']['properties']), {'args', 'stdin', 'workspace'})
        self.assertIn('issue comment add', tool['description'])

    def test_body_over_limit_is_413(self):
        self.config = Config(**{**self.config.__dict__, 'max_body': 2048})
        self.start_http()
        status, _, _ = self.http(raw=b'{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"' + b'x' * 4000 + b'"}}',
                                 headers={'Authorization': f'Bearer {KEY}', 'Content-Type': 'application/json'})
        self.assertEqual(status, 413)

    def test_default_body_limit_is_one_megabyte(self):
        self.assertEqual(Config(api_url='http://x', clients=()).max_body, 1024 * 1024)

    def test_foreign_origin_is_403(self):
        self.assertEqual(self.rpc('tools/list', headers={'Origin': 'https://evil.example'})[0], 403)
        self.assertEqual(self.rpc('tools/list', key=None, headers={'Origin': 'https://evil.example'})[0], 403)
        self.assertEqual(self.rpc('tools/list', headers={'Origin': 'https://claude.ai'})[0], 200)

    def test_foreign_host_is_403(self):
        self.assertEqual(self.rpc('tools/list', headers={'Host': 'evil.example'})[0], 403)

    def test_get_is_405_not_a_session(self):
        status, _, resp = self.http('GET', headers={'Authorization': f'Bearer {KEY}'})
        self.assertEqual((status, resp.getheader('Allow')), (405, 'POST'))

    def test_healthz_and_unknown_paths(self):
        self.assertEqual(self.http('GET', '/healthz')[0], 200)
        self.assertEqual(self.http('GET', '/healthz', peer='198.51.100.7')[0], 404)
        self.assertEqual(self.http('GET', '/')[0], 404)
        self.assertEqual(self.http('POST', '/mcp/../admin', headers={'Authorization': f'Bearer {KEY}'})[0], 404)

    def test_notification_is_202_without_body(self):
        status, text, _ = self.http(body={'jsonrpc': '2.0', 'method': 'notifications/initialized'},
                                    headers={'Authorization': f'Bearer {KEY}'})
        self.assertEqual((status, text), (202, b''))

    def test_malformed_json_and_batches(self):
        h = {'Authorization': f'Bearer {KEY}'}
        self.assertEqual(self.http(raw=b'{oops', headers=h)[0], 400)
        self.assertEqual(self.http(raw=b'[{"jsonrpc":"2.0","id":1,"method":"ping"}]', headers=h)[0], 400)
        self.assertEqual(self.http(raw=b'', headers={**h, 'Content-Length': '0'})[0], 400)

    def test_key_guessing_hits_429(self):
        codes = [self.rpc('tools/list', key='x' * 40)[0] for _ in range(8)]
        self.assertEqual(codes[:5], [401] * 5)
        self.assertEqual(set(codes[5:]), {429})
        self.assertEqual(self.rpc('tools/list')[0], 429)  # the window is per address, so even a valid key waits

    def test_initialize_ping_and_unknown_method(self):
        for version in ('2025-11-25', '2025-06-18'):
            _, init = self.rpc('initialize', {'protocolVersion': version, 'capabilities': {}})
            self.assertEqual(init['result']['protocolVersion'], version)
        _, init = self.rpc('initialize', {'protocolVersion': '2099-01-01', 'capabilities': {}})
        self.assertEqual(init['result']['protocolVersion'], '2025-11-25')
        self.assertEqual(init['result']['serverInfo']['name'], 'multica')
        self.assertEqual(self.rpc('ping')[1]['result'], {})
        self.assertEqual(self.rpc('resources/list')[1]['error']['code'], -32601)

    def test_unsupported_protocol_header_does_not_run_the_tool(self):
        call = {'name': 'multica', 'arguments': {'args': ['version']}}
        status, _ = self.rpc('tools/call', call, headers={'MCP-Protocol-Version': '2099-01-01'})
        self.assertEqual(status, 400)
        self.assertEqual(self.cli.calls, [])
        for version in ('2025-03-26', '2025-06-18', '2025-11-25'):
            self.assertEqual(self.rpc('ping', headers={'MCP-Protocol-Version': version})[0], 200)

    def test_is_error_follows_exit_code(self):
        good = self.tool(['version'])
        self.assertIs(good['result']['isError'], False)
        self.cli.rule(['issue', 'get'], exit=1, stderr='not found')
        bad = self.tool(['issue', 'get', 'MUL-404'])
        self.assertIs(bad['result']['isError'], True)
        self.assertEqual(json.loads(bad['result']['content'][0]['text'])['exit_code'], 1)

    def test_denied_command_is_json_rpc_32602_without_running(self):
        body = self.tool(['agent', 'update', 'a'])
        self.assertEqual(body['error']['code'], -32602)
        self.assertIn('not allowed via MCP', body['error']['message'])
        self.assertNotIn('result', body)
        self.assertEqual(self.cli.calls, [])

    def test_bad_tool_name_or_call_shape_is_32602(self):
        for name in ([], {}, None, True, 42, ''):
            with self.subTest(name=name):
                status, body = self.rpc('tools/call', {'name': name, 'arguments': {}})
                self.assertEqual((status, body['id'], body['error']['code']), (200, 1, -32602))
        for call in ({'name': 'get_issue', 'arguments': {}}, {'name': 'multica', 'arguments': {'args': 'issue'}},
                     {'name': 'multica'}):
            with self.subTest(call=call):
                self.assertEqual(self.rpc('tools/call', call)[1]['error']['code'], -32602)
        self.assertEqual(self.rpc('ping')[1]['result'], {})
        self.assertEqual(self.cli.calls, [])

    def test_secrets_in_argument_errors_are_masked(self):
        body = self.tool(['issue', 'get', 'x'], workspace=f'{KEY} {ALICE_PAT}')
        text = json.dumps(body, ensure_ascii=False)
        self.assertNotIn(KEY, text)
        self.assertNotIn(ALICE_PAT, text)

    def test_forwarded_client_ip_is_trusted_only_from_loopback(self):
        call = {'name': 'multica', 'arguments': {'args': ['version']}}
        # from a reverse proxy on loopback: the client is the last entry
        self.rpc('tools/call', call, headers={'X-Forwarded-For': '1.2.3.4, 160.79.104.5'})
        self.assertEqual(self.audit_lines()[-1]['client_ip'], '160.79.104.5')
        # any other peer cannot spoof it
        self.rpc('tools/call', call, headers={'X-Forwarded-For': '1.2.3.4'}, peer='198.51.100.7')
        self.assertEqual(self.audit_lines()[-1]['client_ip'], '198.51.100.7')


class Settings(McpCase):
    ENV = {'MULTICA_MCP_KEY': 'k' * 32, 'MULTICA_PAT': 'p', 'MULTICA_API_URL': 'http://127.0.0.1:8080/',
           'MULTICA_MCP_WORKSPACES': 'acme, toons', 'MULTICA_MCP_STATE_DIR': '/var/lib/x'}

    def clients_file(self, rows, mode=0o600):
        path = Path(self.tmp.name) / 'clients.json'
        path.write_text(rows if isinstance(rows, str) else json.dumps(rows))
        path.chmod(mode)
        return str(path)

    def good_row(self, **extra):
        return {'name': 'alice', 'key': 'i' * 32, 'token': 'mul_i', **extra}

    def test_full_default_configuration_has_one_client(self):
        c = Config.from_env(self.ENV)
        self.assertEqual((c.api_url, c.port, str(c.state_dir)), ('http://127.0.0.1:8080', 8101, '/var/lib/x'))
        self.assertEqual(c.clients, (Client('default', 'k' * 32, 'p', ('acme', 'toons')),))
        self.assertEqual((c.host, c.allowed_hosts, c.multica_bin), ('127.0.0.1', ('127.0.0.1', 'localhost'), 'multica'))

    def test_api_url_defaults_to_multica_cloud_and_read_only_flag_applies(self):
        env = {k: v for k, v in self.ENV.items() if k != 'MULTICA_API_URL'}
        c = Config.from_env({**env, 'MULTICA_MCP_READ_ONLY': 'true'})
        self.assertEqual(c.api_url, DEFAULT_API_URL)
        self.assertTrue(c.clients[0].read_only)

    def test_systemd_state_directory_wins(self):
        c = Config.from_env({**self.ENV, 'STATE_DIRECTORY': '/var/lib/a:/var/lib/b'})
        self.assertEqual(str(c.state_dir), '/var/lib/a')

    def test_clients_file_adds_to_the_default_client(self):
        path = self.clients_file([self.good_row(workspaces=['acme'], read_only=True), self.good_row(name='bob', key='b' * 32)])
        c = Config.from_env({**self.ENV, 'MULTICA_MCP_CLIENTS': path})
        self.assertEqual([x.name for x in c.clients], ['default', 'alice', 'bob'])
        self.assertEqual((c.clients[1].workspaces, c.clients[1].read_only), (('acme',), True))
        self.assertEqual((c.clients[2].workspaces, c.clients[2].read_only), (('acme', 'toons'), False))
        self.assertEqual(set(c.secrets), {'k' * 32, 'p', 'i' * 32, 'mul_i', 'b' * 32})

    def test_clients_file_alone_without_default_key(self):
        env = {k: v for k, v in self.ENV.items() if k not in ('MULTICA_MCP_KEY', 'MULTICA_PAT')}
        c = Config.from_env({**env, 'MULTICA_MCP_CLIENTS': self.clients_file([self.good_row()])})
        self.assertEqual([x.name for x in c.clients], ['alice'])

    def test_bad_settings_stop_startup(self):
        bad = {'no clients': {'MULTICA_MCP_KEY': '', 'MULTICA_PAT': ''}, 'short key': {'MULTICA_MCP_KEY': 'k' * 31},
               'no PAT': {'MULTICA_PAT': ''}, 'no workspaces': {'MULTICA_MCP_WORKSPACES': ' , '},
               'url with path': {'MULTICA_API_URL': 'http://x/api'}, 'no state dir': {'MULTICA_MCP_STATE_DIR': ''},
               'port out of range': {'MULTICA_MCP_PORT': '70000'}}
        for name, patch in bad.items():
            with self.subTest(name), self.assertRaises(ConfigError):
                Config.from_env({**self.ENV, **patch})

    def test_bad_clients_file_stops_startup(self):
        cases = {
            'missing': '/nonexistent/clients.json', 'not json': self.clients_file('{oops'),
            'not an array': self.clients_file({'a': 1}), 'string row': self.clients_file(['x']),
            'no token': self.clients_file([{'name': 'a', 'key': 'a' * 32}]),
            'short key': self.clients_file([self.good_row(key='short')]),
            'empty workspaces': self.clients_file([self.good_row(workspaces=[])]),
            'non-string workspaces': self.clients_file([self.good_row(workspaces=[1])]),
            'read_only not bool': self.clients_file([self.good_row(read_only='yes')]),
            'duplicate name': self.clients_file([self.good_row(), self.good_row(key='b' * 32)]),
            'duplicate key': self.clients_file([self.good_row(), self.good_row(name='bob')]),
            'same key as default': self.clients_file([self.good_row(key='k' * 32)]),
            'group readable': self.clients_file([self.good_row()], mode=0o640),
            'world readable': self.clients_file([self.good_row()], mode=0o644),
        }
        for name, path in cases.items():
            with self.subTest(name), self.assertRaises(ConfigError):
                Config.from_env({**self.ENV, 'MULTICA_MCP_CLIENTS': path})

    def test_config_errors_do_not_contain_secrets(self):
        path = self.clients_file([self.good_row(key='short', token='mul_TOPSECRET')])
        with self.assertRaises(ConfigError) as ctx:
            Config.from_env({**self.ENV, 'MULTICA_MCP_CLIENTS': path})
        self.assertNotIn('TOPSECRET', str(ctx.exception))


if __name__ == '__main__':
    unittest.main()
