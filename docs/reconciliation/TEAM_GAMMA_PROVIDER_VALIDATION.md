# Team Gamma Provider Live Validation Audit

## Executive verdict

This audit covers `fork/main` at commit `240d4b9bb69df1d2fb1bf179668216b7c68d48c1` on 2026-08-29. No scoped provider can be marked live-verified from this audit because external provider calls were prohibited. The checkout is implementation-ready for deterministic testing, but it still needs provider-specific live evidence and several fail-closed corrections before the advertised capability surface is trustworthy.

The most important readiness findings are:

1. Cursor and Grok are the only scoped native subscription providers with checked-in real-account smoke tests. Codex, Claude, Antigravity, and native OpenCode have no live fixtures.
2. The API catalog grants `usage`, `tools`, and `mcp` to every API provider even though the repository has only adapter-family evidence, not provider and model-specific evidence.
3. Hosted base URL overrides accept any HTTPS host. A workspace profile can therefore pair a daemon-held approved credential variable with an arbitrary HTTPS endpoint. Provider-specific host binding or an explicit trusted operator override is required before hosted live testing.
4. API task launch does not revalidate the selected model against the last discovered catalog. OpenCode Zen and Go also route unknown model families to Chat Completions instead of failing closed.
5. The generic API live smoke copies the full process environment, can silently default to Ollama and the first discovered model, and has no direct secret non-leak assertion.
6. Native OpenCode is implemented by the server but is missing from the frontend replacement-provider catalog.
7. Claude, Antigravity, Cursor, and Grok have native diagnostic paths that forward unsanitized stderr or provider-error text to logs or results. These are live-verification blockers even where result-level redaction already exists.
8. Failed optional API and local probes are silently omitted, so the operator receives no sanitized offline reason for a missing credential, timeout, malformed catalog, or unreachable local service.

## Audit method and safety boundary

- Compared the working checkout to `fork/main` and confirmed the audited baseline commit.
- Read provider catalogs, discovery paths, execution backends, runtime-profile handling, daemon task resolution, and checked-in tests.
- Checked environment-variable presence by variable name and boolean only.
- Checked native executable presence by boolean only.
- Checked local default-port listener presence from the local socket table only.
- Ran deterministic local Go tests and compiled the tagged live tests with opt-in gates absent.
- Did not inspect credential values, CLI account stores, browser sessions, or provider configuration files.
- Did not call any provider, SaaS endpoint, Ollama endpoint, or LM Studio endpoint.
- Did not start, stop, pull, load, or delete any local model or service.

`OpenCode Console API` exists in the implementation catalog, but it is outside the requested Team Gamma scope. It is mentioned only where shared adapter behavior affects Zen and Go and is not assigned a verification status here.

## Provider implementation map

### Native subscription and CLI providers

The scoped subscription paths generally use provider-owned CLI login state, with row-specific exceptions for alternate Grok and Claude modes. Their "default endpoint" is not a Multica-configurable URL because execution is delegated to the local CLI.

