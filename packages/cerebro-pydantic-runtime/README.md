# Cerebro Pydantic Runtime

ACP wrapper for running a Pydantic AI `CodeMode` agent through a Multica custom runtime profile.

## How it works

Multica already supports custom runtime profiles when the executable speaks an existing runtime protocol. This package exposes a `pydantic-acp-agent acp` command that speaks ACP over stdin/stdout, so a profile can use:

- `protocol_family`: `hermes`
- `command_name`: `pydantic-acp-agent`

Inside `session/prompt`, the wrapper runs a Pydantic AI agent with `CodeMode()` against the Firtal AI Gateway. The default model is `tensorx/glm-5.2`.

## Install

From this directory:

```bash
uv tool install --editable .
```

Or for local development:

```bash
uv run pydantic-acp-agent acp
```

Set the Firtal AI Gateway credentials on the host that runs the daemon:

```bash
export FIRTAL_REGISTRY_URL="https://<firtal-ai-gateway-host>"
export FIRTAL_REGISTRY_KEY="<gateway-key>"
```

The wrapper converts `FIRTAL_REGISTRY_URL` to the OpenAI-compatible gateway path `/api/ai/proxy/v1`.

The default model is `tensorx/glm-5.2`. Override it only if needed:

```bash
export FIRTAL_REGISTRY_MODEL="tensorx/glm-5.2"
```

or:

```bash
pydantic-acp-agent acp --model tensorx/glm-5.2
```

## Multica setup

Create a custom runtime profile:

- `display_name`: `Pydantic CodeMode`
- `protocol_family`: `hermes`
- `command_name`: `pydantic-acp-agent`
- `fixed_args`: empty

The daemon launches Hermes-compatible profiles as `<command_name> acp`, which matches this package's CLI.

## Limitations

- This is a compatibility wrapper, not a native Multica provider.
- Pydantic CodeMode tools run inside Pydantic's Monty sandbox. They do not automatically become Multica MCP tools.
- Gateway secrets are read from environment variables at runtime. Do not put keys in `fixed_args`, source code, logs, or README examples.
- Token usage is reported as zero until the wrapper maps Pydantic usage data into ACP usage fields.
- `session/resume` keeps the ACP session id, but the wrapper does not persist Pydantic conversation state across process restarts.

## Tests

```bash
PYTHONPATH=src python3 -m unittest discover -s tests -p 'test_*.py' -v
```
