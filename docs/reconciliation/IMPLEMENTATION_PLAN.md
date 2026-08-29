# Multica Reconciliation Implementation Plan

Date: 2026-08-29

Status: approved for execution from the Phase 1 QA boundary

Base branch: `CCRBrad/multica-upstream-reconciliation`

Protected application base: `fork/main` at `240d4b9bb69df1d2fb1bf179668216b7c68d48c1`

Upstream checkpoint: `origin/main` at `64ec7f54163d918d5d7fd4dcae857f241b7842d0`

## Objective

Deliver all three required tracks on the current upstream architecture:

1. Harden and verify the replacement provider runtime system.
2. Preserve coding, operational, and hybrid workflow modes, then add enforceable provider-neutral operational controls.
3. Preserve and retire superseded OmniRoute, replacement-runtime, operational, report, and worktree state without losing recoverable artifacts.

No upstream merge is needed at the verified checkpoint because `fork/main` is zero commits behind and two commits ahead of `origin/main`.

## Non-negotiable invariants

1. OmniRoute code, provider IDs, migrations, environment contracts, and documentation do not return.
2. The 48-path provider runtime delta on `fork/main` remains the implementation base.
3. Provider credentials remain daemon-owned values. They never enter durable runtime profiles, task payloads, argv, browser responses, logs, Git, or test snapshots.
4. Coding, operational, and hybrid modes are product workflow intent only. They never grant, deny, approve, isolate, or audit a tool call.
5. Tool policy, schema-digest matching, approval, consumption, and audit are enforced independently of mode.
6. No new database foreign keys, references, cascades, or nonconcurrent indexes.
7. One schema worker owns all new migration numbers, handwritten SQL, and generated sqlc output.
8. Operational records contain metadata only. No argument values, result values, headers, URLs, command lines, environment values, or provider error bodies are persisted or returned.
9. Default tests never contact real providers or execute ambient user-installed agent CLIs.
10. Every behavior change follows RED, GREEN, REFACTOR. The worker must record the failing test command and expected failure before production code.
11. New prose, code comments, and commit messages contain no em dash characters.
12. Workers may edit only their assigned ownership surface. Cross-surface changes require a coordinator handoff and a serialized follow-up.

## Report conflict resolution

`DO_NOT_OVERWRITE.md` protects agent operating modes as custom product behavior. `TEAM_DELTA_OPERATIONAL_CONTROLS.md` rejects them as a security-control mechanism. Both conclusions are retained:

- The operating-mode product slice will port the three choices, API validation, prompt identity, repository-context behavior, duplication behavior, draft behavior, and localized UI.
- The operational-controls slice will never inspect operating mode when authorizing a tool call.
- An operational or hybrid selection may offer a default policy template in the UI, but activation requires an explicit policy transaction and cannot weaken an existing policy.
- The mode field will be named `operating_mode` to avoid collision with provider `runtime_mode` concepts.

## Git and integration protocol

Each implementation task uses a fresh top-level Orca worktree based on the latest pushed reconciliation branch. Each worker:

1. Confirms the expected base commit before editing.
2. Writes and runs a failing test first.
3. Implements the minimum behavior needed to pass.
4. Runs focused tests, formatters, `git diff --check`, and the zero-em-dash scan.
5. Commits only its assigned files.
6. Reports the commit, RED evidence, GREEN evidence, files changed, and unresolved risks.

The coordinator reviews and cherry-picks one task at a time into the integration branch, reruns focused checks after every pick, pushes the branch, then creates the next dependent worktree from that new tip. Workers never merge each other.

## Wave 0: preservation checkpoint and contract freeze

### W0-A: private preservation archive

Owner: coordinator only

Dependencies: Phase 1 QA complete, audit and planning workers stopped

Actions:

- Re-run the live ref, worktree, stash, dirty-file, ignored-file, and exact-tip inventory.
- Create a private full Git bundle with `--all`, verify it, and record its SHA-256 checksum.
- Create local archival tags for the protected provider, operational, Phase 3, OmniRoute, replacement-runtime, and stash tips.
- Export the replacement-runtime dirty file as a binary patch.
- Export the Phase 3 stash, including its untracked third parent, as a patch and tar archive.
- Copy the ignored Phase 3 environment file only to a permission-`0600` private directory without reading or publishing its contents.
- Secret-scan public archive artifacts before any later publication. The private environment copy is never scanned into logs or published.

Proof:

- `git bundle verify` succeeds.
- The checksum manifest exists.
- Every protected exact tip resolves through the bundle or a local archival tag.
- Both dirty artifact exports are nonempty.
- The private directory and environment copy have restrictive permissions.

### W0-B: contract freeze

Owner: coordinator

The following first-release contracts are frozen before code workers start:

- Operating modes: `coding`, `operational`, `hybrid`, default `coding`.
- Policy effects: `allow`, `require_approval`; absence preserves compatibility; an existing policy defaults to deny.
- Exact rule identity: transport kind, server key, tool name, schema digest.
- Approval states: pending, approved, consumed, denied, expired, cancelled.
- Enforcement release one: managed remote MCP only.
- Capability names are transport-scoped and provider-family-scoped.
- Retention default: 90 days for terminal approval state and action events, configurable by server setting.
- Realtime event: `operational_controls:changed`, workspace ID only, owner/admin recipients only.
- Public config gate: `operational_controls_v1`, default false.

## Wave 1: parallel foundations

Wave 1 contains two non-overlapping implementation tasks.

### W1-A: provider security and harness foundation

Exclusive ownership:

- `server/pkg/agent/provider_catalog.go`
- `server/pkg/agent/provider_catalog_test.go`
- `server/pkg/agent/openai_compatible.go`
- `server/pkg/agent/openai_compatible_test.go`
- `server/pkg/agent/opencode_api.go`
- OpenCode API route tests in `server/pkg/agent/`
- Native provider sanitizer helpers and provider-specific tests in `server/pkg/agent/`
- `server/pkg/agent/provider_live_smoke_integration_test.go`
- New provider-specific `agentintegration` fixtures in `server/pkg/agent/`
- `packages/core/runtimes/provider-catalog.ts`
- `packages/core/runtimes/provider-catalog.test.ts`

Forbidden in this task:

- `server/internal/daemon/**`
- migrations or generated SQL
- agent handler or operating-mode files
- operational-control APIs or UI

Test-first requirements:

1. Hosted credentials reject an untrusted host unless a trusted operator override is explicitly enabled.
2. Selected models are revalidated against discovery before execution.
3. Unknown OpenCode Zen and Go model families fail closed.
4. Baseline capabilities exclude unproven usage, tools, and MCP.
5. Native provider stderr and provider errors redact canary secrets before logs and result fields.
6. The live harness requires explicit provider and model, constructs an allowlisted environment, drains channels, closes transports, and asserts non-leak without printing the secret.
7. Native OpenCode appears in the frontend catalog.

Focused GREEN gate:

```text
go test ./pkg/agent -run 'Test(Provider|APIProvider|OpenAICompatible|OpenCode|LiveProvider|Sanit)' -count=1
go test -race ./pkg/agent -run 'Test(Provider|APIProvider|OpenAICompatible|OpenCode|LiveProvider|Sanit)' -count=1
go vet ./pkg/agent
pnpm --filter @multica/core test -- provider-catalog
```

### W1-B: reconciled schema foundation

Exclusive ownership:

- New migrations allocated from the live next free prefix after 441
- `server/pkg/db/queries/agent.sql`
- New handwritten policy, rule, revision, approval, action-event, retention, and dashboard query files
- All sqlc-generated files caused by this schema task
- Schema-focused integration tests and migration tests

This worker owns the only migration allocation. It includes:

- `agent.operating_mode` with a database vocabulary check and default `coding`.
- `agent_tool_policy`.
- `agent_tool_policy_revision`.
- `agent_tool_policy_rule`.
- `agent_tool_approval_request`.
- `agent_tool_action_event`.
- Concurrent indexes and later constraint attachment in separate single-statement migration files.

Forbidden in this task:

- handlers, router, daemon, MCP transport, frontend, prompts, and runtime briefs
- legacy migrations 134 through 138
- copied generated SQL from any side branch

Test-first requirements:

1. Duplicate migration prefixes fail.
2. Fresh install and upgrade from migration 441 succeed.
3. Static checks reject foreign keys, references, cascades, and nonconcurrent index creation.
4. Every operational query contains a direct `workspace_id` predicate.
5. Idempotent approval creation returns the existing row only when immutable identity matches.
6. Cursor ordering, expiry selection, retention deletion, and explicit cleanup are deterministic.

Focused GREEN gate:

```text
make sqlc
make test
git diff --check
```

## Wave 2: product behavior, provider daemon integration, and control services

Wave 2 starts only after both Wave 1 commits are integrated. It contains three tasks with explicit file boundaries.

### W2-A: operating-mode product slice

Exclusive ownership:

- Agent handler request, response, create, update, duplicate, and claim mapping for `operating_mode`
- `server/internal/daemon/types.go`
- mode-only additions in `server/internal/daemon/daemon.go`
- `server/internal/daemon/prompt.go`
- `server/internal/daemon/execenv/execenv.go`
- `server/internal/daemon/execenv/runtime_config.go`
- `server/internal/daemon/execenv/runtime_config_sections.go`
- the explicitly mode-gated built-in operational workflow skill and source map
- `packages/core/types/agent.ts`
- agent draft, stored draft, duplicate seed, and builder protocol mode fields and tests
- current shared agent creation and inspector UI
- four locale surfaces