| Provider ID | Implementation family | Credential source | Default endpoint | Discovery behavior | Execution path | Current gates |
| --- | --- | --- | --- | --- | --- | --- |
| `codex` | Native CLI, JSON-RPC app server | Codex or ChatGPT CLI-managed session. Shared login state is linked from `CODEX_HOME`, or the default Codex home, into a task-local home | None exposed by Multica | Runs `codex debug models --bundled`, with a checked-in static fallback. The fallback is not currently marked `Catalog.Fallback=true` | `codex app-server --listen stdio://` | Executable must resolve and version must be at least 0.100.0. Catalog advertises prompt, streaming, completion, cancellation, model discovery, and MCP. Real smoke gate is not implemented for Codex. |
| `claude` | Native CLI, stream-json | Claude CLI-managed subscription session. Optional custom environments can instead select non-subscription modes | None exposed by Multica | Uses a static Claude catalog and dynamically annotates supported thinking modes. This does not prove account entitlement | Claude non-interactive stream-json subprocess | Executable must resolve and version must be at least 2.0.0. Catalog advertises prompt, streaming, completion, cancellation, model discovery, and MCP. Real smoke gate is not implemented. Raw stderr logging/result handling needs sanitization. |
| `antigravity` | Native CLI, non-interactive text plus transcript recovery | `agy` CLI-managed Google Antigravity session | None exposed by Multica | Runs `agy models`. Failed or empty discovery collapses to an empty catalog, and explicit model validation then fails open | `agy -p`, optional `--model`, bounded output and transcript recovery | Executable must resolve, but no minimum version is enforced even though model selection requires a sufficiently new CLI. MCP is intentionally absent. Raw stderr and extracted provider-log diagnostics need sanitization. Real smoke gate is not implemented. |
| `cursor` | Native CLI, stream-json | Cursor Agent CLI-managed subscription session. `CURSOR_MCP_AUTH_SOURCE` is an MCP sidecar source, not the subscription credential | None exposed by Multica | Runs `cursor-agent --list-models`, with a fallback catalog correctly marked `Fallback=true` | `cursor-agent` stream-json with the prompt on stdin | Executable must resolve; no minimum version is enforced. Managed/local MCP is supported, while the separate remote MCP app-contribution broker omits Cursor. Raw stderr logging needs sanitization. An opt-in real smoke exists behind `MULTICA_RUN_REAL_AGENT_SMOKE=1`. |
| `grok` | Native CLI, Agent Client Protocol | Prefers `XAI_API_KEY` when present, otherwise uses the cached session created by `grok login` | None exposed by Multica | Authenticates and reads the model catalog from ACP `session/new`, then uses a static catalog correctly marked `Fallback=true` on failure | `grok --no-auto-update agent --always-approve stdio` | Executable must resolve and version must be at least 0.2.89. Managed/local MCP is supported, while the separate remote MCP app-contribution broker omits Grok. Raw stderr and provider-error summaries need sanitization. An opt-in real smoke exists behind `MULTICA_RUN_REAL_AGENT_SMOKE=1`. |
| `opencode` | Native CLI, JSON event stream | OpenCode CLI-managed session and provider configuration | None exposed by Multica | Runs `opencode models --verbose`, then plain `opencode models`; returns an empty catalog when discovery fails | `opencode run --format json --dangerously-skip-permissions`, prompt on stdin | Executable must resolve. Model selectors must be provider-qualified. Catalog advertises prompt, streaming, completion, cancellation, model discovery, and MCP. No real smoke exists, and the frontend provider catalog omits this ID. |

Primary implementation evidence:

- Catalog and capability definitions: `server/pkg/agent/provider_catalog.go:10-148`
- CLI detection and path/model overrides: `server/internal/daemon/agents_probe.go:115-247`
- Backend selection: `server/pkg/agent/agent.go:388-436`
- Model dispatch: `server/pkg/agent/models.go:140-278`
- Codex execution: `server/pkg/agent/codex.go:257-265`
- Claude execution: `server/pkg/agent/claude.go:34-52,713-770`
- Antigravity execution and model validation: `server/pkg/agent/antigravity.go:43-120,434-505`
- Cursor execution: `server/pkg/agent/cursor.go:17-46,969-1007`
- Grok execution: `server/pkg/agent/grok.go:54-70,110-151`
- OpenCode execution: `server/pkg/agent/opencode.go:43-189`
- Native minimum versions: `server/pkg/agent/version.go:11-22`
- Remote MCP broker allowlist: `server/internal/daemon/remote_mcp_broker.go:55-71,159-165`

### Hosted API and local OpenAI-compatible providers

The daemon resolves credentials, probes `GET /models`, and registers only providers whose configuration and probe succeed. Runtime execution uses the shared streaming adapter. API resume is disabled because these adapters are stateless.

