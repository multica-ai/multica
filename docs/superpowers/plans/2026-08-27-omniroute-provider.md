# OmniRoute Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a native Multica `omniroute` provider that uses the Tailscale VM's OpenAI-compatible API, executes MCP tools under Multica controls, and streams complete agent sessions.

**Architecture:** Add a focused Go backend in `server/pkg/agent` using Chat Completions over HTTP and SSE. Keep MCP discovery and invocation behind an internal interface, with the first implementation translating the existing Multica MCP configuration into streamable HTTP or stdio clients as supported. Register `omniroute` in the existing provider and runtime model paths, while preserving allowlists, session affinity, and the existing daemon `Session` contract.

**Tech Stack:** Go standard library HTTP and JSON, existing `agent.Backend` and `agent.Session` interfaces, MCP JSON-RPC transport, Chi daemon integration, Go tests with `httptest`, and existing repository verification commands.

---

## File map

- Create `server/pkg/agent/omniroute.go`: OmniRoute backend, request loop, SSE consumption, tool-call handling, and result publication.
- Create `server/pkg/agent/omniroute_mcp.go`: MCP configuration decoding, tool listing, and tool invocation boundary.
- Create `server/pkg/agent/omniroute_test.go`: HTTP fixtures, SSE parser, request contract, session behavior, and error tests.
- Create `server/pkg/agent/omniroute_mcp_test.go`: MCP transport and tool allowlist tests.
- Modify `server/pkg/agent/agent.go`: construct the provider and document its configuration contract.
- Modify `server/pkg/agent/models.go`: list OmniRoute models and mark model selection as supported.
- Modify `server/pkg/agent/agent_supported_types_test.go` and related tests: update the canonical provider whitelist expectations.
- Modify `server/internal/daemon/daemon.go`: pass the remote URL and secret-backed environment configuration, preserve provider identity, and enforce allowlist support.
- Modify runtime profile migration or validation only if the existing provider whitelist check requires a schema update for `omniroute`.
- Modify provider documentation and configuration examples only after the implementation passes tests.

### Task 1: Define provider configuration and failing transport tests

**Files:**
- Create: `server/pkg/agent/omniroute.go`
- Create: `server/pkg/agent/omniroute_test.go`
- Modify: `server/pkg/agent/agent.go`

- [x] Add tests for missing base URL, missing API key, URL normalization, bearer authentication, model selection, and `X-Session-Id` propagation using `httptest.Server`.
- [x] Run `go test ./pkg/agent -run OmniRoute -count=1 -v` and verify the new tests fail because the provider does not exist.
- [x] Add an `omnirouteBackend` implementing `Backend`, with configuration resolved from `Config.Env` and no credential logging.
- [x] Add `case "omniroute"` to `agent.New` and include it in the supported provider error text.
- [x] Make `SupportsToolAllowlist("omniroute")` return true because the backend filters tools before sending them upstream.
- [x] Run the focused tests and verify they pass.
- [x] Commit with `feat: add OmniRoute backend transport`.

### Task 2: Implement SSE and Chat Completions parsing

**Files:**
- Modify: `server/pkg/agent/omniroute.go`
- Modify: `server/pkg/agent/omniroute_test.go`

- [x] Add failing fixtures for text deltas, multiple indexed tool calls, fragmented JSON arguments, usage in the final frame, finish reasons, `[DONE]`, malformed JSON, and non-2xx errors.
- [x] Run the focused tests and verify the parser tests fail.
- [x] Implement bounded SSE line parsing with context cancellation and response-body closure.
- [x] Buffer tool-call fragments by index and emit `MessageToolUse` only after arguments decode as valid JSON.
- [x] Emit `MessageText`, `MessageThinking`, `MessageToolUse`, `MessageStatus`, and final `Result.Usage` using the existing types.
- [x] Sanitize upstream error bodies and never include authorization headers in errors.
- [x] Run focused parser and session tests and verify they pass.
- [x] Commit with `feat: parse OmniRoute streaming responses`.

### Task 3: Add MCP discovery and invocation boundary

**Files:**
- Create: `server/pkg/agent/omniroute_mcp.go`
- Create: `server/pkg/agent/omniroute_mcp_test.go`
- Modify: `server/pkg/agent/omniroute.go`

