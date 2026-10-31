import json
import os
import stat
import unittest

from support import ALICE_KEY, ALICE_PAT, KEY, OS_ENV, OTHER_WS, PAT, READER_PAT, WS, McpCase
from multica_mcp.audit import AuditLog
from multica_mcp.config import Client
from multica_mcp.errors import InvalidParams


class Clients(McpCase):
    def test_each_client_runs_with_its_own_token(self):
        self.ok(['issue', 'get', 'MUL-1'])
        self.ok(['issue', 'get', 'MUL-1'], client='alice', workspace='acme')
        calls = [c for c in self.cli.calls if c['argv'][:2] == ['issue', 'get']]
        self.assertEqual([c['env']['MULTICA_TOKEN'] for c in calls], [PAT, ALICE_PAT])

    def test_over_http_the_key_selects_the_token(self):
        self.start_http()
        self.tool(['issue', 'get', 'MUL-1'], key=KEY)
        self.tool(['issue', 'get', 'MUL-1'], key=ALICE_KEY, workspace='acme')
        tokens = [c['env']['MULTICA_TOKEN'] for c in self.cli.calls if c['argv'][:2] == ['issue', 'get']]
        self.assertEqual(tokens, [PAT, ALICE_PAT])
        self.assertEqual([line['client'] for line in self.audit_lines()], ['default', 'alice'])

    def test_process_gets_a_minimal_environment_and_an_empty_cwd(self):
        self.ok(['issue', 'get', 'MUL-1'], stdin='text')
        call = next(c for c in self.cli.calls if c['argv'][:2] == ['issue', 'get'])
        self.assertEqual(set(call['env']) - OS_ENV,
                         {'MULTICA_TOKEN', 'MULTICA_SERVER_URL', 'MULTICA_WORKSPACE_ID', 'HOME', 'PATH'})
        self.assertEqual(call['env']['MULTICA_SERVER_URL'], 'http://127.0.0.1:8080')
        self.assertEqual(call['env']['MULTICA_WORKSPACE_ID'], WS)
        self.assertEqual(call['env']['HOME'], call['cwd'])
        self.assertEqual(call['listing'], [])
        self.assertEqual(call['stdin'], 'text')
        self.assertFalse(os.path.exists(call['cwd']), 'the run directory is removed after the call')

    def test_workspace_is_required_when_the_key_allows_several(self):
        with self.assertRaises(InvalidParams) as ctx:
            self.call(['issue', 'list'], client='alice')
        self.assertIn('workspace', ctx.exception.message)
        self.ok(['issue', 'list'], client='alice', workspace='Toons')
        self.assertEqual(self.cli.calls[-1]['env']['MULTICA_WORKSPACE_ID'], OTHER_WS)

    def test_workspace_outside_the_key_never_starts_a_process(self):
        for ref in ('hidden', 'Hidden', 'aaaaaaaa-0000-4000-8000-000000000003', 'toons', 'nope'):
            with self.subTest(ref=ref), self.assertRaises(InvalidParams):
                self.call(['issue', 'list'], workspace=ref)
        self.assertFalse([c for c in self.cli.calls if c['argv'][:2] == ['issue', 'list']])

    def test_allowed_workspace_uuid_skips_the_lookup(self):
        client = Client('x', 'x' * 40, 'mul_x', (WS,))
        self.service.call_tool('multica', {'args': ['issue', 'list']}, '', client)
        self.assertEqual([c['argv'][:2] for c in self.cli.calls], [['issue', 'list']])
        self.assertEqual(self.cli.calls[0]['env']['MULTICA_WORKSPACE_ID'], WS)

    def test_workspace_list_is_fetched_once_per_client(self):
        self.ok(['issue', 'list'])
        self.ok(['issue', 'list'])
        self.assertEqual(len([c for c in self.cli.calls if c['argv'][:2] == ['workspace', 'list']]), 1)

    def test_help_version_and_workspace_list_need_no_workspace(self):
        for args in (['version'], ['--help'], ['workspace', 'list'], ['issue', 'create', '--help']):
            with self.subTest(args=args):
                self.ok(args, client='alice')
        self.assertTrue(all('MULTICA_WORKSPACE_ID' not in c['env'] for c in self.cli.calls))

    def test_failed_workspace_lookup_is_a_tool_error(self):
        self.cli.behavior['workspaces'] = 'not json'
        self.cli.save()
        is_error, payload = self.call(['issue', 'list'])
        self.assertTrue(is_error)
        self.assertEqual(payload['error']['code'], 'workspace_lookup_failed')

    def test_read_only_client_reads_but_never_writes(self):
        self.ok(['issue', 'list'], client='reader')
        with self.assertRaises(InvalidParams):
            self.call(['issue', 'comment', 'add', 'MUL-1', '--content', 'x'], client='reader')
        self.assertEqual([c['argv'][:2] for c in self.cli.calls if c['env']['MULTICA_TOKEN'] == READER_PAT],
                         [['workspace', 'list'], ['issue', 'list']])
        self.assertIn('read-only', self.audit_lines()[-1]['denied'])