| Provider ID | Implementation family | Credential source | Default endpoint | Discovery behavior | Execution path | Current gates |
| --- | --- | --- | --- | --- | --- | --- |
| `opencode-zen` | OpenCode multi-protocol API | `OPENCODE_ZEN_API_KEY`, fallback `OPENCODE_ZEN_TOKEN` | `https://opencode.ai/zen/v1` | Bounded `GET /models`; Gemini-prefixed models are filtered | Claude and `qwen3` use Anthropic Messages; GPT, Grok, and Muse Spark use Responses; other models currently fall through to Chat Completions | Credential required, hosted URL validation, successful probe. Catalog currently grants prompt, streaming, completion, cancellation, discovery, usage, tools, and MCP. Gemini is gated, but unknown families are not. |
| `opencode-go` | OpenCode multi-protocol API | `OPENCODE_GO_API_KEY`, fallback `OPENCODE_GO_TOKEN` | `https://opencode.ai/zen/go/v1` | Same bounded discovery and Gemini filter as Zen | Same protocol routing as Zen | Credential required, hosted URL validation, successful probe. Same blanket capability grant and unknown-family risk as Zen. |
| `openrouter` | Shared OpenAI-compatible API | `OPENROUTER_API_KEY` | `https://openrouter.ai/api/v1` | Bounded `GET /models` | Streaming Chat Completions | Credential required, hosted URL validation, successful probe. Catalog grants the full API capability set without provider/model evidence. |
| `vercel-ai-gateway` | Shared OpenAI-compatible API | `AI_GATEWAY_API_KEY`, fallback `VERCEL_OIDC_TOKEN` | `https://ai-gateway.vercel.sh/v1` | Bounded `GET /models` | Streaming Chat Completions | Credential required, hosted URL validation, successful probe. Catalog grants the full API capability set. The fallback credential is implemented but lacks a direct unit fixture. |
| `ollama` | Local OpenAI-compatible API | Optional `OLLAMA_API_KEY` | `http://127.0.0.1:11434/v1` | Bounded `GET /models` | Streaming Chat Completions | Keyless is allowed. Endpoint must be loopback HTTP and probe successfully. Tools and MCP are advertised globally even though support is model-dependent. |
| `lmstudio` | Local OpenAI-compatible API | Optional `LMSTUDIO_API_KEY` | `http://127.0.0.1:1234/v1` | Bounded `GET /models` | Streaming Chat Completions | Keyless is allowed. Endpoint must be loopback HTTP and probe successfully. Tools and MCP are advertised globally even though support is model-dependent. |
| `nvidia-nim` | Shared OpenAI-compatible API | `NVIDIA_API_KEY` | `https://integrate.api.nvidia.com/v1` | Bounded `GET /models` | Streaming Chat Completions | Credential required, hosted URL validation, successful probe. Catalog grants the full API capability set without provider/model evidence. |

Primary implementation evidence:

- Descriptors and credential resolution: `server/pkg/agent/provider_catalog.go:73-120,208-345`
- Adapter model discovery and execution: `server/pkg/agent/openai_compatible.go:29-277,363-513`
- Zen and Go protocol routing: `server/pkg/agent/opencode_api.go:17-41`
- Responses and Anthropic transports: `server/pkg/agent/opencode_streams.go:16-128,233-362`
- Daemon probe and registration: `server/internal/daemon/provider_discovery.go:18-136`
- Task backend configuration and stateless resume: `server/internal/daemon/daemon.go:7658-7687,7724-7744`
- Runtime-profile credential references: `server/internal/handler/runtime_profile.go:142-187`

## Durable fixture audit

### Existing checked-in evidence

