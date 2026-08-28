# OmniRoute Provider Design

Date: 2026-08-27
Status: Design for review

## Goal

Add a native `omniroute` provider to Multica Phase 2. The provider sends agent turns to Brad's OmniRoute instance on the Tailscale VM and gives Multica direct control of streaming, MCP tool execution, allowlists, audit events, and session continuity.

The daemon must use the configured remote base URL, such as `OMNIROUTE_BASE_URL`, and must not require a local OmniRoute process on the daemon host.

## Transport

The first implementation uses OmniRoute's OpenAI-compatible Chat Completions endpoint:

```text
POST {baseURL}/chat/completions
GET  {baseURL}/models
```

The base URL is expected to include `/v1`, so the provider appends only the endpoint path. Authentication uses `Authorization: Bearer` with a secret supplied through runtime environment configuration. Credentials are never stored in the database, logs, prompts, tests, or memory layers.

Requests include the selected model, system and user messages, tool definitions, and `stream: true`. The provider sends `X-Session-Id` when a session identifier is available so OmniRoute can preserve affinity across tool turns and resumed executions.

## Execution loop

1. Build the initial message list from the Multica prompt and system prompt.
2. Discover or receive MCP tool definitions from the configured agent MCP sources.
3. Filter tools against the agent's configured allowlist before sending them to OmniRoute.
4. Send a streaming Chat Completions request.
5. Emit text, reasoning, tool-call, status, and usage events through the existing `agent.Session` channels.
6. When the model returns one or more function calls, validate the function name and arguments.
7. Execute each approved call through the MCP client adapter.
8. Append the assistant tool-call message and tool results to the conversation.
9. Repeat until the model returns a final answer, the context is cancelled, the timeout expires, or a safety limit is reached.

The existing Multica tool allowlist remains authoritative. A model cannot expand its own tool access by naming an unavailable or disallowed function.

## MCP boundary

The provider will expose a small internal interface for listing tools and invoking calls. The first adapter will support the MCP configurations already accepted by Multica. OmniRoute's own MCP endpoint remains optional and is not required for the initial provider. Multica's issue, comment, status, and configured business tools stay under Multica's execution and audit controls.

## Configuration

The provider configuration will support:

- Remote base URL, defaulting from `OMNIROUTE_BASE_URL`.
- API key, defaulting from `OMNIROUTE_API_KEY`.
- Agent-selected model, including OmniRoute aliases such as `auto` where returned by `/models`.
- Request timeout and maximum tool-loop turns from existing execution options.
- Optional session affinity identifier derived from the Multica task or resumed session.

The runtime registration and model-listing paths must recognize `omniroute` as a supported provider. Errors must distinguish configuration failures, authentication failures, upstream model failures, malformed tool calls, MCP failures, cancellation, and timeout.

## Streaming and usage

The parser will consume standard Server-Sent Events and handle `[DONE]`, content deltas, tool-call deltas, finish reasons, and the final usage object. Partial tool-call arguments will be buffered by call index until valid JSON can be decoded. Invalid or incomplete calls fail visibly rather than being treated as a successful final answer.

## Failure handling

- A missing URL or key fails before the request is sent.
- A non-2xx response is returned as a sanitized provider error with status and request identifier when available.
- A network disconnect after partial output produces a failed session unless a complete final answer was already received.
- Tool execution errors are returned to the model as tool results and are also emitted as audit events.
- Unknown tools, disallowed tools, invalid arguments, and loop-limit exhaustion fail closed.
- No retry repeats a mutating tool call unless an explicit idempotency policy exists.

## Compatibility scope

Chat Completions is the initial protocol because it directly exposes OpenAI function tools and is the simplest fit for Multica's existing `Session` interface. A later increment may add OmniRoute's `/v1/responses` protocol for models that require Responses-specific semantics. The implementation will not assume every upstream OmniRoute model supports structured tools, so model discovery and execution tests must record tool-calling capability where available.

## Verification

Tests will cover:

- Request construction and secret redaction.
- Model listing and authentication errors.
- SSE text, tool-call, usage, finish, and malformed-frame parsing.
- Multiple tool calls and partial argument chunks.
- Allowlists blocking tools before transmission and during execution.
- MCP success and failure results.
- Cancellation, timeout, loop limits, and upstream disconnects.
- End-to-end execution against the Tailscale VM using a non-mutating tool probe and the selected OmniRoute model.

The live test must not run until the rotated API key is injected into the environment and the VM endpoint returns a successful authenticated `/models` response.
