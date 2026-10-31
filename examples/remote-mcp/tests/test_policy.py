import unittest

from support import PKG  # noqa: F401  (puts the package on sys.path)
from multica_mcp import policy
from multica_mcp.errors import InvalidParams

ALLOWED = (
    ['issue', 'list', '--status', 'todo'], ['issue', 'get', 'MUL-1'], ['issue', 'search', 'login bug'],
    ['issue', 'children', 'MUL-1'], ['issue', 'pull-requests', 'MUL-1'], ['issue', 'timeline', 'MUL-1'],
    ['issue', 'runs', 'MUL-1'], ['issue', 'run-messages', 'r'], ['issue', 'usage', 'MUL-1'],
    ['issue', 'comment', 'list', 'MUL-1'], ['issue', 'subscriber', 'list', 'MUL-1'], ['issue', 'label', 'list', 'MUL-1'],
    ['issue', 'metadata', 'list', 'MUL-1'], ['issue', 'metadata', 'get', 'MUL-1', 'k'],
    ['issue', 'create', '--title', 'x', '--description-stdin'], ['issue', 'update', 'MUL-1', '--title', 'y'],
    ['issue', 'assign', 'MUL-1', '--unassign'], ['issue', 'status', 'MUL-1', 'done'],
    ['issue', 'comment', 'add', 'MUL-1', '--content-stdin'], ['issue', 'comment', 'update', 'c', '--content', 'x'],
    ['issue', 'comment', 'resolve', 'c'], ['issue', 'comment', 'unresolve', 'c'],
    ['issue', 'subscriber', 'add', 'MUL-1'], ['issue', 'subscriber', 'remove', 'MUL-1'],
    ['issue', 'label', 'add', 'MUL-1', 'l'], ['issue', 'label', 'remove', 'MUL-1', 'l'],
    ['project', 'list'], ['project', 'get', 'p'], ['agent', 'list'], ['agent', 'get', 'a'], ['squad', 'list'],
    ['squad', 'get', 's'], ['label', 'list'], ['workspace', 'list'], ['version'],
)
DENIED = (
    # issue commands outside the first scope: deletion, execution control, wakeups, ordering
    ['issue', 'comment', 'delete', 'c'], ['issue', 'metadata', 'set', 'MUL-1'], ['issue', 'metadata', 'delete', 'MUL-1'],
    ['issue', 'rerun', 'MUL-1'], ['issue', 'rerun-task', 'r'], ['issue', 'cancel-task', 'r'], ['issue', 'reorder', 'MUL-1'],
    ['issue', 'wakeup', 'list', 'MUL-1'], ['issue', 'steer', 'MUL-1'], ['issue', 'future-subcommand'], ['issue'],
    # flags before the subcommand: cobra would still resolve `issue comment delete`
    ['issue', '--output', 'json', 'comment', 'delete', 'c'], ['issue', '--output=json', 'comment', 'delete', 'c'],
    # other groups
    ['agent', 'create'], ['agent', 'update', 'a'], ['agent', 'archive', 'a'], ['agent', 'skills', 'list', 'a'],
    ['agent', 'env', 'get', 'a'], ['agent', 'tasks', 'a'], ['autopilot', 'list'], ['skill', 'list'], ['runtime', 'list'],
    ['daemon', 'start'], ['auth', 'status'], ['login'], ['setup'], ['config', 'show'], ['update'], ['attachment', 'download', 'x'],
    ['repo', 'checkout', 'x'], ['label', 'create'], ['label', 'delete', 'x'], ['project', 'create'], ['project', 'delete', 'p'],
    ['squad', 'create'], ['squad', 'member', 'add', 's'], ['workspace', 'get', 'other'], ['workspace', 'member', 'list', 'other'],
    ['workspace', 'switch', 'other'], ['workspace', 'mcp', 'list'], ['chat', 'list'], ['property', 'list'],
    [''], ['ISSUE', 'get'], ['version2'], ['issue2', 'get'],
)
FORBIDDEN_FLAGS = (
    ['issue', 'create', '--description-file', 'x'], ['issue', 'create', '--description-file=/etc/passwd'],
    ['issue', 'comment', 'add', 'K', '--content-file', 'x'], ['issue', 'update', 'K', '--allow-external-file'],
    ['issue', 'create', '--attachment', 'x'], ['issue', 'create', '--attachment-id', 'x'], ['issue', 'update', 'K', '--attach=x'],
    ['issue', 'list', '--server-url', 'http://evil'], ['issue', 'list', '--server-url=http://evil'],
    ['--server-url', 'http://evil', 'issue', 'list'], ['issue', 'list', '--workspace-id', 'x'],
    ['issue', 'list', '--workspace-id=x'], ['issue', 'list', '--profile', 'x'], ['issue', 'list', '--debug'],
)
WRITES = (
    ['issue', 'create', '--title', 'x'], ['issue', 'update', 'K', '--title', 'y'], ['issue', 'assign', 'K', '--to', 'a'],
    ['issue', 'status', 'K', 'done'], ['issue', 'comment', 'add', 'K', '--content', 'x'], ['issue', 'label', 'add', 'K', 'l'],
)