| Provider or family | Durable deterministic coverage | Existing real smoke | Exact live gap |
| --- | --- | --- | --- |
| Codex | JSON-RPC lifecycle, parsing, start/resume, completion, error handling, cancellation, managed MCP, cleanup, and secret redaction in `codex_test.go`, `thinking_test.go`, and `codex_cleanup_unix_test.go` | None | Signed-in non-fallback discovery, streamed marker completion, start/resume, MCP, cancellation, cleanup, and non-leak behavior have no opt-in real fixture. Static fallback is not labeled as fallback. |
| Claude | Stream parsing, control events, MCP, resume, usage, cancellation, deadlock prevention, and context exhaustion, including `testdata/claude-code-2.1.220-context-exhausted-resume.jsonl` | None | Signed-in marker completion, account-valid model selection, MCP, resume, cancellation, and non-leak behavior have no opt-in real fixture. Raw stderr reaches logs and results without the shared sanitizer. |
| Antigravity | Fake executable coverage for provider errors, timeouts, transcript recovery, resume, model validation, and newline handling | None | Signed-in nonempty discovery, selected-model completion, resume, transcript fallback, cancellation, and non-leak behavior have no opt-in real fixture. Stderr and provider-log diagnostics are unsanitized. |
| Cursor | Recorded stream fixture at `testdata/cursor-agent-2026.07.20-stream-json.jsonl`, parsing, tool events, result redaction, and cancellation | `cursor_integration_test.go:14-91` | The real fixture logs raw version and model output, does not prove its `pong` artifact, discovery, cancellation, resume, managed MCP, remote MCP, or non-leak behavior. Raw stderr still reaches logs unsanitized. |
| Grok | Fake ACP coverage for authentication source selection, session creation/load, streams, MCP, usage, cancellation, and model parsing | `grok_integration_test.go:14-63` | The real fixture logs raw version, session ID, and final output and does not prove cached-login selection, non-fallback discovery, resume, MCP, usage, cancellation, or non-leak behavior. Raw stderr/provider errors need sanitization. |
| OpenCode native | JSON event fixtures, stdin transport, cancellation, MCP translation, model parsing, and fake CLI fallback | None | Signed-in discovery, streaming, completion, session behavior, cancellation, and MCP have no opt-in real fixture. |
| OpenCode Zen | Shared API fixtures plus one Zen Responses route case | Generic API smoke can select Zen, but no named or recorded run exists | Anthropic and Chat Completions routes lack Zen-specific fixtures. No live route has durable evidence. |
| OpenCode Go | Shared API fixtures plus one Go Anthropic route case | Generic API smoke can select Go, but no named or recorded run exists | Responses and Chat Completions routes lack Go-specific fixtures. No live route has durable evidence. |
| OpenRouter | Shared Chat Completions, discovery, usage, redaction, redirect, MCP, and cancellation fixtures frequently use OpenRouter-shaped models | Generic API smoke can select OpenRouter, but no named or recorded run exists | No provider-specific live discovery, completion, usage, cancellation, redaction, or MCP evidence. |
| Vercel AI Gateway | Descriptor and shared adapter fixtures | Generic API smoke can select Vercel, but no named or recorded run exists | No provider-specific fixture or live evidence. `VERCEL_OIDC_TOKEN` fallback lacks a direct test. |
| Ollama | Shared streaming, constructor, premature EOF, and cancellation fixtures | Generic API smoke silently defaults to Ollama | No authorized call was made to the listener. Service identity, models, streaming, and model-specific tool support remain unknown. |
| LM Studio | Descriptor and shared adapter fixtures only | Generic API smoke can select LM Studio | No service listener, provider-specific fixture, or live evidence. |
| NVIDIA NIM | Descriptor and shared adapter fixtures only | Generic API smoke can select NVIDIA NIM | No provider-specific fixture or live discovery, completion, usage, cancellation, or redaction evidence. |

Shared fixture references:

- Provider inventory, credentials, endpoint validation, and route gates: `server/pkg/agent/provider_catalog_test.go:5-216`
- Chat Completions, model discovery, Responses, Anthropic, redaction, redirect refusal, MCP, and cancellation: `server/pkg/agent/openai_compatible_test.go:16-491`
- Daemon discovery and registration: `server/internal/daemon/agents_probe_api_test.go:16-212`
- Credential-reference persistence: `server/internal/handler/runtime_profile_handler_test.go:33-107`
- Generic opt-in API smoke: `server/pkg/agent/provider_live_smoke_integration_test.go:1-104`
- Shared native opt-in gate: `server/pkg/agent/real_agent_smoke_integration_test.go:1-15`

### Exact missing deterministic tests

These are prerequisite fixtures because a credentialed live run should not be the first place a boundary is exercised:

1. Hosted endpoint trust policy: reject a credential-bearing override to an unapproved host, or require a separately trusted local operator override.
2. API task launch: revalidate the requested model against the discovered catalog or exercise an explicit manual-model policy.
3. Zen and Go: fail closed for unknown model families rather than silently choosing Chat Completions.
4. Daemon probe: reject an empty `data` array and reuse the same accepted catalog shapes as runtime `ListAPIModels`.
5. Daemon task boundary: prove that daemon-held endpoint and credential configuration wins over attempted task `custom_env` overrides, without exposing the credential.
6. Credential fallbacks: direct cases for `OPENCODE_ZEN_TOKEN`, `OPENCODE_GO_TOKEN`, and `VERCEL_OIDC_TOKEN`.
7. API protocol errors: malformed Chat Completions SSE; Responses and Anthropic premature termination, malformed events, redaction, cancellation, and tool-loop paths.
8. Usage behavior: multi-turn accumulation and provider/model-specific absence handling.
9. Profile refresh: retain approved credential references and endpoint configuration without returning secret values.
10. Native frontend parity: native `opencode` must appear in `packages/core/runtimes/provider-catalog.ts` and its support-card tests.
11. Native diagnostic redaction: sanitize Claude, Antigravity, Cursor, and Grok stderr and provider-error summaries before logging or returning them.
12. Native capability truth: represent Cursor and Grok remote MCP app contributions as a separate unsupported sub-capability while preserving their managed/local MCP support, and label Codex static discovery fallback correctly.
13. Optional-provider observability: retain a sanitized offline reason for missing credentials, probe timeout, invalid or empty catalog, and unreachable local service while continuing to register healthy siblings and retrying on refresh.

