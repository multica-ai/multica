import json
import sys
import types
import unittest
from unittest import mock

from cerebro_pydantic_runtime.acp import ACPServer, DEFAULT_MODEL, PydanticAgentRunner
from cerebro_pydantic_runtime.runner import CodeModeRunner, gateway_base_url


class FakeRunner(PydanticAgentRunner):
    def __init__(self, output: str):
        self.output = output
        self.prompts: list[str] = []

    async def run(self, prompt: str) -> str:
        self.prompts.append(prompt)
        return self.output


def decode_lines(data: str) -> list[dict]:
    return [json.loads(line) for line in data.splitlines() if line.strip()]


class ACPServerTest(unittest.IsolatedAsyncioTestCase):
    async def call(self, server: ACPServer, message: dict) -> list[dict]:
        response = await server.handle_message(json.dumps(message))
        self.assertIsNotNone(response)
        return decode_lines(response or "")

    async def test_initialize_advertises_acp_capabilities(self):
        server = ACPServer(FakeRunner("unused"))

        replies = await self.call(
            server,
            {
                "jsonrpc": "2.0",
                "id": 1,
                "method": "initialize",
                "params": {"protocolVersion": 1},
            },
        )

        self.assertEqual(
            replies,
            [
                {
                    "jsonrpc": "2.0",
                    "id": 1,
                    "result": {
                        "protocolVersion": 1,
                        "agentInfo": {
                            "name": "pydantic-acp-agent",
                            "version": "0.1.0",
                        },
                        "agentCapabilities": {
                            "mcpCapabilities": {"stdio": True, "http": False, "sse": False},
                            "loadSession": False,
                            "promptCapabilities": {
                                "image": False,
                                "audio": False,
                                "embeddedContext": False,
                            },
                        },
                    },
                }
            ],
        )

    async def test_session_new_returns_session_id_and_current_model(self):
        server = ACPServer(FakeRunner("unused"))

        replies = await self.call(
            server,
            {
                "jsonrpc": "2.0",
                "id": 2,
                "method": "session/new",
                "params": {"cwd": "/tmp/project", "model": "tensorx/glm-5.2", "mcpServers": []},
            },
        )

        result = replies[0]["result"]
        self.assertTrue(result["sessionId"].startswith("pydantic-"))
        self.assertEqual(result["currentModeId"], "code-mode")
        self.assertEqual(result["currentModel"]["modelId"], "tensorx/glm-5.2")

    async def test_session_new_defaults_to_tensorx_glm_model(self):
        server = ACPServer(FakeRunner("unused"))

        replies = await self.call(
            server,
            {
                "jsonrpc": "2.0",
                "id": 2,
                "method": "session/new",
                "params": {"cwd": "/tmp/project", "mcpServers": []},
            },
        )

        self.assertEqual(replies[0]["result"]["currentModel"]["modelId"], DEFAULT_MODEL)

    async def test_session_prompt_streams_text_update_before_prompt_response(self):
        runner = FakeRunner("Paris is 22C.")
        server = ACPServer(runner)
        new_session = await self.call(
            server,
            {
                "jsonrpc": "2.0",
                "id": 2,
                "method": "session/new",
                "params": {"cwd": "/tmp/project", "mcpServers": []},
            },
        )
        session_id = new_session[0]["result"]["sessionId"]

        replies = await self.call(
            server,
            {
                "jsonrpc": "2.0",
                "id": 3,
                "method": "session/prompt",
                "params": {
                    "sessionId": session_id,
                    "prompt": [{"type": "text", "text": "Weather in Paris?"}],
                },
            },
        )

        self.assertEqual(runner.prompts, ["Weather in Paris?"])
        self.assertEqual(
            replies[0],
            {
                "jsonrpc": "2.0",
                "method": "session/update",
                "params": {
                    "sessionId": session_id,
                    "update": {
                        "sessionUpdate": "agent_message_chunk",
                        "content": {"type": "text", "text": "Paris is 22C."},
                    },
                },
            },
        )
        self.assertEqual(
            replies[1],
            {
                "jsonrpc": "2.0",
                "id": 3,
                "result": {
                    "stopReason": "end_turn",
                    "usage": {"inputTokens": 0, "outputTokens": 0, "totalTokens": 0},
                },
            },
        )

    async def test_unknown_session_returns_json_rpc_error(self):
        server = ACPServer(FakeRunner("unused"))

        replies = await self.call(
            server,
            {
                "jsonrpc": "2.0",
                "id": 4,
                "method": "session/prompt",
                "params": {"sessionId": "missing", "prompt": [{"type": "text", "text": "Hi"}]},
            },
        )

        self.assertEqual(
            replies,
            [
                {
                    "jsonrpc": "2.0",
                    "id": 4,
                    "error": {"code": -32602, "message": "unknown sessionId"},
                }
            ],
        )

    async def test_server_processes_json_lines(self):
        output_lines: list[str] = []
        server = ACPServer(FakeRunner("Hello"))

        await server.process_line(
            json.dumps({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {}}),
            output_lines.append,
        )

        self.assertEqual(decode_lines("\n".join(output_lines))[0]["id"], 1)