- [x] Add failing tests for decoding supported MCP server entries, listing tools, invoking a tool, JSON-RPC errors, transport disconnects, and disallowed tool names.
- [x] Run `go test ./pkg/agent -run 'OmniRoute.*MCP' -count=1 -v` and verify failure.
- [x] Define a small interface with `ListTools` and `CallTool` methods so the LLM loop is independent of transport details.
- [x] Implement the first transport against the existing streamable HTTP MCP shape, including initialization and session headers. Support stdio only if the current Multica configuration requires it for the first live fixture.
- [x] Convert MCP tool schemas into OpenAI function tool definitions without changing argument schemas.
- [x] Enforce the configured allowlist before exposing tools and again before invocation.
- [x] Convert MCP results and errors into OpenAI tool-result messages and `MessageToolResult` events.
- [x] Run focused MCP tests and verify they pass.
- [x] Commit with `feat: connect OmniRoute agent loop to MCP tools`.

### Task 4: Complete the multi-turn tool loop and daemon registration

**Files:**
- Modify: `server/pkg/agent/omniroute.go`
- Modify: `server/internal/daemon/daemon.go`
- Modify: `server/pkg/agent/models.go`
- Modify: `server/pkg/agent/agent_supported_types_test.go`
- Modify: relevant runtime profile migration or validation tests if required

- [x] Add failing tests for one tool turn followed by a final answer, parallel tool calls, loop-limit exhaustion, cancellation during tool execution, and session ID reuse.
- [x] Add a failing model-list test for authenticated `GET /v1/models` and upstream authentication failure.
- [x] Run focused tests and verify failure.
- [x] Implement the conversation loop with a configurable maximum turn count and no automatic retry of mutating calls.
- [x] Preserve assistant tool-call messages and append tool results in the OpenAI Chat Completions format.
- [x] Send `X-Session-Id` consistently for every turn in one Multica task.
- [x] Add `ListModels` support from the OmniRoute endpoint and map model IDs, labels, providers, and capability metadata without storing the API key.
- [x] Register `omniroute` in `SupportedTypes`, model selection support, runtime registration, and allowlist enforcement.
- [x] Pass `OMNIROUTE_BASE_URL` and `OMNIROUTE_API_KEY` through the daemon's existing environment injection path, with the remote VM URL as the documented deployment target.
- [x] Run focused daemon and provider tests and verify they pass.
- [x] Commit with `feat: register OmniRoute as a Multica provider`.

### Task 5: Verification, documentation, and live execution test

**Files:**
- Modify: `README.md` or the existing provider documentation location
- Modify: provider configuration example if one exists
- Test: `server/pkg/agent/omniroute_test.go`

- [x] Add a non-mutating live test script or documented command that checks authenticated `/v1/models` and sends a minimal tool-call request against the Tailscale VM.
- [x] Do not run the live test until `OMNIROUTE_API_KEY` has been rotated and injected after the previous exposure.
- [x] Run `go test ./...`.
- [x] Run `go build ./...`.
- [x] Run `pnpm lint`, `pnpm typecheck`, and `pnpm test` from the repository root.
- [x] Run the authenticated `/models` probe against the Tailscale endpoint without printing the key.
- [x] Run the non-mutating execution test with the selected OmniRoute model and verify a complete Multica result, at least one structured tool call, and one tool result.
- [x] Verify no em dash characters were introduced in changed prose, comments, or commit messages.
- [x] Document the remote endpoint requirement, model capability caveat, key injection, and local endpoint fallback behavior.
- [x] Commit with `docs: document OmniRoute provider operation`.

## Execution checkpoints

- After Tasks 1 through 3, review the provider API and MCP boundary before adding daemon wiring.
- After Task 4, review the full local test suite and inspect the diff for credential leakage.
- Task 5 is the only task allowed to contact the live OmniRoute VM, and only after a rotated key is available.

## Implementation status

Completed locally on 2026-08-28. The provider is registered in the Go backend,
daemon runtime and frontend MCP capability map. It uses the configured remote
OpenAI-compatible endpoint, supports streamed text and reasoning, namespaced
MCP tools over HTTP or stdio, session affinity, allowlists, bounded tool loops,
usage accumulation, and fail-closed stream parsing. The live integration test
uses a temporary non-mutating stdio probe and passed against the configured
Tailscale VM after an authenticated `/models` response.

Verification completed:

- `go test ./... -count=1`
- `go build ./...`
- `go vet ./...`
- `go test -race ./pkg/agent ./internal/daemon ./internal/handler`
- forced Turbo lint, typecheck, and test runs for all non-mobile packages
- `go test -tags=integration ./pkg/agent -run TestOmniRouteLive -count=1 -v`

The public OmniRoute API reference documents `GET /v1/models`,
`POST /v1/chat/completions`, Bearer authentication, and `X-Session-Id` session
affinity. It does not publish a stable tool-capability field in the model
catalog, so model capability metadata remains unset unless a future endpoint
contract exposes it.
