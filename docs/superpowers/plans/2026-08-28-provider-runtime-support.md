# Provider Runtime Support Implementation Plan

> **For the implementation worker:** REQUIRED SUB-SKILL: Use the executing-plans workflow and test-driven development. Work in `/Users/bradstrawbridge/.config/superpowers/worktrees/multica/provider-runtime` only. Do not edit the archived Phase 3 worktree and do not add or extend OmniRoute.

**Goal:** Add first-class support for the requested subscription, hosted API, and local-model providers while reusing Multica's existing native CLI backends and unified task contract.

**Architecture:** Keep `codex`, `claude`, `antigravity`, `cursor`, `grok`, and native `opencode` on their existing CLI backends. Add a canonical provider descriptor registry and a secure shared HTTP adapter for OpenCode Console, OpenCode Zen, OpenCode Go, OpenRouter, Vercel AI Gateway, Ollama, LM Studio, and NVIDIA NIM. The shared adapter supports OpenAI Chat Completions, OpenAI Responses, and Anthropic Messages routes where the provider's model catalog documents them. Add daemon-owned endpoint and credential resolution, capability-gated registration, model discovery, and frontend metadata. API providers can be defined as workspace runtime profiles with endpoint and approved credential-environment metadata; daemon-only environment discovery remains supported for built-in provider identities.

**Test command baseline:** `GOPROXY=off go test ./... -count=1`, `GOPROXY=off go vet ./...`, `pnpm turbo run lint typecheck test --force`.

## Task 1: Establish a clean provider-support baseline

**Files:** `server/pkg/agent/agent.go`, `server/pkg/agent/agent_supported_types_test.go`, `server/internal/daemon/agents_probe.go`, `server/internal/daemon/config.go`, `server/internal/daemon/daemon.go`, `packages/core/types/agent.ts`, relevant provider registry tests.

1. Add failing tests for the canonical provider inventory and descriptor fields.
2. Centralize the provider metadata in `server/pkg/agent/provider_catalog.go`.
3. Keep existing CLI constructors and registration behavior unchanged while making their descriptor entries derive from the catalog.
4. Add descriptor capability types shared by daemon registration and model discovery.
5. Run the focused agent and daemon tests.

## Task 2: Add the shared OpenAI-compatible HTTP backend

**Files:** `server/pkg/agent/openai_compatible.go`, `server/pkg/agent/openai_compatible_test.go`, `server/pkg/agent/agent.go`, `server/pkg/agent/models.go` or the current model-discovery helpers.

1. Write fixture-driven tests first for request shape, authentication, streamed text, reasoning, usage, provider errors, malformed frames, terminal markers, premature EOF, and cancellation.
2. Implement an `openAICompatibleBackend` satisfying `agent.Backend` without introducing provider-specific branches into the parser.
3. Use an injected `http.Client`, clock, and endpoint resolver in tests; production uses bounded timeouts and daemon configuration.
4. Accumulate usage across turns and return only provider-issued session IDs.
5. Apply the existing redaction package to errors and diagnostics.
6. Add model discovery through `GET /models` with bounded response size and explicit manual-model fallback only when the descriptor permits it.
7. Run `go test ./pkg/agent -run 'OpenAI|Provider|Model' -count=1` and `go vet ./pkg/agent`.

## Task 3: Add OpenCode API transport

**Files:** `server/pkg/agent/opencode_api.go`, `server/pkg/agent/opencode_api_test.go`, provider catalog files, model discovery helpers.

1. Add separate first-class identities for OpenCode Console, OpenCode Zen, and OpenCode Go with separate endpoint and credential variables.
2. Route OpenCode Console through Chat Completions and route Zen and Go models through the documented Chat Completions, Responses, or Anthropic Messages endpoint.
3. Resolve the protocol from the provider/model catalog contract and fail closed for native Google models until a Gemini adapter exists. Never silently use the Console endpoint for Zen or Go.
4. Add redaction, cancellation, usage, tool-loop, malformed-stream, and protocol-selection tests.
5. Run focused provider tests and compare the adapter's emitted messages with the existing CLI fixtures.

## Task 4: Wire provider configuration and safe daemon discovery

**Files:** `server/internal/daemon/config.go`, `server/internal/daemon/agents_probe.go`, `server/internal/daemon/daemon.go`, `server/internal/daemon/types.go`, `server/internal/daemon/agent_command_names.go` or the current discovery helpers, new daemon tests, `.env.example`.

