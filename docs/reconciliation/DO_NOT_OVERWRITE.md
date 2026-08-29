# Multica fork preservation manifest

Verified against `fork/main` at `240d4b9bb69df1d2fb1bf179668216b7c68d48c1` on 2026-08-29.

This file is the merge guard for reconciling the CCRBrad fork with upstream. A clean upstream file, a newer timestamp, or a broad conflict resolution is not sufficient reason to replace a protected fork behavior. Resolve conflicts at the symbol and invariant level, then rerun the focused tests named here.

## Status vocabulary

| Status | Meaning | Reconciliation treatment |
|---|---|---|
| `SHIPPED_ON_FORK_MAIN` | Present in `fork/main` through `cdba54800` or `240d4b9bb` | Preserve unless a reviewed replacement proves equivalent behavior and security. |
| `SIDE_BRANCH_COMMITTED` | Committed on the operational branch through `da4bd0380`, but not present on `fork/main` | Preserve the intent as a port candidate. Do not take whole-file conflict resolutions or old migrations. |
| `HISTORICAL_DIRTY` | Uncommitted work found in an inspected worktree or archived stash | Evidence only. Never promote without a fresh design and implementation review. |
| `SUPERSEDED` | Earlier replacement-runtime work whose supported behavior is implemented more completely on `fork/main` | Keep the fork implementation. Do not restore the older files. |
| `OBSOLETE_OMNIROUTE` | OmniRoute-specific implementation or documentation | Exclude. Do not copy, merge, or use as the provider abstraction. |
| `DOCUMENTATION_ONLY` | Design or plan without a complete reviewed implementation | May inform a new design, but is not shipped behavior. |

## Evidence and ancestry ledger

| Ref | Classification | What it represents | Rule |
|---|---|---|---|
| `240d4b9bb` | `SHIPPED_ON_FORK_MAIN` | Current fork tip. Adds opt-in live provider smoke coverage and documentation on top of the provider runtime work. | This commit and its parent `cdba54800` are the preservation baseline. |
| `cdba54800` | `SHIPPED_ON_FORK_MAIN` | Provider runtime catalog, adapters, daemon discovery, credential custody, runtime profiles, provider UI, tests, and migration 441. | Preserve the security and capability invariants even when upstream files have moved. |
| `7c93031e6` | `SIDE_BRANCH_COMMITTED` | Intermediate allowed-tools API plumbing in the operational branch. | Do not treat this intermediate commit as a complete feature. Use the cumulative operational tip for intent. |
| `da4bd0380` | `SIDE_BRANCH_COMMITTED` | Cumulative operational and hybrid modes, action logging, allowed-tools enforcement, UI, tests, and tailored workflow. | Port manually after current-schema review. It is not merged into `fork/main`. |
| `ff970fd28` | `OBSOLETE_OMNIROUTE` | Cumulative OmniRoute provider implementation built after the operational branch. | Exclude every OmniRoute-specific delta. Operational ancestors remain separate port candidates. |
| `284facd68` | `DOCUMENTATION_ONLY` | Approval queue and operations dashboard design document layered on the obsolete OmniRoute line. Its text says operator-approved design, pending written-spec review. | Treat only as design evidence. It is not an implementation or authorization to copy the stash. |
| `d60346841` | `SUPERSEDED` | Earlier OpenRouter replacement-runtime slice and output redaction work. | Preserve equivalent fork behavior from `cdba54800`; do not restore this side branch wholesale. |
| archived `refs/stash` at `0f5927be` | `HISTORICAL_DIRTY` | Obsolete OmniRoute edits plus an incomplete approval queue schema and generated code. | Do not apply the stash. Extract no code without a new reviewed design. |

The complete operational commit series is:

```text
079c99c6c feat: operational and hybrid agent modes (Phase 1)
2a7b5f663 feat(agent): add agent_action_log audit table (Phase 3 part 1)
84cc1b7ea feat(agent): add agent_action_log data layer (sqlc queries + generated code)
9ecba3dbb feat(agent): write agent_action_log on task complete and fail (Phase 3 audit write)
0868fa1a5 feat(agent): add allowed_tools column and data layer (Phase 3 tool allowlist)
7c93031e6 feat(agent): persist and expose allowed_tools via API (Phase 3 allowlist plumbing)
da4bd0380 feat(agent): complete operational agent controls and audit
```

All seven commits returned `+` from `git cherry -v fork/main da4bd0380`, so none is patch-equivalent on `fork/main`. The archived worktree was clean at `7c93031e6`. The current worktree was also clean at inspection. The replacement-runtime worktree had one uncommitted modification in `packages/core/types/agent.ts`. That dirty provider descriptor list is superseded by the shipped catalog in `packages/core/runtimes/provider-catalog.ts` and must not be copied.

Reproducible provenance commands:

```bash
git rev-parse HEAD fork/main origin/main
git merge-base fork/main da4bd0380
git cherry -v fork/main da4bd0380
git log --reverse --format='%h %s' a06fc273406d75ff67b07a052333c99870be2d39..da4bd0380
git range-diff 64ec7f54163d918d5d7fd4dcae857f241b7842d0..d60346841 64ec7f54163d918d5d7fd4dcae857f241b7842d0..cdba54800
git -C /Users/bradstrawbridge/.agents/workspaces/_archive/multica status --short --branch
git stash show --name-only --include-untracked refs/stash
```

Branch-name hazard: the local branch named `main` points to obsolete OmniRoute commit `ff970fd28`. The only baseline called `main` in this manifest is the remote-tracking ref `fork/main` at `240d4b9bb`.

## 1. Shipped provider runtime system

All items in this section are `SHIPPED_ON_FORK_MAIN`.

### 1.1 Provider identities and fail-closed capability catalog

Protected backend files and symbols:

- `server/pkg/agent/provider_catalog.go`
  - `ProviderKind`
  - `ProviderCapability`
  - `ProviderDescriptor`
  - `ProviderAPIConfig`
  - `providerCatalog`
  - `ProviderCatalog`
  - `ProviderByID`
  - `ProviderSupportsCapability`
  - `IsProviderType`
  - `IsAPIProvider`
  - `ProviderCredentialEnvAllowed`
  - `ValidateProviderAPIBaseURL`
  - `ResolveProviderAPIProfileConfig`
  - `ResolveProviderAPIConfig`
  - `validateAPIBaseURL`
  - `isLoopbackHost`
- `server/pkg/agent/provider_catalog_test.go`
- `server/pkg/agent/agent.go`, especially provider dispatch in `New` and API fields on `Config`
  - `Config.APIBaseURL`
  - `Config.APIKey`
  - `Config.DefaultModel`

Protected provider identities:

