"""JSON-RPC 2.0 for MCP over stateless Streamable HTTP: no sessions, one JSON response per POST."""
import json

from . import tools
from .errors import InvalidParams

PROTOCOLS = ('2025-11-25', '2025-06-18', '2025-03-26', '2024-11-05')
SERVER_INFO = {'name': 'multica', 'version': '0.1.0'}


def _error(msg_id, code, message):
    return {'jsonrpc': '2.0', 'id': msg_id, 'error': {'code': code, 'message': message}}


def _result(msg_id, result):
    return {'jsonrpc': '2.0', 'id': msg_id, 'result': result}


def handle(message, service, client_ip='', client=None):
    """Response dict, or None for notifications."""
    if not isinstance(message, dict) or message.get('jsonrpc') != '2.0' or not isinstance(message.get('method'), str):
        return _error(message.get('id') if isinstance(message, dict) else None, -32600, 'invalid request')
    method, msg_id = message['method'], message.get('id')
    if msg_id is None:
        return None
    params = message.get('params') or {}
    if not isinstance(params, dict):
        return _error(msg_id, -32602, 'params must be an object')
    if method == 'initialize':
        asked = params.get('protocolVersion')
        return _result(msg_id, {'protocolVersion': asked if asked in PROTOCOLS else PROTOCOLS[0],
                                'capabilities': {'tools': {'listChanged': False}}, 'serverInfo': SERVER_INFO})
    if method == 'ping':
        return _result(msg_id, {})
    if method == 'tools/list':
        return _result(msg_id, {'tools': tools.public_list()})
    if method == 'tools/call':
        name = params.get('name')
        if not isinstance(name, str) or not name:
            return _error(msg_id, -32602, 'name must be a non-empty string')
        try:
            is_error, payload = service.call_tool(name, params.get('arguments') or {}, client_ip, client)
        except InvalidParams as e:
            # the message echoes client input, so secrets are masked here too
            return _error(msg_id, -32602, service.redact(e.message))
        # isError follows the service verdict, not the presence of an error key in the payload
        return _result(msg_id, {'content': [{'type': 'text', 'text': json.dumps(payload, ensure_ascii=False)}],
                                'isError': bool(is_error)})
    return _error(msg_id, -32601, f'method not found: {method}')