class Execution(McpCase):
    def test_shell_characters_reach_argv_verbatim(self):
        args = ['issue', 'comment', 'add', 'MUL-1', '--content', '; touch pwned #', '$(id)', '`id`', 'a | b', '> out', '*']
        payload = self.ok(args)
        call = self.cli.calls[-1]
        self.assertEqual(call['argv'][:len(args)], args)
        self.assertEqual(json.loads(payload['stdout'])['argv'][:len(args)], args)
        self.assertEqual(call['listing'], [])

    def test_output_json_is_appended_except_for_help_and_version(self):
        self.ok(['issue', 'list'])
        self.assertEqual(self.cli.calls[-1]['argv'], ['issue', 'list', '--output', 'json'])
        self.ok(['issue', 'list', '--help'])
        self.assertEqual(self.cli.calls[-1]['argv'], ['issue', 'list', '--help'])
        self.ok(['version'])
        self.assertEqual(self.cli.calls[-1]['argv'], ['version'])
        self.ok(['issue', 'list', '--output', 'table'])
        self.assertEqual(self.cli.calls[-1]['argv'], ['issue', 'list', '--output', 'table'])

    def test_subcommand_without_output_flag_is_retried_without_it(self):
        self.cli.rule(['issue', 'metadata'], reject_output=True, stdout='ok')
        payload = self.ok(['issue', 'metadata', 'get', 'MUL-1', 'k'])
        self.assertEqual(payload['stdout'], 'ok')
        argvs = [c['argv'] for c in self.cli.calls if c['argv'][:2] == ['issue', 'metadata']]
        self.assertEqual(argvs, [['issue', 'metadata', 'get', 'MUL-1', 'k', '--output', 'json'],
                                 ['issue', 'metadata', 'get', 'MUL-1', 'k']])

    def test_non_zero_exit_is_an_error_with_output(self):
        self.cli.rule(['issue', 'get'], exit=1, stderr='Error: issue not found')
        is_error, payload = self.call(['issue', 'get', 'MUL-404'])
        self.assertTrue(is_error)
        self.assertEqual((payload['exit_code'], payload['stderr']), (1, 'Error: issue not found'))

    def test_success_returns_the_full_result(self):
        self.cli.rule(['issue', 'get'], stdout='{"id": 1}')
        payload = self.ok(['issue', 'get', 'MUL-1'])
        self.assertEqual(payload, {'exit_code': 0, 'stdout': '{"id": 1}', 'stderr': '', 'truncated': False, 'timeout': False})


class Timeout(McpCase):
    run_overrides = {'run_timeout': 0.5}

    def test_timeout_kills_the_process(self):
        self.cli.rule(['issue', 'runs'], sleep=30)
        is_error, payload = self.call(['issue', 'runs', 'MUL-1'])
        self.assertTrue(is_error)
        self.assertEqual((payload['exit_code'], payload['timeout']), (None, True))