- Native CLI providers: `codex`, `claude`, `antigravity`, `cursor`, `grok`, and `opencode`.
- API providers: `opencode-api`, `opencode-zen`, `opencode-go`, `openrouter`, `vercel-ai-gateway`, `ollama`, `lmstudio`, and `nvidia-nim`.

Protected frontend files and symbols:

- `packages/core/runtimes/provider-catalog.ts`
  - `RuntimeProviderSetup`
  - `RuntimeProviderExecution`
  - `RuntimeProviderCapability`
  - `RuntimeProviderDescriptor`
  - `REPLACEMENT_RUNTIME_PROVIDERS`
  - `ReplacementRuntimeProviderID`
  - `replacementRuntimeProvider`
- `packages/core/runtimes/provider-catalog.test.ts`
- `packages/core/runtimes/index.ts`
- `packages/core/types/agent.ts`, especially runtime profile protocol-family and API metadata types

Protected behavior:

- Capability checks fail closed. A catalog entry does not prove that a provider is configured, healthy, or allowed to perform every action.
- The reviewed API-provider capability set includes prompt, streaming, completion, cancellation, model discovery, usage, tools, and MCP. It deliberately does not claim reasoning or session resume.
- The backend catalog is the execution authority. The frontend catalog is curated setup guidance, not an independent permission source.
- Native `opencode` currently exists in the backend catalog but not in the curated provider support card. Reconcile that difference deliberately instead of mechanically forcing both lists to match.

Conflict rule: keep the fork catalog and its tests as the authority for provider IDs, provider kinds, capability gates, credential allowlists, and endpoint policy. Any upstream provider registry must be mapped into these invariants before old symbols are removed.

### 1.2 Shared OpenAI-compatible execution adapter

Protected files and symbols:

- `server/pkg/agent/openai_compatible.go`
  - `openAICompatibleBackend`
  - `newOpenAICompatibleBackend`
  - `ListAPIModels`
  - `Execute`
  - `statusForAPIContext`
  - `sanitizeProviderOutput`
  - `safeProviderHTTPClient`
  - `doStreamingRequest`
  - `doChatCompletionsStreamingRequest`
  - endpoint construction helpers
  - `newAPIHTTPMCPServers`
  - `validateAPIHTTPMCPURL`
  - `apiMCPCallName`
  - MCP `initialize`, `call`, and `rpc` paths
- `server/pkg/agent/openai_compatible_test.go`
- `server/pkg/agent/agent.go`
- `server/internal/daemon/remote_mcp_broker.go`, especially `providerSupportsRemoteMCPBroker`

Protected behavior:

- Streaming requires a terminal `[DONE]` frame and uses bounded scanner frames and bounded model-list bodies.
- Model discovery is deterministic and deduplicates results.
- A task supports cancellation, status reporting, usage and reasoning events, streamed text, and a maximum of 20 tool turns.
- Provider output, error text, and tool output pass through `sanitizeProviderOutput` so the configured credential and shared diagnostic secret patterns are redacted.
- API execution is stateless and does not claim session resume support.
- A model is required before execution.
- HTTP MCP supports only the reviewed remote transports and uses qualified tool names. Unknown tools and unsupported transports fail closed.
- Redirects are refused by the provider HTTP client.
- `validateAPIHTTPMCPURL` rejects malformed URLs and embedded credentials but currently accepts any absolute HTTP or HTTPS URL. It does not implement the stronger hosted-HTTPS or local-loopback-only provider endpoint rule.

Conflict rule: never replace this adapter with OmniRoute code or a generic upstream HTTP client that permits redirects, unbounded responses, missing completion sentinels, silent resume claims, unqualified MCP tools, or unredacted provider output. Preserve the reviewed HTTP-only remote MCP boundary. Any stronger MCP URL policy must be a focused hardening change rather than code imported from OmniRoute. Behavioral equivalence must be shown by focused tests before symbol replacement.

### 1.3 OpenCode endpoint-family routing

Protected files and symbols:

- `server/pkg/agent/opencode_api.go`
  - `apiProtocol`
  - `providerModelAPIProtocol`
- `server/pkg/agent/opencode_streams.go`
  - `doResponsesStreamingRequest`
  - `doAnthropicStreamingRequest`
  - `doProviderStreamRequest`
  - `scanProviderSSE`
  - `checkProviderHTTPResponse`
  - provider endpoint helpers
- `server/pkg/agent/openai_compatible_test.go`

Protected behavior:

- OpenCode Zen and Go route Claude and `qwen3` model families through the Anthropic Messages shape.
- GPT, Grok, and Muse families route through the Responses shape.
- Other supported models use chat completions.
- Gemini models are rejected until a reviewed Gemini adapter exists.

Conflict rule: preserve explicit protocol routing and negative tests. Do not collapse these paths into a universal chat-completions assumption and do not advertise Gemini support from model discovery alone.

### 1.4 Daemon discovery, credential custody, and runtime execution

Protected files and symbols:

- `server/internal/daemon/provider_discovery.go`
  - `probeAPIProviders`
  - `providerEnvironment`
  - `apiProviderModelEnv`
  - `defaultProbeAPIProviderEndpoint`
  - `modelsEndpoint`
- `server/internal/daemon/agents_probe.go`, especially `probeAgentCLIs`
- `server/internal/daemon/agents_probe_api_test.go`
- `server/internal/daemon/daemon.go`
  - `profileAPIEntry`
  - `profileAPIEntries`
  - `recordProfileAPIEntry`
  - `clearProfileAPIEntriesForWorkspace`
  - `apiProfileForRuntime`
  - `appendProfileRuntimes`
  - `providerCapabilitiesForRegistration`
  - `providerNeedsInlineSystemPrompt`
  - `profileSetSignature`
  - API branches in model listing and `runTask`
  - API provider resume-state clearing
- `server/internal/daemon/daemon_test.go`
- `server/internal/daemon/config.go`
- `server/internal/daemon/client.go`, especially `RuntimeProfile`
- `server/internal/daemon/types.go`, especially API metadata on `AgentEntry` and the private API credential field
- `server/internal/daemon/remote_mcp_broker.go`
- `server/internal/handler/daemon.go`
  - `splitProviderCapabilities`
  - `verifiedProviderCapabilities`
  - provider capability metadata on registration
- `server/internal/handler/daemon_test.go`

Protected security boundaries:

- The daemon owns the resolved endpoint and credential value. Runtime profiles, agent records, API responses, logs, and UI store only a credential environment variable name, never a secret value.
- API profile data is kept in daemon memory and scoped to its workspace. Removal of a workspace clears the matching cached entries.
- `isBlockedEnvKey` prevents agent custom environment data from overriding provider endpoints, provider credential names, token names, or any `MULTICA_*` control variable.
- Provider registration accepts only capabilities verified for that provider. Daemon claims are filtered rather than trusted.
- API endpoint validation requires an absolute HTTP or HTTPS URL and rejects user information, query strings, fragments, backslashes, and parent-path traversal.
- Plain HTTP is restricted to loopback. Local providers are restricted to loopback HTTP endpoints.
- Discovery and model probes refuse redirects, have time and response-size bounds, and do not block daemon startup when an optional provider is unavailable.
- Provider probes run concurrently with a three-second bound and a one MiB response limit.
- The daemon can start without a native CLI when a healthy API provider exists. Hosted providers require an approved credential, while loopback Ollama and LM Studio profiles may be keyless.
- API runtimes skip executable healing, CLI version probing, and CLI model-selection probing. All API providers receive the runtime brief inline.
- Agent model choice overrides a profile or daemon default without exposing provider configuration to task-controlled environment merging.
- API providers do not retain a resumable task session ID.

Conflict rule: these security boundaries win over upstream convenience behavior. If daemon initialization or task execution is refactored, preserve daemon-only credential resolution, workspace scoping, environment blocklists, capability filtering, endpoint validation, redirect refusal, probe bounds, optional-provider isolation, and API resume clearing.

### 1.5 Runtime profile schema, handlers, and API-provider UI

Protected storage and handler files:

- `server/migrations/441_runtime_profile_api_provider.up.sql`
- `server/migrations/441_runtime_profile_api_provider.down.sql`
- `server/pkg/db/queries/runtime_profile.sql`
- `server/pkg/db/generated/runtime_profile.sql.go`
- `server/pkg/db/generated/models.go`, only the runtime-profile fields generated from the shipped schema
- `server/internal/handler/runtime_profile.go`
  - `RuntimeProfileResponse`
  - `normalizeRuntimeProfileAPIFields`
  - `validateRuntimeProfileModel`
  - API-aware create, update, list, and delete behavior
- `server/internal/handler/runtime_profile_handler_test.go`

Protected UI files and symbols:

- `packages/views/runtimes/components/runtime-profile-catalog.ts`
  - `runtimeFamilyLabel`
  - `isAPIProfileFamily`
  - `ProfileFormValues`
  - `validateProfileForm`
- `packages/views/runtimes/components/runtime-profile-catalog.test.ts`
- `packages/views/runtimes/components/runtime-profiles-dialog.tsx`, especially `ProfileFormView` and `ProfileDetailsForm`
- `packages/views/runtimes/components/runtime-profiles-dialog.test.tsx`
- `packages/views/runtimes/components/runtime-row-menu.test.tsx`
- `packages/views/runtimes/components/provider-support-card.tsx`
- `packages/views/runtimes/components/index.ts`
- `packages/views/runtimes/components/runtimes-page.tsx`
- `packages/views/locales/en/runtimes.json`
- `packages/views/locales/ja/runtimes.json`
- `packages/views/locales/ko/runtimes.json`
- `packages/views/locales/zh-Hans/runtimes.json`

Protected behavior:

- Migration 441 adds `api_base_url`, `credential_env`, and `default_model`, expands the allowed protocol families, and enforces the CLI-versus-API transport shape.
- No credential value is persisted.
- Credential environment names are checked against the provider-specific allowlist.
- Default model identifiers are limited to 512 characters and reject NUL or newline input.
- API profiles have no command or fixed arguments. CLI profiles do not receive API-only metadata.
- Protocol family remains immutable during profile edits.
- Existing workspace scoping and owner or administrator authorization remain in force.
- Profile visibility is forced to workspace scope because private-profile reads are not completely enforced, and create or update requests trigger daemon refresh.
- The API form accepts endpoint, credential environment name, and default model. It hides CLI command and fixed-argument inputs and never becomes a secret-key input.
- The detail view displays only nonsecret profile metadata.
- Provider support guidance appears on both empty and populated runtime pages and remains localized in all four shipped locale files.
- The support card is catalog guidance, not live provider availability or health.

Known UX gap: an API-profile edit cannot clear a previously stored optional credential reference or default model because empty fields are omitted from the update patch. Preserve the secret-free form contract, not this clearing defect.

Conflict rule: preserve the schema meaning and validation even if an upstream migration supersedes migration number 441. Never downgrade the form to accept a secret value. If the schema must be reissued, use a new migration compatible with the target branch rather than replaying or renumbering 441 silently.

### 1.6 Configuration contract and live smoke test

Protected files and symbols:

- `.env.example`, names only
- `CLI_AND_DAEMON.md`
- `docs/superpowers/specs/2026-08-28-provider-runtime-support-design.md`
- `docs/superpowers/plans/2026-08-28-provider-runtime-support.md`
- `server/pkg/agent/provider_live_smoke_integration_test.go`
  - build tag `agentintegration`
  - `TestConfiguredAPIProviderSmoke`
  - `providerSmokeEnv`
  - `containsProviderModel`

Protected configuration names, with all values intentionally omitted:

- `OPENCODE_API_BASE_URL`, `OPENCODE_API_KEY`, `OPENCODE_API_TOKEN`, `MULTICA_OPENCODE_API_MODEL`
- `OPENCODE_ZEN_BASE_URL`, `OPENCODE_ZEN_API_KEY`, `OPENCODE_ZEN_TOKEN`, `MULTICA_OPENCODE_ZEN_MODEL`
- `OPENCODE_GO_BASE_URL`, `OPENCODE_GO_API_KEY`, `OPENCODE_GO_TOKEN`, `MULTICA_OPENCODE_GO_MODEL`
- `OPENROUTER_BASE_URL`, `OPENROUTER_API_KEY`, `MULTICA_OPENROUTER_MODEL`
- `AI_GATEWAY_BASE_URL`, `AI_GATEWAY_API_KEY`, `VERCEL_OIDC_TOKEN`, `MULTICA_VERCEL_AI_GATEWAY_MODEL`
- `OLLAMA_BASE_URL`, `OLLAMA_API_KEY`, `MULTICA_OLLAMA_MODEL`
- `LMSTUDIO_BASE_URL`, `LMSTUDIO_API_KEY`, `MULTICA_LMSTUDIO_MODEL`
- `NVIDIA_NIM_BASE_URL`, `NVIDIA_API_KEY`, `MULTICA_NVIDIA_NIM_MODEL`
- `MULTICA_RUN_REAL_PROVIDER_SMOKE`, `MULTICA_PROVIDER_SMOKE_PROVIDER`, `MULTICA_PROVIDER_SMOKE_MODEL`

Protected behavior:

- The live test is opt-in, discovers the selected model, then performs one completion.
- It has a 90-second timeout, defaults to a local Ollama path when explicitly enabled without a different selection, sends one marker prompt, and verifies the marker.
- It is excluded from default test execution because it requires an explicitly configured real provider and may consume quota.
- Configuration documentation describes environment variable names and ownership without exposing values.

