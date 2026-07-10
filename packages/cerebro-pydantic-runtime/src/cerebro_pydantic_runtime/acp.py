from __future__ import annotations

import abc
import asyncio
import json
import sys
import uuid
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any


JSONRPC_VERSION = "2.0"
RUNTIME_NAME = "pydantic-acp-agent"
RUNTIME_VERSION = "0.1.0"
DEFAULT_MODEL = "tensorx/glm-5.2"


class PydanticAgentRunner(abc.ABC):
    @abc.abstractmethod
    async def run(self, prompt: str) -> str:
        """Run one Pydantic AI turn and return text output."""


@dataclass
class Session:
    session_id: str
    cwd: str
    model: str


class ACPServer:
    def __init__(self, runner: PydanticAgentRunner):
        self.runner = runner
        self.sessions: dict[str, Session] = {}

    async def handle_message(self, raw: str) -> str | None:
        try:
            request = json.loads(raw)
        except json.JSONDecodeError:
            return encode_message(error_response(None, -32700, "parse error"))

        request_id = request.get("id")
        method = request.get("method")
        params = request.get("params") or {}
        if not isinstance(params, dict):
            return encode_message(error_response(request_id, -32602, "params must be an object"))

        if method == "initialize":
            return encode_message(response(request_id, initialize_result()))
        if method == "session/new":
            return encode_message(response(request_id, self.new_session(params)))
        if method == "session/resume":
            return encode_message(response(request_id, self.resume_session(params)))
        if method == "session/set_model":
            return encode_message(self.set_model(request_id, params))
        if method == "session/prompt":
            return await self.prompt(request_id, params)

        return encode_message(error_response(request_id, -32601, f"unknown method: {method}"))

    async def process_line(self, line: str, write_line: Callable[[str], None]) -> None:
        rendered = await self.handle_message(line)
        if not rendered:
            return
        for out in rendered.splitlines():
            write_line(out)

    async def serve(self) -> None:
        loop = asyncio.get_running_loop()
        while True:
            line = await loop.run_in_executor(None, sys.stdin.readline)
            if line == "":
                return
            await self.process_line(line.strip(), lambda item: print(item, flush=True))

    def new_session(self, params: dict[str, Any]) -> dict[str, Any]:
        session_id = "pydantic-" + uuid.uuid4().hex
        model = clean_string(params.get("model")) or DEFAULT_MODEL
        session = Session(
            session_id=session_id,
            cwd=clean_string(params.get("cwd")) or ".",
            model=model,
        )
        self.sessions[session_id] = session
        return session_result(session)

    def resume_session(self, params: dict[str, Any]) -> dict[str, Any]:
        session_id = clean_string(params.get("sessionId")) or "pydantic-" + uuid.uuid4().hex
        model = clean_string(params.get("model")) or DEFAULT_MODEL
        session = self.sessions.get(session_id)
        if session is None:
            session = Session(
                session_id=session_id,
                cwd=clean_string(params.get("cwd")) or ".",
                model=model,
            )
            self.sessions[session_id] = session
        return session_result(session)

    def set_model(self, request_id: Any, params: dict[str, Any]) -> dict[str, Any]:
        session_id = clean_string(params.get("sessionId"))
        session = self.sessions.get(session_id)
        if session is None:
            return error_response(request_id, -32602, "unknown sessionId")
        model = clean_string(params.get("modelId"))
        if not model:
            return error_response(request_id, -32602, "modelId is required")
        self.sessions[session_id] = Session(session.session_id, session.cwd, model)
        return response(request_id, session_result(self.sessions[session_id]))

    async def prompt(self, request_id: Any, params: dict[str, Any]) -> str:
        session_id = clean_string(params.get("sessionId"))
        if session_id not in self.sessions:
            return encode_message(error_response(request_id, -32602, "unknown sessionId"))

        prompt = extract_prompt_text(params.get("prompt"))
        if prompt == "":
            return encode_message(error_response(request_id, -32602, "prompt text is required"))

        try:
            output = await self.runner.run(prompt)
        except Exception as exc:  # noqa: BLE001 - runtime errors must be sent over JSON-RPC
            return encode_message(error_response(request_id, -32000, f"pydantic runner failed: {exc}"))

        messages = []
        if output:
            messages.append(
                notification(
                    "session/update",
                    {
                        "sessionId": session_id,
                        "update": {
                            "sessionUpdate": "agent_message_chunk",
                            "content": {"type": "text", "text": output},
                        },
                    },
                )
            )
        messages.append(
            response(
                request_id,
                {
                    "stopReason": "end_turn",
                    "usage": {"inputTokens": 0, "outputTokens": 0, "totalTokens": 0},
                },
            )
        )
        return "".join(encode_message(message) for message in messages)


def initialize_result() -> dict[str, Any]:
    return {
        "protocolVersion": 1,
        "agentInfo": {"name": RUNTIME_NAME, "version": RUNTIME_VERSION},
        "agentCapabilities": {
            "mcpCapabilities": {"stdio": True, "http": False, "sse": False},
            "loadSession": False,
            "promptCapabilities": {"image": False, "audio": False, "embeddedContext": False},
        },
    }


def session_result(session: Session) -> dict[str, Any]:
    return {
        "sessionId": session.session_id,
        "currentModeId": "code-mode",
        "currentModel": {"modelId": session.model},
        "availableModes": [{"id": "code-mode", "name": "CodeMode"}],
        "availableModels": [{"modelId": session.model}],
    }


def extract_prompt_text(prompt: Any) -> str:
    if isinstance(prompt, str):
        return prompt.strip()
    if not isinstance(prompt, list):
        return ""
    parts: list[str] = []
    for item in prompt:
        if isinstance(item, dict) and item.get("type") == "text":
            text = clean_string(item.get("text"))
            if text:
                parts.append(text)
    return "\n".join(parts).strip()


def clean_string(value: Any) -> str:
    return value.strip() if isinstance(value, str) else ""


def response(request_id: Any, result: Any) -> dict[str, Any]:
    return {"jsonrpc": JSONRPC_VERSION, "id": request_id, "result": result}


def notification(method: str, params: dict[str, Any]) -> dict[str, Any]:
    return {"jsonrpc": JSONRPC_VERSION, "method": method, "params": params}


def error_response(request_id: Any, code: int, message: str) -> dict[str, Any]:
    return {"jsonrpc": JSONRPC_VERSION, "id": request_id, "error": {"code": code, "message": message}}


def encode_message(message: dict[str, Any]) -> str:
    return json.dumps(message, separators=(",", ":")) + "\n"