### Live harness defects

`TestConfiguredAPIProviderSmoke` is useful scaffolding, but it is not yet safe enough to serve as release evidence:

- `providerSmokeEnv()` copies every entry from `os.Environ()` instead of allowlisting only the selected provider's endpoint, credential, and model variables.
- Provider selection silently defaults to Ollama.
- Model selection silently takes the first sorted discovered model.
- A hosted run could therefore use an unintended or expensive model.
- The test checks completed status and one text marker, but not incremental streaming, usage, cancellation, secret redaction, MCP, or protocol-specific routes.
- The success log prints the descriptor default endpoint rather than proving the effective configured endpoint.

The Cursor and Grok live fixtures also run an unbounded `--version` subprocess before bounded execution and log raw version or model output. Live evidence should use only owned, context-bounded processes and sanitized booleans.

## Boolean-only environment and local readiness

No value was printed, persisted, decoded, or content-inspected. Only unset-or-empty versus nonempty presence was evaluated. A value of `false` below means the variable was absent or empty at audit time.

### API credential variables

| Environment variable | Present |
| --- | ---: |
| `OPENCODE_ZEN_API_KEY` | false |
| `OPENCODE_ZEN_TOKEN` | false |
| `OPENCODE_GO_API_KEY` | false |
| `OPENCODE_GO_TOKEN` | false |
| `OPENROUTER_API_KEY` | false |
| `AI_GATEWAY_API_KEY` | false |
| `VERCEL_OIDC_TOKEN` | false |
| `OLLAMA_API_KEY` | false |
| `LMSTUDIO_API_KEY` | false |
| `NVIDIA_API_KEY` | false |

### Native account-related environment variables

These variables are not all provider credentials. They are included because they can select an account store, alternate authentication path, or MCP sidecar source in a scoped native backend.

| Environment variable | Present | Role in scoped implementation |
| --- | ---: | --- |
| `CODEX_HOME` | true | Selects the shared Codex state root that feeds task-local Codex homes; the path/value was not inspected |
| `OPENAI_API_KEY` | false | Not used by the native ChatGPT subscription path audited here |
| `ANTHROPIC_API_KEY` | false | Optional non-subscription Claude mode |
| `ANTHROPIC_BASE_URL` | false | Optional non-subscription Claude endpoint selection |
| `CLAUDE_CODE_USE_BEDROCK` | false | Optional non-subscription Claude mode |
| `XAI_API_KEY` | false | Alternate Grok authentication; absent means a future authorized run would rely on cached `grok login` state |
| `CURSOR_MCP_AUTH_SOURCE` | false | Optional Cursor MCP sidecar source, not the Cursor subscription credential |

### Endpoint, model, and opt-in variables

| Environment variable | Present |
| --- | ---: |
| `OPENCODE_ZEN_BASE_URL` | false |
| `OPENCODE_GO_BASE_URL` | false |
| `OPENROUTER_BASE_URL` | false |
| `AI_GATEWAY_BASE_URL` | false |
| `OLLAMA_BASE_URL` | false |
| `LMSTUDIO_BASE_URL` | false |
| `NVIDIA_NIM_BASE_URL` | false |
| `MULTICA_OPENCODE_ZEN_MODEL` | false |
| `MULTICA_OPENCODE_GO_MODEL` | false |
| `MULTICA_OPENROUTER_MODEL` | false |
| `MULTICA_VERCEL_AI_GATEWAY_MODEL` | false |
| `MULTICA_OLLAMA_MODEL` | false |
| `MULTICA_LMSTUDIO_MODEL` | false |
| `MULTICA_NVIDIA_NIM_MODEL` | false |
| `MULTICA_RUN_REAL_AGENT_SMOKE` | false |
| `MULTICA_RUN_REAL_PROVIDER_SMOKE` | false |

### Native executable and local listener checks

| Check | Present |
| --- | ---: |
| `codex` executable | true |
| `claude` executable | true |
| `agy` executable | true |
| `cursor-agent` executable | true |
| `grok` executable | true |
| `opencode` executable | true |
| Ollama default-port listener at `127.0.0.1:11434` | true |
| LM Studio default-port listener at `127.0.0.1:1234` | false |

The six native path override variables and six native model override variables were also absent. Executable presence does not establish authentication, subscription entitlement, or minimum-version compliance. No `--version` command was run. A listener establishes only that some process owns the port; it does not establish service identity or model availability.