Conflict rule: retain opt-in gating and the model-discovery precondition. Never make a real-provider call part of a default unit-test run. The implementation and reviewed documentation are the authority for nonsecret defaults; this manifest intentionally records no endpoint or credential values.

### 1.7 Complete shipped file inventory

The complete custom delta from the inspected upstream baseline to `fork/main` is protected:

```text
.env.example
CLI_AND_DAEMON.md
docs/superpowers/plans/2026-08-28-provider-runtime-support.md
docs/superpowers/specs/2026-08-28-provider-runtime-support-design.md
packages/core/agents/mcp-support.test.ts
packages/core/agents/mcp-support.ts
packages/core/runtimes/index.ts
packages/core/runtimes/provider-catalog.test.ts
packages/core/runtimes/provider-catalog.ts
packages/core/types/agent.ts
packages/views/locales/en/runtimes.json
packages/views/locales/ja/runtimes.json
packages/views/locales/ko/runtimes.json
packages/views/locales/zh-Hans/runtimes.json
packages/views/runtimes/components/index.ts
packages/views/runtimes/components/provider-support-card.tsx
packages/views/runtimes/components/runtime-profile-catalog.test.ts
packages/views/runtimes/components/runtime-profile-catalog.ts
packages/views/runtimes/components/runtime-profiles-dialog.test.tsx
packages/views/runtimes/components/runtime-profiles-dialog.tsx
packages/views/runtimes/components/runtime-row-menu.test.tsx
packages/views/runtimes/components/runtimes-page.tsx
server/internal/daemon/agents_probe.go
server/internal/daemon/agents_probe_api_test.go
server/internal/daemon/client.go
server/internal/daemon/config.go
server/internal/daemon/daemon.go
server/internal/daemon/daemon_test.go
server/internal/daemon/provider_discovery.go
server/internal/daemon/remote_mcp_broker.go
server/internal/daemon/types.go
server/internal/handler/daemon.go
server/internal/handler/daemon_test.go
server/internal/handler/runtime_profile.go
server/internal/handler/runtime_profile_handler_test.go
server/migrations/441_runtime_profile_api_provider.down.sql
server/migrations/441_runtime_profile_api_provider.up.sql
server/pkg/agent/agent.go
server/pkg/agent/openai_compatible.go
server/pkg/agent/openai_compatible_test.go
server/pkg/agent/opencode_api.go
server/pkg/agent/opencode_streams.go
server/pkg/agent/provider_catalog.go
server/pkg/agent/provider_catalog_test.go
server/pkg/agent/provider_live_smoke_integration_test.go
server/pkg/db/generated/models.go
server/pkg/db/generated/runtime_profile.sql.go
server/pkg/db/queries/runtime_profile.sql
```

Conflict rule: this inventory is a review boundary, not an instruction to choose the fork version of every whole file. For shared files such as `daemon.go`, `agent.go`, `models.go`, and `packages/core/types/agent.ts`, merge current upstream changes around the protected symbols and invariants.

## 2. Operational agent controls not yet on fork/main

The cumulative implementation at `da4bd0380` is `SIDE_BRANCH_COMMITTED`. It is valuable custom work, but it predates the shipped provider-runtime architecture and current migration rules. Preserve its behavior as a manual port candidate only.

### 2.1 Coding, operational, and hybrid agent modes

Protected intent and exact areas:

- `packages/core/types/agent.ts`: specifically `Agent.mode`, `CreateAgentRequest.mode`, and `UpdateAgentRequest.mode`.
- `packages/views/agents/components/inspector/mode-picker.tsx`: `ModePicker` and `MODE_OPTIONS`.
- `packages/views/agents/components/create-agent-dialog.tsx`: three mode choices and default coding behavior.
- `packages/views/agents/components/agent-detail-inspector.tsx` and `agent-overview-pane.tsx`: mode display and editing.
- `packages/views/locales/{en,ja,ko,zh-Hans}/agents.json`: mode labels, descriptions, and operational copy.
- `server/internal/handler/agent.go`: validation and persistence of `coding`, `operational`, or `hybrid`.
- `server/internal/handler/daemon.go`: mode on claimed `TaskAgentData`.
- `server/internal/daemon/types.go`: `AgentData.Mode`.
- `server/internal/daemon/daemon.go`: propagation into `TaskContextForEnv.AgentMode`.
- `server/internal/daemon/prompt.go`:
  - `BuildPrompt`
  - `buildMetaSkillContent`
  - `writeHeader`
  - `writeRepositories`
- `server/internal/daemon/execenv/execenv.go`
- `server/internal/daemon/execenv/runtime_config.go`
- `server/internal/daemon/execenv/runtime_config_sections.go`
- `server/migrations/134_agent_mode.up.sql` and `.down.sql`
- `server/pkg/db/queries/agent.sql` and the generated files from that branch

Protected behavior:

- `coding` is the compatibility default.
- `operational` removes repository-oriented prompt sections and supplies business-workflow instructions.
- `hybrid` retains coding context and operational instructions.
- Duplicating an agent inherits the source agent's mode.
- Chat, comment, autopilot, and quick-create paths return before the assignment-prompt mode switch in `BuildPrompt`; they receive only the mode-aware runtime brief.
- Invalid mode values fail validation rather than silently becoming more permissive.
- Missing or unknown stored values fall back to coding behavior in prompt generation.
- This side implementation is advisory prompt behavior. It does not prevent repository checkout, filesystem access, shell execution, or use of a coding provider.

Known gaps:

- No mode-specific tests were added.
- The mode field can be confused with existing `runtime_mode` and provider routing modes.
- The side migration has no database check constraint for the mode vocabulary.
- Operational mode is not an execution-isolation boundary.

Conflict rule: port the concept, validation, prompt behavior, and UI semantics into the current handler and daemon shapes, preferably with an unambiguous field name such as `operating_mode`. Do not copy migration 134 or generated SQL files. Allocate a fresh migration and regenerate sqlc output from current queries. If the product intends operational mode to be a security boundary, add tested capability and execution gates rather than relying on prompt wording.

### 2.2 Provider-aware allowed-tools enforcement

Protected intent and exact areas:

- `server/internal/handler/agent_allowed_tools.go`
  - `parseAllowedTools`
  - allowed-tool normalization, limits, and clear behavior
- `server/internal/handler/agent_allowed_tools_test.go`
- `server/internal/handler/agent.go`
- `server/internal/daemon/agent_tools.go`
  - `decodeAllowedTools`
  - capability-aware enforcement
- `server/internal/daemon/agent_tools_test.go`
- `server/pkg/agent/agent.go`
  - `ExecOptions.AllowedTools`
  - explicit configured-state tracking
  - `SupportsToolAllowlist`