1. Add failing tests for API provider discovery with healthy, offline, malformed, and missing endpoint configuration.
2. Define daemon configuration names and safe defaults for each provider. Local defaults must be loopback only; hosted defaults must use the documented provider origin.
3. Ensure API providers do not require an executable and are represented in the same runtime registration response as CLI providers.
4. Apply trusted provider endpoint and credential values after agent custom environment merging.
5. Add provider credential names to blocked task and MCP environment keys.
6. Bound health probes, model probes, response sizes, and retry frequency.
7. Add registration capability metadata and safe offline reasons without secret values.
8. Run focused daemon discovery and registration tests.

## Task 5: Persist runtime provider configuration and enforce server validation

**Files:** new numbered migration pair under `server/migrations/`, `server/pkg/db/queries/runtime_profile.sql`, generated runtime-profile SQL/models, `server/internal/handler/runtime_profile.go`, `server/internal/handler/runtime.go`, `server/internal/handler/agent.go`, handler tests.

1. Add a migration that extends the runtime provider whitelist and stores non-secret provider configuration metadata. Store only endpoint references and opaque credential references, never plaintext keys.
2. Keep the existing custom CLI profile path valid, make API profiles executable-free, and preserve separate transport validation for both profile types.
3. Validate provider ID, protocol family, endpoint scheme/host policy, model policy, and capability policy on create, update, registration, and task claim.
4. Add workspace ownership checks for every provider configuration lookup.
5. Regenerate SQL code with the repository's configured `sqlc` command.
6. Run isolated Postgres migrations and handler tests.

## Task 6: Connect task execution to API provider backends

**Files:** `server/internal/daemon/daemon.go`, `server/internal/daemon/client.go` if task metadata needs extension, `server/pkg/agent/agent.go`, execution tests.

1. Add failing tests that resolve each new provider to the correct adapter and reject unavailable capability combinations before launch.
2. Plumb provider descriptor, endpoint, credential reference, model, system context, cancellation, and capability metadata into `agent.Config` and `ExecOptions`.
3. Preserve existing CLI launch paths and session behavior.
4. Ensure task-scoped custom environment cannot override trusted API provider settings.
5. Add non-mutating end-to-end HTTP fixture tests through the daemon runner.
6. Run `go test ./internal/daemon ./pkg/agent -count=1` and race tests for those packages.

## Task 7: Add frontend provider and capability surfaces

**Files:** `packages/core/types/agent.ts`, `packages/core/types/api.ts`, `packages/core/api/client.ts`, runtime and agent picker components, provider logo/label maps, focused component tests.

1. Add failing tests for the provider catalog, capability badges, masked credential forms, offline state, and manual-model state.
2. Generate or hand-maintain the frontend provider union from the server contract with a compatibility fallback for older servers.
3. Add provider setup cards for endpoint and credential references without ever rendering secret values.
4. Show only capabilities returned by the daemon and server validation.
5. Add clear unavailable and probe-failed states.
6. Run focused UI tests, then the full Turbo lint, typecheck, and test matrix.

## Task 8: Documentation and verification gates

**Files:** `README.md`, `README.zh.md` or current localized docs, `CLI_AND_DAEMON.md`, `SELF_HOSTING.md`, `apps/docs/content/docs/providers.mdx`, `.env.example`, provider design/spec docs.

1. Document setup for subscription CLI providers, hosted API providers, and local endpoints.
2. Document capability gates and the exact environment variables without example secrets.
3. Add local fixture commands and opt-in live verification commands that keep credentials out of argv and logs.
4. Run `git diff --check`, `gofmt -d`, the full Go build/test/vet/race matrix, full frontend checks, and a changed-line no-em-dash scan.
5. Review the complete diff for secret persistence, endpoint redirection, workspace isolation, and provider-list drift.

## Task 9: Review and integration

1. Request a code review focused on credential custody, SSRF/endpoint validation, cancellation, SSE terminal handling, capability truthfulness, and workspace isolation.
2. Fix all blocking findings and rerun affected tests.
3. Commit only provider-runtime changes on `feat/multica-provider-runtime-support`.
4. Push only after local verification is green and the operator authorizes remote delivery.
5. Do not merge automatically from the Codex coder runtime. Leave merge readiness and any upstream conflict resolution explicit.

## Definition of done

- All requested provider identities are represented by a canonical descriptor.
- Existing subscription CLI providers continue to pass their current tests.
- API and local providers execute through tested HTTP adapters with bounded SSE parsing, cancellation, redaction, model discovery, and truthful capability gates.
- No API key is persisted, returned, logged, inherited by task children, or placed in command arguments.
- Daemon registration, server validation, UI setup, and docs agree on provider availability.
- Full backend and frontend verification is green, with no new em dashes.