## Runnable now versus blocked

### Runnable now under this audit's no-call boundary

- All deterministic provider catalog, adapter, daemon registration, credential-reference, native parser, cancellation, and cleanup fixtures.
- Tagged compilation of the Cursor, Grok, and generic API live tests with opt-in gates absent. These must skip and must not resolve credentials or start provider execution.
- Boolean executable, environment-name, and local listener checks.

### Candidate live tests after explicit authorization

These are not proven runnable, because authentication and model availability were intentionally not inspected:

- Cursor real smoke: fixture exists and executable is present; authentication and version readiness are unknown.
- Grok real smoke: fixture exists and executable is present; authentication and required minimum-version readiness are unknown.
- Ollama generic API smoke: a default-port listener is present and no credential is required, but service identity and an existing model are unknown.

### Blocked by missing live test implementation

- Codex subscription
- Claude subscription
- Google Antigravity subscription
- OpenCode native

### Blocked by missing credentials

- OpenCode Zen
- OpenCode Go
- OpenRouter
- Vercel AI Gateway
- NVIDIA NIM

### Blocked by missing local service

- LM Studio

Every hosted API test is also blocked until provider-specific endpoint trust policy is corrected or a trusted fixed default endpoint is enforced. Every provider remains blocked from a `verified` label until green live evidence is captured against the exact commit under test.

## Credential-safe live test contract

Every live test should follow this common contract:

1. Require the `agentintegration` build tag and the provider-family opt-in gate before executable lookup, credential resolution, discovery, or execution.
2. Require an explicit provider and model. Never default a hosted run to the first returned model.
3. Construct a minimal environment map containing only the selected descriptor's base URL variable, primary credential variable, optional credential variables, and model variable.
4. Keep credentials out of argv, test names, subtest names, logs, snapshots, error formatting, and persisted artifacts.
5. For environment-key providers, capture messages, result fields, errors, and test logs in memory and assert only a boolean that the in-memory credential is absent. Never include the credential in failure output.
6. For CLI-session providers, do not inspect account stores or actual tokens. Require companion deterministic sanitizer-canary fixtures and keep the live test's diagnostic logging to sanitized metadata only.
7. Use a fresh `t.TempDir()` workspace for every CLI run.
8. Use a unique exact success marker per provider and protocol route.
9. Assert at least one incremental stream event before terminal completion.
10. Use a dedicated HTTP transport for API tests, close every response body, and call `CloseIdleConnections()` during cleanup.
11. Cancel the context, drain message and result channels, require terminal completion, and verify owned process exit before the hard deadline.
12. Run provider tests serially to bound quota and simplify attribution.
13. Never create, upload, pull, delete, or mutate remote or local provider resources. Use a plain text prompt and an already available model.
14. Log only provider ID, selected model ID, marker matched boolean, duration, terminal status, and separately verified capability booleans.

Use a separate cancellation case with a 30 second outer limit. Start a long generation, cancel after the first stream event or after five seconds, and require terminal cancellation and process cleanup within ten seconds.

## Proposed live validation matrix

Baseline verification means discovery, requested-model presence, incremental streaming, exact marker completion, terminal status, redaction checks, and cleanup. Usage, tools, and MCP require separate green cases before those capabilities remain advertised.