- `server/pkg/agent/claude.go` and `claude_test.go`
- `server/pkg/agent/codebuddy.go` and `codebuddy_test.go`
- `packages/views/agents/components/tabs/allowed-tools-tab.tsx`
  - `AllowedToolsTab`
- `packages/core/api/client.ts`
- `packages/core/types/agent.ts`
- `server/migrations/136_agent_allowed_tools.up.sql` and `.down.sql`
- `server/pkg/db/queries/agent.sql`, especially `ClearAgentAllowedTools`, and generated output from that branch

Protected behavior:

- Database `NULL` or omitted configuration means unrestricted compatibility behavior.
- An explicit empty JSON array means deny all tools. This distinction must survive API, storage, daemon, and provider translation.
- Parsing trims and deduplicates entries, accepts at most 128 patterns, and caps each pattern at 256 bytes.
- A configured allowlist on a provider without verified enforcement support fails closed.
- Only Claude and CodeBuddy were verified in the side implementation.
- Provider-owned flags are injected by the daemon, and agent custom arguments cannot override either spelling of the allowlist flag.
- The current side UI converts a blank editor to `null`, so it cannot create the documented explicit deny-all state.
- Adapter tests cover argument construction, not live provider enforcement. CodeBuddy flag semantics in current `fork/main` must be revalidated before porting the older comma-joined form.
- The explicit empty array becomes an empty native CLI flag value in the side adapters, but no test proves Claude or CodeBuddy interpret that value as deny all.
- The side response exposes `allowed_tools` more broadly than the existing owner-only treatment of the Composio toolkit allowlist, so response authorization and redaction need reconciliation.
- The old tab is always visible and is not gated by editing permission or verified provider capability.
- Obsolete OmniRoute used `mcp__server__tool` names while the shipped API adapter uses `server__tool`. Stored patterns need a single provider-neutral naming contract before porting.

Conflict rule: port the `NULL` versus empty-array contract and fail-closed capability gate, then integrate with the shipped provider capability catalog through a distinct `tool-allowlist` enforcement capability. For API providers, filter tools before advertising them to the model and check the policy again immediately before invocation. Do not mark all API providers as allowlist-capable. Add a visible, testable deny-all UI state and prove deny-all provider by provider, or refuse execution when it cannot be guaranteed. Reconcile owner-only response rules, reverify each CLI's real flag semantics, define one portable tool-name contract, and do not reuse the old migration or overwrite the shipped `agent.go` and `packages/core/types/agent.ts` provider changes.

### 2.3 Agent action audit log and UI

Protected intent and exact areas:

- `server/internal/handler/agent_action_log.go`
  - `taskMessageAuditSummary`
  - `truncateAgentActionSummary`
  - `AgentActionResponse`
  - `ListAgentActions`
- `server/internal/handler/agent_action_log_test.go`
- `server/internal/handler/daemon.go`
  - `logAgentAction`
  - lifecycle write points in `CompleteTask` and `FailTask`
  - tool-event write points in `ReportTaskMessages`
- `server/cmd/server/router.go`: `GET /api/agents/{id}/actions`
- `server/pkg/db/queries/agent_action_log.sql`
  - `CreateAgentActionLog`
  - `CreateAgentToolActionLog`
  - `ListAgentActionsByAgent`
  - `ListRecentAgentActions`
- `server/migrations/135_agent_action_log.up.sql` and `.down.sql`
- `server/migrations/137_agent_action_log_task.up.sql` and `.down.sql`
- `packages/core/api/client.ts`: action-list client method
- `packages/core/types/agent.ts`: `AgentAction`
- `packages/views/agents/components/tabs/actions-tab.tsx`
- `packages/views/agents/components/agent-detail-inspector.tsx`

Protected behavior:

- Records tool-use and tool-result events plus `task:completed` and `task:failed` lifecycle events.
- Summaries are redacted and bounded to 4096 bytes.
- Only tool-message rows are idempotent through `(task_id, message_seq)`. Completion and failure rows can duplicate.
- Listing defaults to 100 rows and caps at 500.
- Action history is restricted to the existing `canManageAgent` owner or administrator boundary.
- Audit writes are best effort in the old implementation and do not corrupt task execution on a logging failure.
- Tool-use and tool-result events become separate rows and are not correlated into one completed call.
- Lifecycle rows lack task identifiers, and provider streaming channels can drop events before `ReportTaskMessages` receives them.
- The side schema uses text identifiers and lacks a direct workspace identifier.
- The old table has no retention policy or explicit application-layer orphan cleanup.

Current partially superseding infrastructure:

- `server/internal/handler/daemon.go`, especially `ReportTaskMessages`, already redacts and persists tool messages.
- `server/pkg/db/queries/task_message.sql` stores task-message input and output.
- `packages/views/common/task-transcript/` already renders and folds tool-use and tool-result pairs.
- Task completion and failure are already represented on the task record.

Conflict rule: preserve the operational audit view, bounded workspace and agent aggregates, authorization, redaction, event coverage, idempotency, and append-only intent. Prefer the existing task-message and task-lifecycle records as source data. Add a separate read model only if aggregation performance requires it. If a new table is still necessary, use current UUID, workspace, task, and call-ID contracts; specify application-layer cleanup, bounded retention, current redaction helpers, and whether best-effort writes honestly satisfy the audit requirement. A compliance-grade trail needs persistence at the provider call boundary or a reliable outbox, not only lossy stream messages. Do not copy old migration numbers, old generated files, or whole handler files over newer upstream code.

### 2.4 Tailored operational workflow skill

Protected intent and exact file:

- `server/internal/service/builtin_skills/multica-operational-workflow/SKILL.md`

Protected workflow:

1. The file is non-user-invocable and declares `Bash(multica *)` in its frontmatter.
2. Read the issue and its comments.
3. Use approved MCP business tools instead of raw API calls to perform the assigned operational task.
4. Post a concise result comment.
5. Move the work item to done only after execution evidence exists, or update its status honestly when execution cannot be completed.
6. Do not modify code unless the issue explicitly asks for code changes.

Known gaps:

- No code in the side branch conditionally attaches or injects this skill only when an agent changes to operational or hybrid mode. Built-in skills are appended through `TaskService.BuiltinSkills`, so registration without a condition could expose it globally.
- Its example posts an agent-authored comment with inline `--content`, which conflicts with the current file-first `--content-file` safety rule.
- Frontmatter is skill metadata, not a global runtime sandbox.

Conflict rule: preserve this tailored workflow when operational mode is ported, but wire mode activation explicitly and test it. Rewrite examples against current CLI guidance, source-map conventions, and `--content-file`. Review its tool names and authorization assumptions against the current MCP registry. Keep product-generic examples in Multica core and private CCR system examples outside the upstream product unless deliberately productized. Do not install it independently while operational mode and allowlist enforcement are absent.

