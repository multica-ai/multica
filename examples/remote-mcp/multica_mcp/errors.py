"""Server errors: InvalidParams becomes JSON-RPC -32602, ToolError a tool result with isError."""


class InvalidParams(Exception):
    """The arguments or the command failed validation; no process was started."""

    def __init__(self, message, denied=None):
        super().__init__(message)
        self.message = message
        self.denied = denied  # policy denial reason for the audit log; None for malformed arguments


class ToolError(Exception):
    def __init__(self, code, message):
        super().__init__(message)
        self.code = code
        self.message = message

    def to_dict(self):
        return {'error': {'code': self.code, 'message': self.message}}