| Provider or route | Exact success marker | Hard timeout | Baseline live case | Additional capability case | Current blocker |
| --- | --- | ---: | --- | --- | --- |
| Codex | `MULTICA-LIVE-CODEX-OK` | 180s overall, 170s execution | Signed-in discovery and app-server marker completion | Local deterministic MCP tool plus cancellation | Live fixture missing; auth unknown |
| Claude | `MULTICA-LIVE-CLAUDE-OK` | 180s overall, 170s execution | Static catalog selection and stream-json marker completion | Local deterministic MCP tool plus cancellation | Live fixture missing; auth unknown |
| Antigravity | `MULTICA-LIVE-ANTIGRAVITY-OK` | 180s overall, 170s execution | `agy models`, selected model, marker completion, transcript fallback check | Cancellation only; MCP remains off | Live fixture missing; auth unknown |
| Cursor | `MULTICA-LIVE-CURSOR-OK` | 180s overall, 170s execution | Harden existing test with non-mutating marker and explicit discovery | Existing tool behavior plus separate cancellation and non-leak assertions | Auth unknown; fixture hardening needed |
| Grok | `MULTICA-LIVE-GROK-OK` | 90s overall, 80s execution | Harden existing ACP marker test with explicit discovery | Local deterministic MCP tool plus cancellation and non-leak assertions | Auth unknown; fixture hardening needed |
| OpenCode native | `MULTICA-LIVE-OPENCODE-OK` | 180s overall, 170s execution | Provider-qualified discovery and JSON-stream marker completion | Local deterministic MCP tool plus cancellation | Live fixture missing; auth unknown |
| OpenCode Zen Responses | `MULTICA-LIVE-ZEN-RESPONSES-OK` | 100s overall; 15s discovery, 75s execution | Explicit supported GPT, Grok, or Muse Spark model | Usage plus one local MCP tool turn | Credential missing; named fixture missing |
| OpenCode Zen Anthropic | `MULTICA-LIVE-ZEN-ANTHROPIC-OK` | 100s overall; 15s discovery, 75s execution | Explicit supported Claude or `qwen3` model | Usage plus one local MCP tool turn | Credential missing; route fixture missing |
| OpenCode Zen Chat | `MULTICA-LIVE-ZEN-CHAT-OK` | 100s overall; 15s discovery, 75s execution | Explicit catalog model with an approved Chat route | Usage plus one local MCP tool turn | Credential missing; route policy and fixture missing |
| OpenCode Go Responses | `MULTICA-LIVE-GO-RESPONSES-OK` | 100s overall; 15s discovery, 75s execution | Explicit supported Responses model | Usage plus one local MCP tool turn | Credential missing; route fixture missing |
| OpenCode Go Anthropic | `MULTICA-LIVE-GO-ANTHROPIC-OK` | 100s overall; 15s discovery, 75s execution | Explicit supported Claude or `qwen3` model | Usage plus one local MCP tool turn | Credential missing; named live case missing |
| OpenCode Go Chat | `MULTICA-LIVE-GO-CHAT-OK` | 100s overall; 15s discovery, 75s execution | Explicit catalog model with an approved Chat route | Usage plus one local MCP tool turn | Credential missing; route policy and fixture missing |
| OpenRouter | `MULTICA-LIVE-OPENROUTER-OK` | 100s overall; 15s discovery, 75s execution | Explicit pinned model, streamed Chat completion | Usage plus model-supported local MCP tool turn | Credential missing; host policy and named test missing |
| Vercel AI Gateway | `MULTICA-LIVE-VERCEL-OK` | 100s overall; 15s discovery, 75s execution | Run once per supported credential source with explicit model | Usage plus model-supported local MCP tool turn | Credentials missing; host policy and fallback fixture missing |
| Ollama | `MULTICA-LIVE-OLLAMA-OK` | 75s overall; 10s discovery, 60s execution | Explicit already-installed model, no pull, streamed completion | Only for a model known to support tools | Service/model identity unknown; harness hardening needed |
| LM Studio | `MULTICA-LIVE-LMSTUDIO-OK` | 75s overall; 10s discovery, 60s execution | Explicit already-loaded model, streamed completion | Only for a model known to support tools | No listener; harness hardening needed |
| NVIDIA NIM | `MULTICA-LIVE-NVIDIA-NIM-OK` | 100s overall; 15s discovery, 75s execution | Explicit pinned model, streamed Chat completion | Usage plus model-supported local MCP tool turn | Credential missing; host policy and named test missing |

The MCP capability case should use a deterministic localhost test server, require exactly one tool call and one matching result, and end with the provider-specific marker. It must not depend on a public MCP server.

## Smallest implementation changes by provider

