# Provider Runtime Support Design

Date: 2026-08-28  
Status: Approved architecture direction, implementation in progress

## Goal

Make Multica a provider-neutral runtime host for the requested subscription,
hosted API, and local-model runtimes. Existing native CLI backends remain the
execution path for subscription products. API-key and local products use a
first-class OpenAI-compatible HTTP adapter with the same Multica task
lifecycle, streaming, usage, cancellation, model discovery, and capability
reporting contracts.

## Provider inventory

The runtime catalog exposes these provider identities:

| Provider identity | Credential or transport | Execution family |
| --- | --- | --- |
| `codex` | ChatGPT subscription through Codex CLI | Native subscription CLI |
| `claude` | Claude subscription through Claude Code | Native subscription CLI |
| `antigravity` | Google Antigravity subscription through `agy` | Native subscription CLI |
| `cursor` | Cursor subscription through `cursor-agent` | Native subscription CLI |
| `grok` | Grok subscription through Grok Build ACP | Native subscription CLI |
| `opencode` | OpenCode CLI with its configured account or key | Native CLI |
| `opencode-api` | OpenCode Console API key and endpoint | OpenAI Chat Completions adapter |
| `opencode-zen` | OpenCode Zen API key and endpoint | OpenCode HTTP adapter with model-specific routes |
| `opencode-go` | OpenCode Go API key and endpoint | OpenCode HTTP adapter with model-specific routes |
| `openrouter` | OpenRouter API key | OpenAI-compatible API adapter |
| `vercel-ai-gateway` | Vercel AI Gateway API key | OpenAI-compatible API adapter |
| `ollama` | Local Ollama endpoint, no key by default | OpenAI-compatible API adapter |
| `lmstudio` | Local LM Studio endpoint, no key by default | OpenAI-compatible API adapter |
| `nvidia-nim` | NVIDIA NIM API key and endpoint | OpenAI-compatible API adapter |

The catalog must not describe a provider as available merely because a name is
configured. Availability requires a validated endpoint or executable and a
successful bounded health or version probe.

## Architecture

### Shared provider descriptor

Add a provider descriptor registry in `server/pkg/agent`. Each descriptor owns
the stable provider ID, display name, execution family, default endpoint,
credential environment name, model-discovery path, supported capabilities,
health probe, and safe redaction keys. The daemon, API responses, runtime
picker, and docs consume this registry instead of maintaining independent
provider lists.

The existing CLI providers are descriptors backed by their current native
implementations. The new API and local providers are descriptors backed by two
adapter layers:

- OpenAI-compatible chat streaming for OpenRouter, Vercel AI Gateway, Ollama,
  LM Studio, and NVIDIA NIM.
- OpenCode HTTP routing for Console, Zen, and Go. Console uses Chat
  Completions. Zen and Go select Chat Completions, Responses, or Anthropic
  Messages per documented model route. A model whose route is not supported is
  omitted from discovery and rejected during execution.

### API execution contract

The HTTP backend implements `agent.Backend` and emits the existing unified
message types. It must:

1. Resolve a provider descriptor and endpoint from daemon-owned configuration.
2. Reject unsupported or malformed endpoint URLs before making a request.
3. Send a bounded request with the selected model, prompt, system context, and
   verified capability-gated tool fields.
4. Parse only valid SSE data frames and require the protocol's terminal
   completion marker.
5. Stream text, reasoning when present, tool calls when verified, and usage.
6. Accumulate usage across every model turn.
7. Honor context cancellation and close the response body promptly.
8. Redact configured keys, authorization values, and provider secrets from
   errors, logs, and result text.
9. Treat API executions as stateless unless the provider adds a resumable
   session contract. The daemon clears stale CLI session identifiers before
   dispatching an API task rather than fabricating continuity.

The first release is capability-gated. Every provider must support prompt,
streaming response, completion, cancellation, and model discovery before it is
advertised as available. Tool calls, MCP, reasoning, usage cost, and resume are
advertised only for provider and model combinations covered by tests.

### Credential boundary

Subscription credentials stay in the provider's local CLI account store. API
keys are read only from daemon configuration or an approved runtime secret
source and are never persisted in agent rows, runtime rows, logs, UI payloads,
or memory layers. Local endpoints may be configured without a key. API endpoint
configuration is validated and redacted in all diagnostics.

The daemon supplies provider configuration to the backend after agent custom
environment merging, so user-controlled task environment values cannot
redirect a trusted provider or replace its credential. Provider-specific
credential variable names are blocked from task and MCP child inheritance.

### Registration and discovery

CLI providers continue to register through executable probing. API and local
providers register when their endpoint configuration is present and their
bounded probe succeeds. The registration payload includes provider ID,
protocol family, display name, provider capability metadata, model catalog
state, and a safe offline reason when probing fails. Client capability metadata
remains a separate field for mixed-version compatibility.

The daemon must not fail startup because an optional provider is offline. It
reports the provider as unavailable and retries it on the normal discovery
interval.

### Runtime and agent configuration

Provider IDs and protocol families are added to the canonical server whitelist
and frontend unions. API providers do not require a local executable. Their
runtime profile fields are endpoint reference, credential reference, and
capability policy; the credential reference is opaque and never returns the
secret.

Agent model selection uses the provider's catalog. A manually entered model is
allowed only when the provider descriptor explicitly supports manual models.
Provider, model, and capability validation occurs again at task claim time so
stale UI state cannot bypass the boundary.

## Testing strategy

### Backend unit tests

- Descriptor registry has one canonical entry per requested provider.
- CLI descriptors resolve to the existing native backends.
- API descriptors resolve to the correct adapter and default endpoint.
- URL, model, credential, and capability validation fails closed.
- SSE parsing handles text, reasoning, tool-call deltas, usage, terminal
  finish, malformed frames, provider errors, and premature EOF.
- Cancellation closes HTTP bodies and returns a cancelled result.
- Errors and logs contain no API keys, bearer values, or raw secret headers.
- Multi-turn usage is summed and session support is never fabricated.

### Daemon and handler tests

- Optional API providers register only after a successful bounded probe.
- Offline providers remain isolated from healthy provider registration.
- Provider config survives refresh without leaking secret values.
- Task environment cannot override trusted endpoint or credential values.
- Runtime profile and agent validation accept only the canonical provider set.
- Cross-workspace provider references are rejected.

### Frontend tests

- Runtime and agent pickers show all requested providers with accurate labels.
- API-key fields are masked and never round-trip plaintext from response data.
- Capability badges match server metadata.
- Offline and manual-model states are explicit and actionable.

### Integration tests

Use local HTTP fixtures for every API adapter family. Verify a non-mutating
prompt through streamed completion, model discovery, cancellation, malformed
provider response, and credential-redaction cases. Live provider checks remain
opt-in and must never embed keys in command arguments or committed fixtures.

## Rollout

Existing CLI behavior is unchanged. New API and local providers are disabled
until their endpoint probe and capability tests pass. A provider becomes
selectable only when the daemon reports it online with a verified catalog or an
explicitly supported manual-model capability. No provider is labeled fully
supported until its documented capability matrix is green.