class CodeModeRunnerTest(unittest.TestCase):
    def test_gateway_base_url_appends_openai_compatible_path(self):
        self.assertEqual(
            gateway_base_url("https://gateway.firtal.com"),
            "https://gateway.firtal.com/api/ai/proxy/v1",
        )
        self.assertEqual(
            gateway_base_url("https://gateway.firtal.com/api/ai/proxy/v1"),
            "https://gateway.firtal.com/api/ai/proxy/v1",
        )

    def test_runner_requires_gateway_key(self):
        with mock.patch.dict("os.environ", {"FIRTAL_REGISTRY_URL": "https://gateway.firtal.com"}, clear=True):
            runner = CodeModeRunner()
            with self.assertRaisesRegex(RuntimeError, "FIRTAL_REGISTRY_KEY"):
                runner._load_agent()

    def test_runner_builds_pydantic_agent_for_firtal_gateway(self):
        constructed: dict[str, object] = {}

        class FakeAgent:
            def __init__(self, model, capabilities):
                constructed["agent_model"] = model
                constructed["capabilities"] = capabilities

        class FakeOpenAIChatModel:
            def __init__(self, model_name, provider):
                constructed["model_name"] = model_name
                constructed["provider"] = provider

        class FakeOpenAIProvider:
            def __init__(self, base_url, api_key):
                self.base_url = base_url
                self.api_key = api_key

        class FakeCodeMode:
            pass

        fake_pydantic_ai = types.ModuleType("pydantic_ai")
        fake_pydantic_ai.Agent = FakeAgent
        fake_models = types.ModuleType("pydantic_ai.models")
        fake_models_openai = types.ModuleType("pydantic_ai.models.openai")
        fake_models_openai.OpenAIChatModel = FakeOpenAIChatModel
        fake_providers = types.ModuleType("pydantic_ai.providers")
        fake_providers_openai = types.ModuleType("pydantic_ai.providers.openai")
        fake_providers_openai.OpenAIProvider = FakeOpenAIProvider
        fake_harness = types.ModuleType("pydantic_ai_harness")
        fake_harness.CodeMode = FakeCodeMode

        modules = {
            "pydantic_ai": fake_pydantic_ai,
            "pydantic_ai.models": fake_models,
            "pydantic_ai.models.openai": fake_models_openai,
            "pydantic_ai.providers": fake_providers,
            "pydantic_ai.providers.openai": fake_providers_openai,
            "pydantic_ai_harness": fake_harness,
        }

        env = {
            "FIRTAL_REGISTRY_URL": "https://gateway.firtal.com",
            "FIRTAL_REGISTRY_KEY": "secret-key",
        }
        with mock.patch.dict(sys.modules, modules), mock.patch.dict("os.environ", env, clear=True):
            runner = CodeModeRunner()
            agent = runner._load_agent()

        self.assertIsInstance(agent, FakeAgent)
        self.assertEqual(constructed["model_name"], "tensorx/glm-5.2")
        provider = constructed["provider"]
        self.assertEqual(provider.base_url, "https://gateway.firtal.com/api/ai/proxy/v1")
        self.assertEqual(provider.api_key, "secret-key")
        self.assertIs(constructed["agent_model"], constructed["agent_model"])
        self.assertEqual(len(constructed["capabilities"]), 1)


if __name__ == "__main__":
    unittest.main()
