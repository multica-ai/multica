"""Opt-in transport probe against an unmodified Hermes source checkout.

The real stock ACP server and gateway execution owner handle /steer. Only the
loopback model and its terminal-tool barrier are controlled. No account is used.
"""
import asyncio
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import shlex
import signal
import sqlite3
import sys
import threading

ROOT = Path(os.environ["MULTICA_HERMES_SOURCE"]).resolve()
sys.path.insert(0, str(ROOT))

import acp
from acp_adapter.gateway_server import GatewayACPAgent
from tests.gateway.fixtures.local_recovery_probe import child_env, daemon


class Model(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        self.reply(b'{"data":[]}', 'application/json')

    def reply(self, payload, kind):
        self.send_response(200)
        self.send_header('Content-Type', kind)
        self.send_header('Content-Length', str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        primary = bool(body.get('tools'))
        if primary:
            self.server.requests.append(body)
        first = primary and len(self.server.requests) == 1
        if first:
            code = ('from pathlib import Path; import time; '
                    f'p=Path({str(self.server.release)!r}); '
                    'deadline=time.monotonic()+30; '
                    'exec("while not p.exists() and time.monotonic()<deadline: time.sleep(0.01)")')
            command = shlex.quote(sys.executable) + ' -c ' + shlex.quote(code)
            message = {'role': 'assistant', 'content': None, 'tool_calls': [{
                'id': 'native-steer-barrier', 'type': 'function', 'function': {
                    'name': 'terminal', 'arguments': json.dumps({'command': command, 'timeout': 40})}}]}
        elif not primary:
            # A command-safety classifier is not an agent-turn model request.
            message = {'role': 'assistant', 'content': 'controlled fixture command'}
        else:
            marker = os.environ['GUIDANCE_MARKER']
            text = marker if marker in json.dumps(body.get('messages', [])) else 'MISSING_NATIVE_GUIDANCE'
            message = {'role': 'assistant', 'content': text}
        usage = {'prompt_tokens': 10 if first else 17, 'completion_tokens': 3 if first else 5,
                 'total_tokens': 13 if first else 22}
        finish = 'tool_calls' if first else 'stop'
        if body.get('stream'):
            delta = dict(message)
            if first:
                delta['tool_calls'] = [dict(delta['tool_calls'][0], index=0)]
            frame = {'id': 'native-model', 'model': 'controlled-gateway', 'object': 'chat.completion.chunk',
                     'choices': [{'index': 0, 'delta': delta, 'finish_reason': finish}], 'usage': usage}
            self.reply(('data: ' + json.dumps(frame) + '\n\ndata: [DONE]\n\n').encode(), 'text/event-stream')
        else:
            payload = {'id': 'native-model', 'model': 'controlled-gateway',
                       'choices': [{'index': 0, 'message': message, 'finish_reason': finish}], 'usage': usage}
            self.reply(json.dumps(payload).encode(), 'application/json')


class Probe(GatewayACPAgent):
    """Record transport calls and release the tool after the stock command returns.

    super().prompt performs all routing, steering and model execution unchanged.
    """
    def __init__(self, peer, owner_pid, trace):
        super().__init__()
        self.peer, self.owner_pid, self.trace = peer, owner_pid, trace
        self.steers = 0

    async def prompt(self, prompt, session_id, **kwargs):
        control = bool(prompt and getattr(prompt[0], 'text', '').startswith('/steer '))
        if control:
            self.steers += 1
        result = await super().prompt(prompt, session_id, **kwargs)
        if control:
            self.peer.release.touch()
        else:
            with sqlite3.connect(self._home / 'state.db') as db:
                runs = db.execute('SELECT count(*) FROM session_admissions').fetchone()[0]
            self.trace.write_text(json.dumps({
                'kind': 'stock-gateway', 'pid': os.getpid(), 'ownerPid': self.owner_pid,
                'runs': runs, 'steers': self.steers, 'modelCalls': len(self.peer.requests),
                'messages': self.peer.requests[-1].get('messages', []),
            }), encoding='utf-8')
        return result


def main():
    def stop(_signal, _frame):
        raise SystemExit(0)
    signal.signal(signal.SIGTERM, stop)
    home, trace = Path(os.environ['HERMES_HOME']), Path(os.environ['GUIDANCE_TRACE'])
    env = child_env()
    user = home / 'user'
    user.mkdir(parents=True, exist_ok=True)
    peer = ThreadingHTTPServer(('127.0.0.1', 0), Model)
    peer.requests, peer.release = [], home / 'release-tool'
    thread = threading.Thread(target=peer.serve_forever, daemon=True)
    thread.start()
    url = f'http://127.0.0.1:{peer.server_port}/v1'
    (home / 'config.yaml').write_text(json.dumps({
        'gateway': {'multiplex_profiles': False},
        'model': {'provider': 'custom', 'default': 'controlled-gateway', 'base_url': url},
        'platform_toolsets': {'acp': ['terminal']},
        'auxiliary': {'title_generation': {'enabled': False}}, 'model_catalog': {'enabled': False},
        'models_dev': {'url': url.removesuffix('/v1') + '/models-dev-offline'},
    }), encoding='utf-8')
    env.update(HOME=str(user), USERPROFILE=str(user), HERMES_HOME=str(home), PYTHONPATH=str(ROOT),
               OPENAI_API_KEY='loopback-only', OPENAI_BASE_URL=url, PYTHONUNBUFFERED='1',
               HERMES_ACP_SKIP_CONFIGURED_MCP='1', GUIDANCE_MARKER=os.environ['GUIDANCE_MARKER'])
    os.environ.clear()
    os.environ.update(env)
    try:
        with daemon(ROOT, home, env, barrier=False) as (process, _):
            asyncio.run(acp.run_agent(Probe(peer, process.pid, trace), use_unstable_protocol=True))
    finally:
        peer.release.touch()
        peer.shutdown()
        peer.server_close()
        thread.join(timeout=5)


if __name__ == '__main__':
    main()
