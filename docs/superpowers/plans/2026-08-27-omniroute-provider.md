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

- [ ] Add tests for missing base URL, missing API key, URL normalization, bearer authentication, model selection, and `X-Session-Id` propagation using `httptest.Server`.
- [ ] Run `go test ./pkg/agent -run OmniRoute -count=1 -v` and verify the new tests fail because the provider does not exist.
- [ ] Add an `omnirouteBackend` implementing `Backend`, with configuration resolved from `Config.Env` and no credential logging.
- [ ] Add `case "omniroute"` to `agent.New` and include it in the supported provider error text.
- [ ] Make `SupportsToolAllowlist("omniroute")` return true because the backend filters tools before sending them upstream.
- [ ] Run the focused tests and verify they pass.
- [ ] Commit with `feat: add OmniRoute backend transport`.

### Task 2: Implement SSE and Chat Completions parsing

**Files:**
- Modify: `server/pkg/agent/omniroute.go`
- Modify: `server/pkg/agent/omniroute_test.go`

- [ ] Add failing fixtures for text deltas, multiple indexed tool calls, fragmented JSON arguments, usage in the final frame, finish reasons, `[DONE]`, malformed JSON, and non-2xx errors.
- [ ] Run the focused tests and verify the parser tests fail.
- [ ] Implement bounded SSE line parsing with context cancellation and response-body closure.
- [ ] Buffer tool-call fragments by index and emit `MessageToolUse` only after arguments decode as valid JSON.
- [ ] Emit `MessageText`, `MessageThinking`, `MessageToolUse`, `MessageStatus`, and final `Result.Usage` using the existing types.
- [ ] Sanitize upstream error bodies and never include authorization headers in errors.
- [ ] Run focused parser and session tests and verify they pass.
- [ ] Commit with `feat: parse OmniRoute streaming responses`.

### Task 3: Add MCP discovery and invocation boundary

**Files:**
- Create: `server/pkg/agent/omniroute_mcp.go`
- Create: `server/pkg/agent/omniroute_mcp_test.go`
- Modify: `server/pkg/agent/omniroute.go`

- [ ] Add failing tests for decoding supported MCP server entries, listing tools, invoking a tool, JSON-RPC errors, transport disconnects, and disallowed tool names.
- [ ] Run `go test ./pkg/agent -run 'OmniRoute.*MCP' -count=1 -v` and verify failure.
- [ ] Define a small interface with `ListTools` and `CallTool` methods so the LLM loop is independent of transport details.
- [ ] Implement the first transport against the existing streamable HTTP MCP shape, including initialization and session headers. Support stdio only if the current Multica configuration requires it for the first live fixture.
- [ ] Convert MCP tool schemas into OpenAI function tool definitions without changing argument schemas.
- [ ] Enforce the configured allowlist before exposing tools and again before invocation.
- [ ] Convert MCP results and errors into OpenAI tool-result messages and `MessageToolResult` events.
- [ ] Run focused MCP tests and verify they pass.
- [ ] Commit with `feat: connect OmniRoute agent loop to MCP tools`.

### Task 4: Complete the multi-turn tool loop and daemon registration

**Files:**
- Modify: `server/pkg/agent/omniroute.go`
- Modify: `server/internal/daemon/daemon.go`
- Modify: `server/pkg/agent/models.go`
- Modify: `server/pkg/agent/agent_supported_types_test.go`
- Modify: relevant runtime profile migration or validation tests if required

- [ ] Add failing tests for one tool turn followed by a final answer, parallel tool calls, loop-limit exhaustion, cancellation during tool execution, and session ID reuse.
- [ ] Add a failing model-list test for authenticated `GET /v1/models` and upstream authentication failure.
- [ ] Run focused tests and verify failure.
- [ ] Implement the conversation loop with a configurable maximum turn count and no automatic retry of mutating calls.
- [ ] Preserve assistant tool-call messages and append tool results in the OpenAI Chat Completions format.
- [ ] Send `X-Session-Id` consistently for every turn in one Multica task.
- [ ] Add `ListModels` support from the OmniRoute endpoint and map model IDs, labels, providers, and capability metadata without storing the API key.
- [ ] Register `omniroute` in `SupportedTypes`, model selection support, runtime registration, and allowlist enforcement.
- [ ] Pass `OMNIROUTE_BASE_URL` and `OMNIROUTE_API_KEY` through the daemon's existing environment injection path, with the remote VM URL as the documented deployment target.
- [ ] Run focused daemon and provider tests and verify they pass.
- [ ] Commit with `feat: register OmniRoute as a Multica provider`.

### Task 5: Verification, documentation, and live execution test

**Files:**
- Modify: `README.md` or the existing provider documentation location
- Modify: provider configuration example if one exists
- Test: `server/pkg/agent/omniroute_test.go`

- [ ] Add a non-mutating live test script or documented command that checks authenticated `/v1/models` and sends a minimal tool-call request against the Tailscale VM.
- [ ] Do not run the live test until `OMNIROUTE_API_KEY` has been rotated and injected after the previous exposure.
- [ ] Run `go test ./...`.
- [ ] Run `go build ./...`.
- [ ] Run `pnpm lint`, `pnpm typecheck`, and `pnpm test` from the repository root.
- [ ] Run the authenticated `/models` probe against the Tailscale endpoint without printing the key.
- [ ] Run the non-mutating execution test with the selected OmniRoute model and verify a complete Multica result, at least one structured tool call, and one tool result.
- [ ] Verify no em dash characters were introduced in changed prose, comments, or commit messages.
- [ ] Document the remote endpoint requirement, model capability caveat, key injection, and local endpoint fallback behavior.
- [ ] Commit with `docs: document OmniRoute provider operation`.

## Execution checkpoints

- After Tasks 1 through 3, review the provider API and MCP boundary before adding daemon wiring.
- After Task 4, review the full local test suite and inspect the diff for credential leakage.
- Task 5 is the only task allowed to contact the live OmniRoute VM, and only after a rotated key is available.