Forbidden in this task:

- provider construction, provider credentials, provider discovery, MCP broker, policy authorization, approval, audit, migrations, or generated SQL

Test-first requirements:

1. Invalid values fail API validation.
2. Missing stored values behave as coding.
3. Operational mode omits repository brief sections and uses business-task identity.
4. Hybrid retains repository context and adds operational identity.
5. Coding behavior remains unchanged.
6. Duplicate and unfinished draft flows preserve the selected mode.
7. The operational workflow skill is attached only for operational and hybrid modes.
8. UI cards, inspector changes, keyboard behavior, and localization work on web and desktop shared surfaces.

### W2-B: policy and metadata-only audit services

Exclusive ownership:

- New provider-neutral policy and action-event service packages
- Dedicated policy and action handlers
- Handler tests
- Additive router registrations for policy and action endpoints
- Agent and workspace explicit cleanup service changes
- No daemon or frontend files

Test-first requirements:

1. Owner/admin policy replacement is human-only and revision-guarded.
2. Agent owner has read-only access only to owned-agent effective policy and action metadata.
3. Member, agent, task, daemon, and cross-workspace actors cannot mutate policy.
4. Policy replacement is wholesale, canonical, exact, and cancels older unconsumed approvals atomically.
5. Metadata-only action events reject raw values and secret canaries.
6. Audit write failures fail closed at security transitions.

### W2-C: provider discovery and observability integration

Exclusive ownership:

- `server/internal/daemon/provider_discovery.go`
- provider-probe tests
- provider-specific portions of `server/internal/daemon/daemon.go` only after W2-A is integrated
- runtime-profile provider availability responses and sanitized offline-reason handlers
- daemon tests for model revalidation and capability filtering

Forbidden in this task:

- mode prompt behavior
- operational policy or approval enforcement
- migrations, sqlc, or frontend provider catalog

Test-first requirements:

1. Failed optional probes preserve a sanitized offline reason and do not hide healthy providers.
2. Failed probes retry on refresh.
3. Daemon credential precedence remains server-owned and value-secret.
4. Task launch revalidates provider, route, model, and verified capabilities.
5. Hosted untrusted endpoints fail before a credential is attached.

## Wave 3: approval state machine, client contracts, and managed transport gate

Wave 3 begins after W2-B and W2-C are integrated. Three tasks may run in parallel only where their ownership does not overlap.

### W3-A: approval backend state machine

Exclusive ownership:

- Approval service and sweeper packages
- Operator approval handlers
- Daemon control-plane approval handlers
- Additive route registration for approval endpoints
- Backend approval, race, authorization, retention, and rollback tests

Required behavior:

- Idempotent create-or-get with immutable identity comparison.
- Human-only approve or deny.
- Guarded expiry and cancellation.
- Exactly-once atomic consumption.
- Policy replacement versus consumption revalidation.
- Decision, `activity_log`, and action event in one transaction.
- No free-form notes or raw summaries.

### W3-B: core API and realtime contracts

Exclusive ownership:

- New `packages/core` policy, action, approval, capability, and operations schemas
- API client methods
- workspace-scoped React Query keys and options
- `operational_controls:changed` validation and cache invalidation
- protected-cache eviction on role downgrade
- malformed-response tests with sanitized diagnostics

Forbidden in this task:

- shared views, app routes, Go server, Zustand server-state storage

### W3-C: managed remote MCP pre-transport gate

Exclusive ownership:

- `server/internal/daemon/remote_mcp_broker.go`
- managed MCP policy and approval helper files
- transport-scoped capability advertisement
- invocation ID propagation through managed tool-use and tool-result paths
- broker and daemon tests

Required enforcement order:

1. Capability.
2. Exact allowlist identity.
3. Exact schema digest.
4. Approval requirement.
5. Atomic consume.
6. Pre-forward started event commit.
7. Tool transport.
8. Terminal event and task message commit.

The mock upstream must receive zero calls for deny, pending, expired, cancelled, schema drift, audit failure, unsupported transport, and duplicate consume. It must receive exactly one call after one valid consumption under concurrent retries.

## Wave 4: operator surfaces and operations reporting

Wave 4 starts after Wave 3 is integrated.

### W4-A: shared approval queue and policy UI

Exclusive ownership:

- Shared approval queue and structured policy views in `packages/views`
- Agent access or MCP surface integration
- Web `/approvals` page
- Desktop route and navigation entry
- owner/admin permissions and accessible interaction tests
- locale changes for these views

Required behavior:

- React Query owns server state.
- Decisions are non-optimistic.
- Stale `409`, expiry, offline, unsupported, permission, loading, and empty states are explicit.
- Only metadata is rendered.
- Keyboard navigation, focus return, accessible names, and restrained announcements pass.

### W4-B: operations dashboard backend and UI

This is serialized if it touches files owned by W4-A.

Ownership:

- Operations aggregate handlers and tests
- `packages/core/dashboard` operations schemas and queries
- shared `/usage?tab=operations` view and tests
- web and desktop shared routing adapters as needed

Required metrics:

- Pending current count.
- Approved, denied, expired, failed, and intercepted counts by requested window.
- Declaration-only counts shown separately.
- Median decision time.
- Capability gaps.
- Agent-level and recent metadata-only views.

## Wave 5: end-to-end verification and authorized live evidence

### W5-A: managed MCP end-to-end fixture

Use a deterministic non-mutating localhost MCP fixture with one exact tool. Prove:

- Pending, denied, expired, cancelled, duplicate consume, and schema drift cause zero upstream calls.
- Approved then consumed causes exactly one upstream call.
- Policy, approval, action ledger, transcript, queue, and dashboard converge.
- A secret canary is absent from database rows, API responses, logs, WebSocket payloads, and UI text.

### W5-B: provider live campaign

Live tests remain opt-in and run serially.

- Recheck credentials by variable name and boolean only.
- Run local Ollama only with an explicit already-installed model and no model pull.
- Run LM Studio only if a listener and explicit loaded model exist.
- Run hosted and subscription providers only when their own required credential or authenticated CLI is present and the fixture is provider-specific.
- Record only exact commit, provider ID, model ID, marker matched boolean, duration, terminal status, and separately proven capability booleans.
- Never infer tools, MCP, usage, or resume from a baseline text completion.

### W5-C: full repository gate

Required commands, adjusted only when the repository script itself selects a superset:

```text
make test
go test -race on changed approval, daemon, handler, and agent packages
go vet ./...
pnpm typecheck
pnpm test
pnpm lint
pnpm build
make check
pnpm exec playwright test for changed web and desktop flows
git diff --check
zero-em-dash scan of changed lines
forbidden OmniRoute and legacy migration scan
```

## Wave 6: reconciliation report, PR, merge, and cleanup

### W6-A: `RECONCILIATION_REPORT.md`

The report must include:

1. Exact upstream and fork heads, merge base, ahead/behind counts, and total historical upstream commits absorbed.
2. Every custom provider and operational behavior preserved, rewritten, rejected, or superseded.
3. Status of provider hardening, operating modes, policy, audit, approvals, queue, dashboard, live evidence, and branch retirement.
4. Migration allocation and database verification.
5. Focused, race, full repository, integration, and live test evidence.
6. Known unavailable live providers stated as unavailable, never implied green.
7. Preservation archive path, checksums, tags, and cleanup decisions without secret content.

### W6-B: review and merge

- Commit and push every integrated change.
- Open one PR from the reconciliation branch to `fork/main`.
- Wait for all required GitHub checks and review bots.
- Resolve findings with tests first.
- Merge only when required checks are green.
- Verify the remote `fork/main` merge SHA and that `origin/main` is still an ancestor or record the newly arrived upstream delta and reconcile it before merge.

### W6-C: guarded retirement

After merge, worker quiescence, and verified archives:

1. Refresh the full worktree and ref inventory.
2. Verify the private bundle and artifact checksums.
3. Record commit-by-commit acceptance or rejection for historical operational and replacement-runtime lines.
4. Clean or preserve dirty worktrees without reset or force removal.
5. Remove merged Orca worktrees with normal `git worktree remove` or repository `make remove-worktree` when they own a database.
6. Delete merged remote report and implementation branches with exact-tip force-with-lease guards.
7. Delete merged local branches with lowercase `-d`.
8. Delete intentionally rejected unmerged branch names only after exact-tip, archival-tag, and bundle-verification guards.
9. Rename stale local `main` to an archive name, then recreate local `main` from verified `fork/main`.
10. Quarantine the legacy primary checkout last. Do not recursively delete it.

Final cleanup proof:

- `git worktree list` contains only accepted active worktrees.
- `git branch -vv` has no merged dead implementation branches.
- `git ls-remote --heads fork` has no merged report or feature branches.
- `fork/main` contains the merge commit and all required report artifacts.
- The private preservation archive still verifies after cleanup.

## Completion definition

The reconciliation is complete only when all three tracks are implemented or truthfully marked unavailable for lack of an external credential or service, all mandatory tests are green, `RECONCILIATION_REPORT.md` is merged to `fork/main`, remote state is verified, and every branch or worktree eligible for retirement has been removed under the preservation guards above.
