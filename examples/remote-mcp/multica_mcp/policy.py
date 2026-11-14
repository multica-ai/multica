"""Which CLI commands the tool may run. Deny by default: only exact command paths listed in COMMANDS are allowed.

A new CLI subcommand stays unreachable through MCP until it is added here on purpose.
"""
from .errors import InvalidParams

READ, WRITE = 'read', 'write'

# Command path (the words before any flag or argument) -> access level.
COMMANDS = {
    ('issue', 'list'): READ,
    ('issue', 'get'): READ,
    ('issue', 'search'): READ,
    ('issue', 'children'): READ,
    ('issue', 'pull-requests'): READ,
    ('issue', 'timeline'): READ,
    ('issue', 'runs'): READ,
    ('issue', 'run-messages'): READ,
    ('issue', 'usage'): READ,
    ('issue', 'comment', 'list'): READ,
    ('issue', 'subscriber', 'list'): READ,
    ('issue', 'label', 'list'): READ,
    ('issue', 'metadata', 'list'): READ,
    ('issue', 'metadata', 'get'): READ,
    ('issue', 'create'): WRITE,
    ('issue', 'update'): WRITE,
    ('issue', 'assign'): WRITE,
    ('issue', 'status'): WRITE,
    ('issue', 'comment', 'add'): WRITE,
    ('issue', 'comment', 'update'): WRITE,
    ('issue', 'comment', 'resolve'): WRITE,
    ('issue', 'comment', 'unresolve'): WRITE,
    ('issue', 'subscriber', 'add'): WRITE,
    ('issue', 'subscriber', 'remove'): WRITE,
    ('issue', 'label', 'add'): WRITE,
    ('issue', 'label', 'remove'): WRITE,
    ('project', 'list'): READ,
    ('project', 'get'): READ,
    ('agent', 'list'): READ,
    ('agent', 'get'): READ,
    ('squad', 'list'): READ,
    ('squad', 'get'): READ,
    ('label', 'list'): READ,
    ('workspace', 'list'): READ,
    ('version',): READ,
}

# Commands that do not act inside a workspace: help, version and the caller's own workspace list.
NO_WORKSPACE = frozenset({(), ('version',), ('workspace', 'list')})

# Global flags that would send the token to another server, read a local profile or pick a workspace past the allowlist.
GLOBAL_FLAGS = frozenset({'--server-url', '--profile', '--workspace-id', '--debug'})
HELP_FLAGS = frozenset({'--help', '-h'})
DENIED = 'not allowed via MCP'


def _deny(reason):
    raise InvalidParams(f'{DENIED}: {reason}', denied=reason)


def _flag_name(token):
    return token.split('=', 1)[0] if token.startswith('--') else token


def _forbidden_flag(name):
    # *-file and --attachment read files on the server host; --allow-external-file lifts the CLI's own guard.
    return name.endswith('-file') or name.startswith('--attach') or name in GLOBAL_FLAGS


def _command_path(argv):
    """Leading words before the first flag: what cobra resolves as the command when flags come last."""
    words = []
    for token in argv:
        if token.startswith('-'):
            break
        words.append(token)
    return words


def _match(argv):
    """Longest COMMANDS entry that is a prefix of argv's leading words."""
    words = tuple(_command_path(argv))
    for path in sorted(COMMANDS, key=len, reverse=True):
        if words[:len(path)] == path:
            return path
    return None


def is_help(argv):
    """Help only when no flag precedes it: in `--title --help` the flag takes it as its value and the command runs."""
    return argv[-1] in HELP_FLAGS and not any(t.startswith('-') for t in argv[:-1])


def _is_group_help(argv):
    """`--help`, `issue --help`, `issue comment --help`: help for a group that contains allowed commands."""
    if not is_help(argv):
        return False
    words = tuple(argv[:-1])
    return any(path[:len(words)] == words for path in COMMANDS)


def check(argv, read_only=False):
    """Returns the matched command path, or raises InvalidParams (JSON-RPC -32602) before anything runs."""
    if not argv:
        _deny('empty command')
    for token in argv:
        if token.startswith('-') and _forbidden_flag(_flag_name(token)):
            _deny(f'flag {_flag_name(token)}')
    path = _match(argv)
    if path is None:
        if _is_group_help(argv):
            return ()
        _deny(f'command {" ".join(_command_path(argv)[:3])!r}')
    if read_only and COMMANDS[path] == WRITE and not is_help(argv):
        _deny(f'command {" ".join(path)!r} writes and this key is read-only')
    return path


def wants_output_flag(argv):
    """Append `--output json` unless this is help, `version`, or the caller chose --output."""
    if is_help(argv) or argv[0] == 'version':
        return False
    return not any(t == '--output' or t.startswith('--output=') for t in argv)


def needs_workspace(path, argv):
    return path not in NO_WORKSPACE and not is_help(argv)
