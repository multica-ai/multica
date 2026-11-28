"""Masks configured secrets in nested data before fields are truncated or JSON-encoded."""
import re


class Redactor:
    def __init__(self, secrets=()):
        # A long secret may contain a short one: replace the longest match in a single pass.
        secrets = sorted({s for s in secrets if s}, key=len, reverse=True)
        self._pattern = re.compile('|'.join(re.escape(s) for s in secrets)) if secrets else None

    def __call__(self, value):
        if isinstance(value, str):
            return self._pattern.sub('[redacted]', value) if self._pattern else value
        if isinstance(value, dict):
            return {self(k): self(v) for k, v in value.items()}
        if isinstance(value, list):
            return [self(v) for v in value]
        return value
