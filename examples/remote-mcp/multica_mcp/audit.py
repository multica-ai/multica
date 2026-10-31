"""Audit log: one JSON line per tool call in $STATE_DIRECTORY/audit.jsonl (0600), with every client key and PAT masked."""
import hashlib
import json
import os
import threading
import time

from .redaction import Redactor

HEAD_CHARS = 200


def text_digest(text, redact):
    # Hash and length describe the original; secrets are masked before the preview is cut.
    return {'sha256': hashlib.sha256(text.encode()).hexdigest(), 'head': redact(text)[:HEAD_CHARS], 'length': len(text)}


class AuditLog:
    def __init__(self, path, secrets=(), clock=time.time):
        self.path, self.redact, self._clock = path, Redactor(secrets), clock
        self._lock = threading.Lock()
        fd = os.open(self.path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
        os.close(fd)
        os.chmod(self.path, 0o600)

    def write(self, **fields):
        entry = {'time': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime(self._clock())), **fields}
        line = json.dumps(self.redact(entry), ensure_ascii=False, sort_keys=True)
        with self._lock:
            fd = os.open(self.path, os.O_WRONLY | os.O_APPEND)
            try:
                os.write(fd, (line + '\n').encode())
                os.fsync(fd)
            finally:
                os.close(fd)