### 2.5 Operational UI and localization review

Side-branch UI changes include the mode cards in `create-agent-dialog.tsx`, `ModePicker` in the inspector, `actions-tab.tsx`, `allowed-tools-tab.tsx`, their registration in `agent-overview-pane.tsx`, and agent locale additions in English, Japanese, Korean, and Simplified Chinese.

Known gaps:

- Creation-mode copy is localized, but inspector mode labels, action content, allowed-tools content, and new tab labels include hard-coded English.
- Both new tab components disable literal-string linting.
- Mode cards lack explicit radio semantics or `aria-pressed` state.
- Action rows omit timestamp, task or issue navigation, tool-call correlation, and pagination.
- The allowed-tools placeholder is tied to a private BuilderLync example.
- No frontend tests cover mode choice, allowlist editing, deny-all state, audit authorization, or the new tabs.

Conflict rule: rebuild these surfaces against the current access picker, MCP tab, component tests, package boundaries, and full i18n conventions. Preserve the concepts, not the stale component files, literal strings, or accessibility defects. Do not replace the current create-agent dialog wholesale.

### 2.6 Complete operational side-branch inventory

The cumulative custom surface at `da4bd0380` is:

```text
packages/core/api/client.ts
packages/core/types/agent.ts
packages/core/types/index.ts
packages/views/agents/components/agent-detail-inspector.tsx
packages/views/agents/components/agent-overview-pane.tsx
packages/views/agents/components/create-agent-dialog.tsx
packages/views/agents/components/inspector/mode-picker.tsx
packages/views/agents/components/tabs/actions-tab.tsx
packages/views/agents/components/tabs/allowed-tools-tab.tsx
packages/views/locales/en/agents.json
packages/views/locales/ja/agents.json
packages/views/locales/ko/agents.json
packages/views/locales/zh-Hans/agents.json
server/cmd/server/router.go
server/internal/daemon/agent_tools.go
server/internal/daemon/agent_tools_test.go
server/internal/daemon/daemon.go
server/internal/daemon/execenv/execenv.go
server/internal/daemon/execenv/runtime_config.go
server/internal/daemon/execenv/runtime_config_sections.go
server/internal/daemon/prompt.go
server/internal/daemon/types.go
server/internal/handler/agent.go
server/internal/handler/agent_action_log.go
server/internal/handler/agent_action_log_test.go
server/internal/handler/agent_allowed_tools.go
server/internal/handler/agent_allowed_tools_test.go
server/internal/handler/daemon.go
server/internal/service/builtin_skills/multica-operational-workflow/SKILL.md
server/migrations/134_agent_mode.down.sql
server/migrations/134_agent_mode.up.sql
server/migrations/135_agent_action_log.down.sql
server/migrations/135_agent_action_log.up.sql
server/migrations/136_agent_allowed_tools.down.sql
server/migrations/136_agent_allowed_tools.up.sql
server/migrations/137_agent_action_log_task.down.sql
server/migrations/137_agent_action_log_task.up.sql
server/pkg/agent/agent.go
server/pkg/agent/agent_test.go
server/pkg/agent/claude.go
server/pkg/agent/claude_test.go
server/pkg/agent/codebuddy.go
server/pkg/agent/codebuddy_test.go
server/pkg/db/generated/agent.sql.go
server/pkg/db/generated/agent_action_log.sql.go
server/pkg/db/generated/models.go
server/pkg/db/queries/agent.sql
server/pkg/db/queries/agent_action_log.sql
```

Conflict rule: use this list for completeness checks only. Shared files must be ported at symbol level, migrations must be redesigned, and all generated files must be regenerated from the reconciled current schema.

## 3. Superseded replacement-runtime slice

The five-commit OpenRouter slice ending at `d60346841` is `SUPERSEDED`, not missing work. Its relevant behavior is covered more broadly on `fork/main` by `cdba54800` and `240d4b9bb`.

Side-branch files:

```text
docs/superpowers/plans/2026-08-28-replacement-runtime-openrouter-slice.md
docs/superpowers/specs/2026-08-28-replacement-provider-capability-gates.md
server/pkg/agent/agent.go
server/pkg/agent/builtin_runtimes.go
server/pkg/agent/openai_compatible.go
server/pkg/agent/openai_compatible_test.go
server/pkg/agent/provider_catalog.go
server/pkg/agent/provider_catalog_test.go
```

Preserved outcome on `fork/main`:

- A multi-provider backend and frontend catalog replaces the narrow OpenRouter-only catalog.
- The shared HTTP adapter includes streamed execution, MCP, provider protocol routing, endpoint safety, and credential redaction.
- `sanitizeProviderOutput` and `TestOpenAICompatibleExecuteRedactsAPISecretsFromStreamedOutput` retain the security intent of the side-branch redaction commit.
- Capability gates are explicit and fail closed.

Historical dirty exclusion:

- The replacement-runtime worktree had an uncommitted edit to `packages/core/types/agent.ts` adding a second provider descriptor list.
- Dirty symbols are `RUNTIME_PROVIDER_IDS`, `RuntimeProviderId`, `RuntimeProviderExecutionFamily`, `RuntimeProviderSetupMode`, `RUNTIME_PROVIDER_CAPABILITIES`, `RuntimeProviderStatus`, and `RuntimeProviderDescriptor`.
- That draft has 12 identities, omits `opencode-zen` and `opencode-go`, and conflicts with both the shipped 14-identity Go catalog and the shipped 13-identity frontend guidance catalog.
- The shipped `packages/core/runtimes/provider-catalog.ts` is the curated frontend authority. Do not copy the dirty constants or create two competing catalogs.

Conflict rule: choose the shipped fork implementation for all overlapping symbols. Retain `Config.APIBaseURL`, `Config.APIKey`, `Config.DefaultModel`, and `New` dispatch from `fork/main`; do not restore the side branch's `ProviderBaseURL`, `ProviderAPIKey`, or `ResolveBackend` API. The side docs may be retained as history only if clearly labeled superseded. They must not be used to narrow or overwrite the shipped multi-provider behavior.

## 4. Explicitly excluded OmniRoute work

Everything in this section is `OBSOLETE_OMNIROUTE`. No OmniRoute code, migration, environment contract, UI capability entry, provider ID, session mechanism, or direct MCP registry may be copied into the reconciled branch.

Obsolete commit series: `53cf3566b`, `9a7cc9d3a`, `7a0000f8c`, `34be9e31d`, `c895331a6`, `16aac3121`, `934ea114a`, `9dac84eb8`, `ccbf1de5a`, and `ff970fd28`.

Excluded committed files from the OmniRoute delta between `da4bd0380` and `ff970fd28`:

