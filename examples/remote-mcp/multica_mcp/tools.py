"""Schema of the single `multica` tool and argument validation. Behavior lives in service.py."""
from . import policy
from .errors import InvalidParams

MAX_ARGS = 200
MAX_ARG_LENGTH = 16000
MAX_STDIN_BYTES = 256 * 1024

DESCRIPTION = (
    'Run the Multica CLI (`multica`) as the owner of this MCP key. '
    'args is argv without the binary name, e.g. ["issue","get","MUL-123"]. Pass long text through stdin with '
    '--content-stdin / --description-stdin; *-file and --attachment flags are rejected. Output is JSON by default '
    '(--output json is appended). Help: ["issue","create","--help"]. Allowed commands: {commands}. '
    'Note: issue create/update with --assignee and issue assign start an agent run; issue comment add wakes the assignee.'
)

TOOLS = {
    'multica': {
        'description': DESCRIPTION.format(commands=', '.join(' '.join(path) for path in policy.COMMANDS)),
        'inputSchema': {
            'type': 'object',
            'properties': {
                'args': {'type': 'array', 'items': {'type': 'string'}, 'minItems': 1, 'maxItems': MAX_ARGS,
                         'description': 'argv after the binary name; passed as-is, no shell.'},
                'stdin': {'type': 'string', 'description': 'Text for --content-stdin / --description-stdin, up to 256 KB.'},
                'workspace': {'type': 'string',
                              'description': 'Workspace slug, name or id. Required when the key allows more than one.'},
            },
            'required': ['args'],
            'additionalProperties': False,
        },
    },
}


def public_list():
    return [{'name': name, 'description': spec['description'], 'inputSchema': spec['inputSchema']}
            for name, spec in TOOLS.items()]


def validate(arguments):
    """Returns (args, stdin, workspace) or raises InvalidParams."""
    if not isinstance(arguments, dict):
        raise InvalidParams('arguments must be an object')
    extra = sorted(set(arguments) - {'args', 'stdin', 'workspace'})
    if extra:
        raise InvalidParams(f'unknown arguments: {", ".join(extra)}')
    args = arguments.get('args')
    if not isinstance(args, list) or not args or len(args) > MAX_ARGS or not all(isinstance(a, str) for a in args):
        raise InvalidParams(f'args must be a non-empty array of up to {MAX_ARGS} strings')
    if any(len(a) > MAX_ARG_LENGTH or '\x00' in a for a in args):
        raise InvalidParams(f'args: an element is longer than {MAX_ARG_LENGTH} characters or contains a NUL byte')
    stdin = arguments.get('stdin', '')
    if not isinstance(stdin, str) or len(stdin.encode()) > MAX_STDIN_BYTES:
        raise InvalidParams('stdin must be a string of up to 256 KB')
    workspace = arguments.get('workspace')
    if workspace is not None and (not isinstance(workspace, str) or not workspace.strip() or len(workspace) > 200):
        raise InvalidParams('workspace must be a non-empty string')
    return args, stdin, workspace.strip() if workspace else None
