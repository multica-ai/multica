"""Configuration from the environment and an owner-only clients file. Invalid settings stop the server at startup."""
import json
import re
import stat
from dataclasses import dataclass
from pathlib import Path

MIN_KEY_LENGTH = 32
DEFAULT_API_URL = 'https://api.multica.ai'
DEFAULT_ORIGINS = ('https://claude.ai', 'https://claude.com')
DEFAULT_HOSTS = ('127.0.0.1', 'localhost')
DEFAULT_CLIENT = 'default'
TRUE = ('1', 'true', 'yes')


class ConfigError(ValueError):
    pass


def _list(value):
    return tuple(x.strip() for x in (value or '').split(',') if x.strip())


@dataclass(frozen=True)
class Client:
    """Who calls the MCP server: a bearer key plus that person's own Multica PAT; every CLI run acts as them."""
    name: str
    key: str
    token: str
    workspaces: tuple
    read_only: bool = False


def load_clients(path, default_workspaces):
    path = Path(path)
    try:
        mode = path.stat().st_mode
    except OSError as e:
        raise ConfigError(f'MULTICA_MCP_CLIENTS: file is not readable ({e.strerror})') from None
    if mode & (stat.S_IRWXG | stat.S_IRWXO):
        raise ConfigError('MULTICA_MCP_CLIENTS: file must be accessible by its owner only (chmod 600)')
    try:
        rows = json.loads(path.read_text())
    except ValueError:
        raise ConfigError('MULTICA_MCP_CLIENTS: invalid JSON') from None
    if not isinstance(rows, list):
        raise ConfigError('MULTICA_MCP_CLIENTS: expected an array of clients')
    clients = []
    for i, row in enumerate(rows):
        if not isinstance(row, dict):
            raise ConfigError(f'MULTICA_MCP_CLIENTS[{i}]: expected an object')
        name, key, token = (row.get(f) for f in ('name', 'key', 'token'))
        if not all(isinstance(v, str) and v.strip() for v in (name, key, token)):
            raise ConfigError(f'MULTICA_MCP_CLIENTS[{i}]: name, key and token must be non-empty strings')
        if len(key) < MIN_KEY_LENGTH:
            raise ConfigError(f'MULTICA_MCP_CLIENTS[{i}]: key is shorter than {MIN_KEY_LENGTH} characters')
        workspaces = row.get('workspaces', list(default_workspaces))
        if not isinstance(workspaces, list) or not workspaces or not all(isinstance(w, str) and w.strip() for w in workspaces):
            raise ConfigError(f'MULTICA_MCP_CLIENTS[{i}]: workspaces must be a non-empty array of strings '
                              '(or set MULTICA_MCP_WORKSPACES as the default)')
        read_only = row.get('read_only', False)
        if not isinstance(read_only, bool):
            raise ConfigError(f'MULTICA_MCP_CLIENTS[{i}]: read_only must be true or false')
        clients.append(Client(name.strip(), key.strip(), token.strip(), tuple(w.strip() for w in workspaces), read_only))
    return clients


@dataclass(frozen=True)
class Config:
    api_url: str
    clients: tuple
    multica_bin: str = 'multica'
    run_dir: str | None = None  # parent of the empty per-run working directories; None means the system TMPDIR
    state_dir: Path = Path('.')
    host: str = '127.0.0.1'
    port: int = 8101
    allowed_origins: tuple = DEFAULT_ORIGINS
    allowed_hosts: tuple = DEFAULT_HOSTS
    max_body: int = 1024 * 1024
    run_timeout: float = 30.0
    max_stdin: int = 256 * 1024
    max_output: int = 64 * 1024
    max_parallel: int = 4
    auth_fail_limit: int = 10
    auth_fail_window: float = 60.0

    @property
    def secrets(self):
        return tuple(s for c in self.clients for s in (c.key, c.token))

    @classmethod
    def from_env(cls, env):
        def need(name):
            value = env.get(name, '').strip()
            if not value:
                raise ConfigError(f'{name} is not set')
            return value

        api_url = (env.get('MULTICA_API_URL', '').strip() or DEFAULT_API_URL).rstrip('/')
        if not re.fullmatch(r'https?://[A-Za-z0-9.-]+(:\d+)?', api_url):
            raise ConfigError('MULTICA_API_URL: expected http(s)://host[:port] without a path')
        default_workspaces = _list(env.get('MULTICA_MCP_WORKSPACES'))
        clients = []
        key = env.get('MULTICA_MCP_KEY', '').strip()
        if key:
            if len(key) < MIN_KEY_LENGTH:
                raise ConfigError(f'MULTICA_MCP_KEY is shorter than {MIN_KEY_LENGTH} characters')
            if not default_workspaces:
                raise ConfigError('MULTICA_MCP_WORKSPACES is empty')
            read_only = env.get('MULTICA_MCP_READ_ONLY', '').strip().lower() in TRUE
            clients.append(Client(DEFAULT_CLIENT, key, need('MULTICA_PAT'), default_workspaces, read_only))
        if env.get('MULTICA_MCP_CLIENTS', '').strip():
            clients += load_clients(env['MULTICA_MCP_CLIENTS'].strip(), default_workspaces)
        if not clients:
            raise ConfigError('no clients: set MULTICA_MCP_KEY + MULTICA_PAT and/or MULTICA_MCP_CLIENTS')
        names = [c.name.lower() for c in clients]
        if len(set(names)) != len(names):
            raise ConfigError('client names must be unique')
        if len({c.key for c in clients}) != len(clients):
            raise ConfigError('client keys must be unique')
        state = env.get('STATE_DIRECTORY', '').split(':')[0] or env.get('MULTICA_MCP_STATE_DIR', '')
        if not state:
            raise ConfigError('set MULTICA_MCP_STATE_DIR (or STATE_DIRECTORY under systemd)')
        port = int(env.get('MULTICA_MCP_PORT', '8101'))
        if not 1 <= port <= 65535:
            raise ConfigError('MULTICA_MCP_PORT is out of range')
        return cls(
            api_url=api_url, clients=tuple(clients), state_dir=Path(state),
            multica_bin=env.get('MULTICA_BIN', 'multica'), run_dir=env.get('MULTICA_MCP_RUN_DIR') or None,
            host=env.get('MULTICA_MCP_HOST', '127.0.0.1'), port=port,
            allowed_origins=_list(env.get('MULTICA_MCP_ALLOWED_ORIGINS')) or DEFAULT_ORIGINS,
            allowed_hosts=_list(env.get('MULTICA_MCP_ALLOWED_HOSTS')) or DEFAULT_HOSTS,
        )