```text
README.md
docs/superpowers/plans/2026-08-27-omniroute-provider.md
docs/superpowers/specs/2026-08-27-omniroute-provider-design.md
packages/core/agents/mcp-support.test.ts
packages/core/agents/mcp-support.ts
server/internal/daemon/config.go
server/internal/daemon/daemon.go
server/internal/daemon/daemon_test.go
server/migrations/135_omniroute_runtime_profile.down.sql
server/migrations/135_omniroute_runtime_profile.up.sql
server/pkg/agent/agent.go
server/pkg/agent/agent_supported_types_test.go
server/pkg/agent/agent_test.go
server/pkg/agent/models.go
server/pkg/agent/omniroute.go
server/pkg/agent/omniroute_live_test.go
server/pkg/agent/omniroute_mcp.go
server/pkg/agent/omniroute_mcp_test.go
server/pkg/agent/omniroute_models.go
server/pkg/agent/omniroute_models_test.go
server/pkg/agent/omniroute_test.go
```

Complete archived stash inventory, including untracked files:

```text
.env.example
README.md
README.zh-CN.md
docs/superpowers/plans/2026-08-27-omniroute-provider.md
docs/superpowers/specs/2026-08-27-omniroute-provider-design.md
server/internal/handler/agent.go
server/internal/handler/agent_allowed_tools.go
server/internal/handler/agent_allowed_tools_test.go
server/migrations/138_agent_approval_queue.down.sql
server/migrations/138_agent_approval_queue.up.sql
server/pkg/agent/omniroute_live_test.go
server/pkg/agent/omniroute_mcp.go
server/pkg/agent/omniroute_mcp_test.go
server/pkg/agent/omniroute_models.go
server/pkg/agent/omniroute_models_test.go
server/pkg/db/generated/agent.sql.go
server/pkg/db/generated/agent_approval_request.sql.go
server/pkg/db/generated/models.go
server/pkg/db/queries/agent.sql
server/pkg/db/queries/agent_approval_request.sql
```

The OmniRoute paths above are obsolete. The handler, migration, query, and generated approval paths are incomplete and are governed by section 5. No configuration value from `.env.example` was inspected or recorded.

Excluded behavior:

- OmniRoute provider registration and models.
- Direct OmniRoute OpenAI-compatible transport.
- OmniRoute-specific SSE parsing.
- Session header discovery and persisted OmniRoute session identifiers.
- Direct HTTP and stdio OmniRoute MCP registries and tool loops.
- OmniRoute model discovery, live tests, environment configuration, README instructions, and UI MCP-support claims.
- Migration `135_omniroute_runtime_profile`, which also collides numerically with the operational action-log migration line.

Superseding implementation:

- Use `server/pkg/agent/openai_compatible.go`, `opencode_api.go`, `opencode_streams.go`, and `provider_catalog.go` from `fork/main`.
- Use daemon-owned profile resolution, provider discovery, and `remote_mcp_broker.go` from `fork/main`.
- Use `packages/core/agents/mcp-support.ts` from `fork/main` without restoring an OmniRoute provider entry.

Conflict rule: if a conflict hunk contains both valuable operational work and OmniRoute work, reconstruct only the operational symbols against the shipped fork file. Never accept the full side-branch hunk.

## 5. Incomplete approval queue and dashboard

There is no complete approval queue implementation to preserve. The area has two separate statuses:

- `docs/superpowers/specs/2026-08-28-agent-approval-queue-and-operations-dashboard-design.md` at `284facd68` is `DOCUMENTATION_ONLY`.
- The document's own status is `Design approved by operator, pending written-spec review`.
- The archived stash contains `HISTORICAL_DIRTY` schema and query work that was never completed or reviewed as a shippable system.

Historical dirty files that must not be copied blindly:

```text
server/internal/handler/agent.go
server/internal/handler/agent_allowed_tools.go
server/internal/handler/agent_allowed_tools_test.go
server/migrations/138_agent_approval_queue.down.sql
server/migrations/138_agent_approval_queue.up.sql
server/pkg/db/generated/agent.sql.go
server/pkg/db/generated/agent_approval_request.sql.go
server/pkg/db/generated/models.go
server/pkg/db/queries/agent.sql
server/pkg/db/queries/agent_approval_request.sql
```

Design intent that may be reconsidered in a fresh provider-neutral design:

- Tool patterns can identify calls that require approval, with exact and reviewed wildcard matching.
- A pending request is fail closed until a qualified human owner or administrator approves it.
- Task actors cannot approve their own request.
- Requests need explicit expiry, idempotency, decision, and one-time consumption semantics.
- Stored argument summaries must be redacted and bounded.
- Operations staff need a dashboard for pending, decided, expired, and recently consumed requests.

Why the dirty implementation is excluded:

- It has schema and query fragments but no complete provider-neutral execution bridge.
- The `approval_required_tools` column plumbing is partial and has no complete migration in the runnable branch.
- It has no complete route and handler surface for approve, deny, expire, consume, and list workflows.
- It has no provider callback or `ToolApprovalHandler` boundary and no enforced human-actor middleware.
- It has no complete daemon polling or event mechanism and no end-to-end approval pause and resume behavior.
- It has no complete dashboard implementation or end-to-end test suite.
- A `cancelled` state is named without a complete cancellation query or workflow.
- The draft create query uses `ON CONFLICT DO NOTHING RETURNING *`, which does not return the existing request as the design promises.
- Draft aggregates have no route or UI consumer.
- The draft table has no direct workspace identifier, and approve or deny queries have no workspace predicate. They rely on authorization handlers that were never written.
- The draft migration uses database foreign keys and cascading or set-null actions, which violate current repository migration rules.
- The draft migration creates indexes nonconcurrently and groups them with other statements, which violates the rule that each concurrent index build has its own single-statement migration file.
- Migration number 138 already belongs to `138_issue_title_trgm_index` in current history. Side migration numbers 134 through 137 also collide with unrelated current migrations.
- The design was written on the obsolete OmniRoute line and contains provider-specific assumptions that must not enter the shared provider abstraction.

Conflict rule: exclude migration 138, its queries, generated files, and handler hunks. If approval control is authorized later, start with a new provider-neutral design, use current migration numbering, direct workspace scoping, human-actor middleware, transactional one-time consumption, application-layer cleanup, no foreign keys or cascade actions, and one concurrent index per single-statement migration. Prove cross-workspace denial plus pending, approve, deny, expire, cancel, and consume paths end to end before adding UI claims.

## 6. Migration and generated-code rules

