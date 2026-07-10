from __future__ import annotations

import inspect
import os
from typing import Any

from .acp import DEFAULT_MODEL, PydanticAgentRunner


class CodeModeRunner(PydanticAgentRunner):
    def __init__(self, model: str | None = None):
        self.model = model or os.environ.get("FIRTAL_REGISTRY_MODEL") or os.environ.get("PYDANTIC_AI_MODEL") or DEFAULT_MODEL
        self._agent: Any | None = None

    async def run(self, prompt: str) -> str:
        agent = self._load_agent()
        result = await maybe_await(agent.run(prompt))
        return extract_output(result)

    def _load_agent(self) -> Any:
        if self._agent is not None:
            return self._agent
        base_url = gateway_base_url(first_env("FIRTAL_REGISTRY_URL", "FIRTAL_AI_GATEWAY_URL"))
        api_key = first_env("FIRTAL_REGISTRY_KEY", "FIRTAL_AI_GATEWAY_KEY")
        if not base_url:
            raise RuntimeError("FIRTAL_REGISTRY_URL is required for the Firtal AI Gateway")
        if not api_key:
            raise RuntimeError("FIRTAL_REGISTRY_KEY is required for the Firtal AI Gateway")

        try:
            from pydantic_ai import Agent
            from pydantic_ai.models.openai import OpenAIChatModel
            from pydantic_ai.providers.openai import OpenAIProvider
            from pydantic_ai_harness import CodeMode
        except ImportError as exc:
            raise RuntimeError(
                "pydantic-ai-harness[code-mode] is required. Install this package with the code-mode extra."
            ) from exc

        model = OpenAIChatModel(
            self.model,
            provider=OpenAIProvider(base_url=base_url, api_key=api_key),
        )
        self._agent = Agent(model, capabilities=[CodeMode()])
        return self._agent


def gateway_base_url(raw: str) -> str:
    base = raw.strip().rstrip("/")
    if base == "":
        return ""
    suffix = "/api/ai/proxy/v1"
    if base.endswith(suffix):
        return base
    return base + suffix


def first_env(*names: str) -> str:
    for name in names:
        value = os.environ.get(name, "").strip()
        if value:
            return value
    return ""


async def maybe_await(value: Any) -> Any:
    if inspect.isawaitable(value):
        return await value
    return value


def extract_output(result: Any) -> str:
    for attr in ("output", "data"):
        value = getattr(result, attr, None)
        if value is not None:
            return str(value)
    return str(result)