class Truncation(McpCase):
    run_overrides = {'max_output': 200}

    def setUp(self):
        super().setUp()
        self.cli.behavior['workspaces'] = [{'id': WS, 'slug': 'acme'}]  # the workspace list is capped too
        self.cli.save()

    def test_long_output_is_truncated_with_a_marker(self):
        self.cli.rule(['issue', 'get'], stdout='x' * 5000)
        payload = self.ok(['issue', 'get', 'MUL-1'])
        self.assertTrue(payload['truncated'])
        self.assertTrue(payload['stdout'].startswith('x' * 200))
        self.assertIn('[truncated', payload['stdout'])
        self.assertLess(len(payload['stdout']), 300)

    def test_secrets_are_masked_on_the_truncation_boundary(self):
        for secret in (PAT, KEY, ALICE_PAT, ALICE_KEY):
            for pad in (200 - 10, 200 - len(secret) // 2, 200 - 1, 200):
                with self.subTest(secret=secret[:12], pad=pad):
                    self.cli.behavior['rules'] = []
                    self.cli.rule(['issue', 'get'], stdout='x' * pad + secret + 'y' * 300, stderr=secret * 2)
                    payload = self.ok(['issue', 'get', 'MUL-1'])
                    for text in (payload['stdout'], payload['stderr']):
                        self.assertNotIn(secret[:12], text)
                        self.assertNotIn(secret[-12:], text)

    def test_client_token_printed_by_the_process_does_not_leak(self):
        self.cli.rule(['issue', 'get'], stdout='token={TOKEN}', stderr='{TOKEN}')
        for name in ('default', 'alice'):
            payload = self.ok(['issue', 'get', 'MUL-1'], client=name, workspace='acme')
            self.assertEqual(payload['stdout'], 'token=[redacted]')
            self.assertEqual(payload['stderr'], '[redacted]')


class Denied(McpCase):
    def test_denial_starts_no_process_and_is_audited(self):
        with self.assertRaises(InvalidParams) as ctx:
            self.call(['agent', 'update', 'a', '--instructions', 'x'])
        self.assertIn('not allowed via MCP', ctx.exception.message)
        self.assertEqual(self.cli.calls, [])
        (line,) = self.audit_lines()
        self.assertEqual((line['client'], line['argv'][:2], line['exit_code']), ('default', ['agent', 'update'], None))
        self.assertTrue(line['denied'])

    def test_malformed_arguments_are_32602(self):
        for arguments in ({}, {'args': 'issue get'}, {'args': []}, {'args': ['issue', 1]}, {'args': [['issue']]}, {'args': ['a\x00b']},
                          {'args': ['issue'], 'stdin': 5}, {'args': ['issue'], 'extra': 1}, {'args': ['issue'], 'workspace': ''},
                          {'args': ['issue', 'x' * 20000]}, {'args': ['issue'], 'stdin': 'x' * (256 * 1024 + 1)}, [], 'x', None):
            with self.subTest(arguments=str(arguments)[:40]), self.assertRaises(InvalidParams):
                self.service.call_tool('multica', arguments, '', self.client())
        self.assertEqual(self.cli.calls, [])

    def test_unknown_tool_is_32602(self):
        for name in ('get_issue', 'create_issue', 'x'):
            with self.subTest(name=name), self.assertRaises(InvalidParams):
                self.service.call_tool(name, {}, '', self.client())


class Audit(McpCase):
    def test_entry_has_client_argv_exit_code_sizes_and_duration(self):
        self.cli.rule(['issue', 'comment'], stdout='done')
        self.ok(['issue', 'comment', 'add', 'MUL-1', '--content-stdin'], stdin='Hello, ' + PAT)
        (line,) = self.audit_lines()
        self.assertEqual(line['client'], 'default')
        self.assertEqual(line['argv'], ['issue', 'comment', 'add', 'MUL-1', '--content-stdin'])
        self.assertEqual((line['exit_code'], line['stdout_length'], line['client_ip']), (0, 4, '203.0.113.9'))
        self.assertIn('duration', line)
        self.assertIsNone(line['denied'])
        self.assertEqual(line['stdin']['length'], len('Hello, ' + PAT))
        self.assertEqual(len(line['stdin']['sha256']), 64)
        self.assertEqual(line['stdin']['head'], 'Hello, [redacted]')

    def test_stdin_preview_is_capped_and_masks_secrets_on_the_boundary(self):
        self.ok(['issue', 'comment', 'add', 'MUL-1', '--content-stdin'], stdin='x' * 190 + ALICE_PAT)
        (line,) = self.audit_lines()
        self.assertEqual(len(line['stdin']['head']), 200)
        self.assertNotIn(ALICE_PAT[:10], json.dumps(line))

    def test_no_client_key_or_pat_reaches_the_audit_log(self):
        self.ok(['issue', 'get', 'MUL-1', '--title', KEY + ALICE_KEY + PAT + ALICE_PAT])
        text = (self.state / 'audit.jsonl').read_text()
        for secret in (KEY, ALICE_KEY, PAT, ALICE_PAT):
            self.assertNotIn(secret, text)

    def test_exit_codes_are_recorded(self):
        self.cli.rule(['issue', 'get'], exit=3)
        self.call(['issue', 'get', 'X'])
        self.assertEqual(self.audit_lines()[0]['exit_code'], 3)

    def test_file_mode_is_0600(self):
        self.ok(['version'])
        self.assertEqual(stat.S_IMODE((self.state / 'audit.jsonl').stat().st_mode), 0o600)

    def test_existing_log_is_appended(self):
        self.ok(['version'])
        AuditLog(self.state / 'audit.jsonl').write(tool='x')
        self.assertEqual(len(self.audit_lines()), 2)


if __name__ == '__main__':
    unittest.main()