class Policy(unittest.TestCase):
    def test_every_allowed_command_passes(self):
        for argv in ALLOWED:
            with self.subTest(argv=argv):
                self.assertIn(policy.check(argv), policy.COMMANDS)

    def test_examples_cover_the_whole_table(self):
        self.assertEqual({policy.check(a) for a in ALLOWED}, set(policy.COMMANDS))

    def test_commands_outside_the_table_are_denied_before_running(self):
        for argv in DENIED:
            with self.subTest(argv=argv), self.assertRaises(InvalidParams) as ctx:
                policy.check(argv)
            self.assertTrue(ctx.exception.message.startswith('not allowed via MCP'))
            self.assertTrue(ctx.exception.denied)

    def test_forbidden_flags_are_denied_even_on_allowed_commands(self):
        for argv in FORBIDDEN_FLAGS:
            with self.subTest(argv=argv), self.assertRaises(InvalidParams):
                policy.check(argv)

    def test_read_only_key_cannot_write_but_can_read_and_ask_for_help(self):
        for argv in WRITES:
            with self.subTest(argv=argv), self.assertRaises(InvalidParams) as ctx:
                policy.check(argv, read_only=True)
            self.assertIn('read-only', ctx.exception.message)
        for argv in (['issue', 'list'], ['issue', 'get', 'K'], ['issue', 'create', '--help'], ['workspace', 'list']):
            with self.subTest(argv=argv):
                policy.check(argv, read_only=True)

    def test_help_on_allowed_commands_and_groups(self):
        for argv in (['--help'], ['-h'], ['issue', '--help'], ['issue', 'comment', '--help'], ['agent', '--help'],
                     ['issue', 'create', '--help'], ['project', 'list', '-h'], ['workspace', '--help']):
            with self.subTest(argv=argv):
                policy.check(argv)
        for argv in (['agent', 'update', '--help'], ['daemon', '--help'], ['--help', 'agent', 'update'],
                     ['--help', '--server-url', 'x'], ['auth', '-h'], ['autopilot', '--help']):
            with self.subTest(argv=argv), self.assertRaises(InvalidParams):
                policy.check(argv)

    def test_empty_argv_is_denied(self):
        with self.assertRaises(InvalidParams):
            policy.check([])

    def test_output_flag_is_added_only_where_needed(self):
        self.assertTrue(policy.wants_output_flag(['issue', 'list']))
        self.assertFalse(policy.wants_output_flag(['issue', 'list', '--help']))
        self.assertFalse(policy.wants_output_flag(['version']))
        self.assertFalse(policy.wants_output_flag(['issue', 'list', '--output', 'table']))
        self.assertFalse(policy.wants_output_flag(['issue', 'list', '--output=table']))
        self.assertFalse(policy.wants_output_flag(['issue', 'create', '-h']))

    def test_help_as_a_flag_value_is_not_help(self):
        # In `--title --help` pflag takes --help as the value and the command runs, so it needs a workspace.
        self.assertFalse(policy.is_help(['issue', 'create', '--title', '--help']))
        self.assertTrue(policy.needs_workspace(('issue', 'create'), ['issue', 'create', '--title', '--help']))
        self.assertTrue(policy.wants_output_flag(['issue', 'create', '--title', '--help']))
        with self.assertRaises(InvalidParams):
            policy.check(['issue', 'create', '--title', '--help'], read_only=True)
        self.assertTrue(policy.is_help(['issue', 'get', 'MUL-1', '--help']))
        self.assertFalse(policy.needs_workspace(('issue', 'create'), ['issue', 'create', '-h']))


if __name__ == '__main__':
    unittest.main()