| Area | Old artifact | Status | Reconciliation rule |
|---|---|---|---|
| API runtime profiles | `441_runtime_profile_api_provider` | `SHIPPED_ON_FORK_MAIN` | Preserve its schema meaning. If target history conflicts, replace through a reviewed forward migration rather than silently dropping fields. |
| Agent mode | `134_agent_mode` | `SIDE_BRANCH_COMMITTED` | Do not cherry-pick. Reissue with a fresh number against the current agent table. |
| Action log | `135_agent_action_log`, `137_agent_action_log_task` | `SIDE_BRANCH_COMMITTED` | Redesign against current audit needs and issue fresh migration numbers. Its indexes also need separate concurrent builds. |
| Allowed tools | `136_agent_allowed_tools` | `SIDE_BRANCH_COMMITTED` | Preserve null-versus-empty semantics in a fresh migration. |
| OmniRoute | `135_omniroute_runtime_profile` | `OBSOLETE_OMNIROUTE` | Exclude completely. |
| Approval queue | `138_agent_approval_queue` | `HISTORICAL_DIRTY` | Exclude. Number 138 already belongs to `138_issue_title_trgm_index`; the draft also violates current relationship and index rules and lacks a complete runtime. |

Generated files such as `server/pkg/db/generated/models.go`, `agent.sql.go`, `agent_action_log.sql.go`, `agent_approval_request.sql.go`, and `runtime_profile.sql.go` are outputs, not merge authorities.

Exact number collisions in current history:

| Number | Side purpose | Current purpose |
|---|---|---|
| 134 | agent mode | runtime profile Qoder |
| 135 | action log and, on another side line, OmniRoute profile | comment workspace index |
| 136 | allowed tools | runtime profile Trae CLI |
| 137 | action-log task idempotency | `pg_trgm` extension |
| 138 | dirty approval queue | issue title trigram index |

Conflict rule: reconcile migrations and source queries first, then run the repository's current sqlc generation workflow. Never solve a generated-file conflict by selecting the side branch wholesale.

## 7. Highest-risk conflict map

The ten files changed by both the Phase 3 line and the shipped fork provider commits are:

```text
packages/core/agents/mcp-support.test.ts
packages/core/agents/mcp-support.ts
packages/core/types/agent.ts
server/internal/daemon/config.go
server/internal/daemon/daemon.go
server/internal/daemon/daemon_test.go
server/internal/daemon/types.go
server/internal/handler/daemon.go
server/pkg/agent/agent.go
server/pkg/db/generated/models.go
```

Preserve the fork symbols first, then add approved controls manually. Thirty-five further Phase 3 paths overlap newer upstream work, so whole-branch or whole-file selection is unsafe even outside this direct ten-file set.

| Protected area | Competing changes | Required resolution |
|---|---|---|
| `server/internal/daemon/daemon.go` | Shipped provider credential custody, discovery, capability filtering, and API task execution overlap side operational mode, allowlists, and obsolete OmniRoute paths. | Keep all shipped provider security paths. Manually add only approved operational behavior. Reject every OmniRoute hunk. |
| `server/pkg/agent/agent.go` | Shipped API `Config` and backend dispatch overlap side `ExecOptions` allowlists and obsolete OmniRoute provider construction. | Merge individual fields and interfaces. Keep shipped API factory behavior, port allowlist state only with capability tests, and exclude OmniRoute. |
| `server/internal/handler/agent.go` | Current handler changes overlap side mode and allowed-tools fields plus dirty approval-required fields. | Reimplement approved fields against the current request and authorization model. Do not copy the old file or dirty approval code. |
| `server/internal/handler/daemon.go` | Shipped verified provider capabilities overlap side action-audit write points. | Keep capability filtering and registration metadata. Add audit hooks only after current event and failure semantics are reviewed. |
| `packages/core/types/agent.ts` | Shipped runtime-profile API types overlap side mode and allowlist types plus a dirty duplicate provider catalog. | Current fork types win. Add approved operational fields explicitly and reject duplicate provider constants. |
| `packages/core/agents/mcp-support.ts` | Shipped API-provider support overlaps obsolete OmniRoute support. | Preserve shipped provider entries and never restore OmniRoute. |
| `server/pkg/db/generated/models.go` | Shipped profile fields, side operational fields, and dirty approval fields all collide. | Never hand-merge. Reconcile migrations and queries, then regenerate. |
| migration namespace | Side migrations 134 through 138 collide with current history and with each other at 135. | Allocate new numbers on the target branch, follow concurrent-index rules, and exclude OmniRoute and approval queue migrations. |
| runtime profile UI | Shipped nonsecret API fields can be lost in broad upstream dialog rewrites. | Preserve the endpoint, credential-name, and default-model flow, protocol immutability, API versus CLI input separation, tests, and all four locales. |
| operational controls UI | Side tabs and mode picker predate current shared-view structure. | Port through current package boundaries and localization patterns. Do not paste stale components or literal strings. |

## 8. Reconciliation acceptance checklist

A reconciliation is not safe until all statements below are true:

- [ ] `fork/main` provider IDs, capability gates, and backend or frontend catalog roles are still represented.
- [ ] API credentials are resolved only in the daemon and no secret value is stored or returned by runtime-profile APIs.
- [ ] Endpoint validation, loopback policy, redirect refusal, probe limits, custom-environment blocklists, and output redaction remain tested.
- [ ] OpenCode protocol routing and explicit Gemini rejection remain tested.
- [ ] API providers remain stateless for resume and optional provider failures do not block daemon startup.
- [ ] Remote MCP transport and tool-name restrictions remain fail closed.
- [ ] Runtime profile API metadata, migration meaning, handler authorization, UI fields, tests, and four locale files survive.
- [ ] Real-provider smoke coverage remains opt-in and is not part of the default test path.
- [ ] No OmniRoute provider ID, file, migration, environment contract, session logic, or MCP logic was restored.
- [ ] No approval queue code, migration 138, generated file, or dashboard claim was copied from the dirty stash.
- [ ] Any operational-mode port retains coding-default compatibility and mode-specific prompt behavior.
- [ ] Any allowlist port preserves `NULL` as unrestricted and explicit empty array as deny all, with provider capability enforcement.
- [ ] Any action-log port retains owner or administrator authorization, redaction, size limits, list limits, event coverage, and idempotency.
- [ ] Side migrations were redesigned with fresh numbers, no foreign keys or cascade actions, and each concurrent index in its own single-statement migration.
- [ ] sqlc outputs were regenerated instead of conflict-resolved manually.
- [ ] Focused backend, daemon, handler, frontend catalog, runtime profile, MCP support, and UI tests pass on the reconciled tree.

## Final merge directive

The shipped provider runtime system on `fork/main` is the protected base. Operational modes, provider-aware allowed tools, action history, and the operational workflow skill are unmerged candidates whose behavior should be ported deliberately after current-schema review. The earlier replacement-runtime slice is superseded, all OmniRoute work is obsolete, and the approval queue is incomplete design and dirty evidence only.
