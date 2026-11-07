"""Runs `multica` without a shell: argv as-is, an empty cwd, a minimal environment, a timeout and an output cap."""
import os
import signal
import subprocess
import tempfile
import threading
import time
from dataclasses import dataclass

from .redaction import Redactor

UNKNOWN_OUTPUT_FLAG = 'unknown flag: --output'


@dataclass
class RunResult:
    exit_code: int | None
    stdout: str
    stderr: str
    truncated: bool
    timeout: bool
    duration: float
    stdout_length: int
    stdin_length: int = 0

    def to_dict(self):
        return {'exit_code': self.exit_code, 'stdout': self.stdout, 'stderr': self.stderr,
                'truncated': self.truncated, 'timeout': self.timeout}


class _Pipe(threading.Thread):
    """Drains the stream but keeps at most limit bytes, so a chatty process cannot grow memory."""

    def __init__(self, stream, limit):
        super().__init__(daemon=True)
        self.stream, self.limit, self.data, self.total = stream, limit, bytearray(), 0

    def run(self):
        while chunk := self.stream.read(8192):
            self.total += len(chunk)
            if len(self.data) < self.limit:
                self.data += chunk[:self.limit - len(self.data)]


class Runner:
    def __init__(self, config, clock=time.monotonic):
        self.config, self.clock = config, clock
        self._slots = threading.BoundedSemaphore(config.max_parallel)
        self._redact = Redactor(config.secrets)

    def run(self, token, argv, stdin='', workspace_id=''):
        with self._slots:
            return self._run(token, argv, stdin, workspace_id)

    def _run(self, token, argv, stdin, workspace_id):
        cfg = self.config
        started = self.clock()
        with tempfile.TemporaryDirectory(prefix='mcp-run-', dir=cfg.run_dir) as cwd:
            env = {'MULTICA_TOKEN': token, 'MULTICA_SERVER_URL': cfg.api_url, 'HOME': cwd,
                   'PATH': os.environ.get('PATH', '/usr/local/bin:/usr/bin:/bin')}
            if workspace_id:
                env['MULTICA_WORKSPACE_ID'] = workspace_id
            result = self._spawn(argv, stdin, env, cwd, started)
            if (result.exit_code not in (0, None) and UNKNOWN_OUTPUT_FLAG in result.stderr
                    and argv[-2:] == ['--output', 'json']):
                # The subcommand has no --output flag; flags are parsed before any action, so retrying is safe
                result = self._spawn(argv[:-2], stdin, env, cwd, started)
        return result

    def _spawn(self, argv, stdin, env, cwd, started):
        cfg = self.config
        # Read a little past the cap so a secret on the cut boundary is masked whole before truncation
        margin = max((len(s) for s in cfg.secrets), default=0)
        limit = cfg.max_output + margin
        proc = subprocess.Popen([cfg.multica_bin, *argv], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, cwd=cwd, env=env, start_new_session=True, close_fds=True)
        out, err = _Pipe(proc.stdout, limit), _Pipe(proc.stderr, limit)
        out.start()
        err.start()
        feeder = threading.Thread(target=self._feed, args=(proc, stdin.encode()), daemon=True)
        feeder.start()
        timeout = False
        try:
            proc.wait(timeout=cfg.run_timeout)
        except subprocess.TimeoutExpired:
            timeout = True
            self._kill(proc)
        out.join(2)
        err.join(2)
        proc.stdout.close()
        proc.stderr.close()
        stdout, cut_out = self._shape(out)
        stderr, cut_err = self._shape(err)
        return RunResult(exit_code=None if timeout else proc.returncode, stdout=stdout, stderr=stderr,
                         truncated=cut_out or cut_err, timeout=timeout, duration=round(self.clock() - started, 3),
                         stdout_length=out.total, stdin_length=len(stdin))

    @staticmethod
    def _feed(proc, data):
        try:
            proc.stdin.write(data)
        except OSError:  # the process does not read stdin or has already exited
            pass
        finally:
            try:
                proc.stdin.close()
            except OSError:
                pass

    @staticmethod
    def _kill(proc):
        try:
            os.killpg(proc.pid, signal.SIGKILL)
        except (ProcessLookupError, PermissionError):
            pass
        proc.wait()

    def _shape(self, pipe):
        """Text after masking, cut to max_output bytes; truncated means there was more output."""
        limit = self.config.max_output
        text = self._redact(bytes(pipe.data).decode('utf-8', errors='replace'))
        raw = text.encode()
        cut = len(raw) > limit or pipe.total > len(pipe.data)
        if len(raw) > limit:
            text = raw[:limit].decode('utf-8', errors='ignore')
        return text + ('\n[truncated: output exceeds %d bytes]' % limit if cut else ''), cut