| Provider | Minimum change before `verified` is allowed |
| --- | --- |
| Codex | Mark static model discovery as `Fallback=true`, then add a gated real CLI smoke for non-fallback discovery, exact marker completion, start/resume, managed MCP, cancellation, redaction, and process cleanup. |
| Claude | Sanitize stderr before daemon logging and `Result.Error`, add direct redaction fixtures, then add the equivalent gated stream-json smoke. Replace or qualify static model discovery if account entitlement cannot be proven. |
| Antigravity | Enforce a minimum version that supports claimed model behavior, return a real discovery failure, fail closed for an explicit model without an authoritative catalog, sanitize stderr/provider logs, and add a gated `agy models` plus marker smoke. Retain the no-MCP gate. |
| Cursor | Sanitize stderr before logging, establish a minimum tested version, remove raw version/output logging, replace the unbounded version command, assert the marker artifact, and add discovery, resume, cancellation, managed MCP, and sanitizer-canary cases. Gate remote MCP app contributions separately or add Cursor to that broker with tests. |
| Grok | Sanitize stderr and provider-error summaries, remove raw version/output/session logging, replace the unbounded version command, and add cached-login selection, non-fallback discovery, resume, cancellation, local MCP, and sanitizer-canary cases. Gate remote MCP app contributions separately or add Grok to that broker with tests. |
| OpenCode native | Add a gated discovery and execution smoke, plus cancellation and MCP evidence if advertised. Add `opencode` to the frontend replacement-provider catalog and tests. |
| OpenCode Zen | Add Zen-specific Anthropic and Chat fixtures, fail unknown model families closed, verify selected models against discovery, then live-test one model per supported protocol. Gate usage, tools, and MCP per proven model route. |
| OpenCode Go | Add Go-specific Responses and Chat fixtures, fail unknown model families closed, verify selected models against discovery, then live-test one model per supported protocol. Gate usage, tools, and MCP per proven model route. |
| OpenRouter | Bind credential-bearing endpoints to the official host unless a trusted operator override is enabled. Add a named explicit-model baseline and retain advanced capabilities only after model-specific evidence. |
| Vercel AI Gateway | Apply the same endpoint trust rule, directly test both credential-source fallbacks, and add a named explicit-model discovery and completion smoke. |
| Ollama | Harden the generic harness and run it only against an explicit already-installed model. Gate tools and MCP by selected model. Do not add service or model lifecycle management to the test. |
| LM Studio | Harden the generic harness. Once an operator starts the service and loads a model outside the test, run an explicit-model baseline and keep tools/MCP model-gated. |
| NVIDIA NIM | Apply hosted endpoint binding, add a provider-named fixture and explicit-model live baseline, then retain advanced capabilities only after provider/model evidence. |

## Cross-cutting release gates

The smallest safe production sequence is:

1. Correct trust and capability gates.
   - Bind hosted credentials to provider-approved hosts or require an explicit trusted operator override.
   - Validate API models at task launch.
   - Fail unknown Zen and Go routes closed.
   - Advertise only prompt, streaming, completion, cancellation, and discovery as the API baseline until advanced evidence exists.
   - Sanitize every native stderr/provider-error path before logging or returning diagnostics.
   - Align native discovery metadata and the separate remote MCP app-contribution gate with actual fallback and broker behavior.
   - Preserve a sanitized offline reason for optional provider probe failures while isolating healthy providers and retrying failed probes during refresh.
2. Harden and fill the harness.
   - Make provider and model explicit.
   - Use an allowlisted environment.
   - Add secret non-leak, cancellation, channel drain, process cleanup, and transport cleanup assertions.
   - Add missing native and protocol-route fixtures.
3. Run a controlled live campaign.
   - Use exact commit provenance.
   - Run one provider at a time with explicit authorization.
   - Record only sanitized booleans, model ID, duration, and terminal status.
   - Mark a provider baseline-verified only after its baseline matrix row is green.
   - Mark usage, tools, and MCP verified independently.

## Verification performed in this audit

The following local, no-network test groups passed:

```text
GOPROXY=off go test ./pkg/agent -run 'Test(Provider|APIProvider|OpenAICompatible|OpenCode|ListAPIModels|Opencode)' -count=1
GOPROXY=off go test ./internal/daemon -run 'Test(ProbeAPI|APIProvider|RuntimeProfilesRegisterAPI|DetectBuiltinRuntimesRegistersAPI|IsBlockedEnvKey)' -count=1
```

Focused runtime-profile handler fixtures also passed. The tagged Cursor, Grok, and generic API live tests compiled and skipped because their opt-in gates were absent. No provider or local-model request was made.

## Final status

| Status | Providers |
| --- | --- |
| Deterministic implementation evidence present | All scoped providers |
| Existing opt-in real smoke, not run in this audit | Cursor, Grok, generic one-at-a-time API harness |
| Candidate for a future authorized local run | Ollama, subject to confirming service identity and an existing model |
| Blocked by missing live fixture | Codex, Claude, Antigravity, OpenCode native |
| Blocked by missing credential | OpenCode Zen, OpenCode Go, OpenRouter, Vercel AI Gateway, NVIDIA NIM |
| Blocked by missing local service | LM Studio |
| Live-verified at audited commit | None |
