# Team Alpha Upstream Audit

Audit date: 2026-08-29

Scope: read-only reconciliation of `origin/main`, `fork/main`, the custom operational baseline `7c93031e63d2fa230344751eff7335e86c42cd85`, and the related custom runtime branches. Application code was not changed.

## Executive finding

No upstream commits remain unabsorbed by `fork/main` at the refreshed refs. `origin/main` is an ancestor of `fork/main`, the merge base is exactly the current `origin/main` tip, and `fork/main` is two custom commits ahead and zero commits behind.

This does not mean the old operational-mode work is absorbed. The custom baseline is a sibling line, not an ancestor of either current main. It is six commits ahead of its historical merge base while current upstream is 1,186 commits ahead of that same merge base. The old operational branch should be treated as a source of requirements and tests, not as a branch to merge wholesale.

## Ref evidence after refresh

Both remotes were refreshed read-only with `git fetch --prune <remote> main` before this evidence was collected at `2026-08-29T21:39:19Z`.

| Ref | Exact object | Commit time | Subject |
| --- | --- | --- | --- |
| `origin/main` | `64ec7f54163d918d5d7fd4dcae857f241b7842d0` | `2026-08-28T18:00:35+08:00` | `MUL-6737: fix(timeouts): close the two gaps the 2h inactivity budget left open (#7699)` |
| `fork/main` | `240d4b9bb69df1d2fb1bf179668216b7c68d48c1` | `2026-08-29T15:57:29-04:00` | `test: add live provider smoke coverage` |
| custom baseline | `7c93031e63d2fa230344751eff7335e86c42cd85` | `2026-07-06T09:17:10-04:00` | `feat(agent): persist and expose allowed_tools via API (Phase 3 allowlist plumbing)` |

Exact graph checks:

```text
merge-base origin/main fork/main:
64ec7f54163d918d5d7fd4dcae857f241b7842d0

git rev-list --left-right --count origin/main...fork/main:
0  2

origin/main ancestor of fork/main:
yes

fork/main ancestor of origin/main:
no
```

The two fork-only commits are:

1. `cdba5480000c1a49ceb1a03c3fc52befdcbbaaab` - `feat: add provider runtime support`
2. `240d4b9bb69df1d2fb1bf179668216b7c68d48c1` - `test: add live provider smoke coverage`

Conclusion: the prior estimate of a large unabsorbed upstream gap is stale for the current main refs. There is no upstream merge or rebase to perform now.

## Historical baseline relationship

The baseline and current upstream diverged at:

```text
a06fc273406d75ff67b07a052333c99870be2d39
```

Exact symmetric count for `7c93031e...origin/main`:

```text
6  1186
```

The six commits belong only to the custom operational line. The 1,186 commits belong only to upstream. `git merge-base --is-ancestor 7c93031e... origin/main` and the same check against `fork/main` both return false.

For that reason, the notation `7c93031e..origin/main` is useful as a set of upstream commits absent from the custom baseline, but it is not a linear fast-forward range.

## Historical upstream delta classification

### Scale

The range contains 1,186 upstream-only commits dated from `2026-07-03T11:57:34+08:00` through `2026-08-28T18:00:35+08:00`.

```text
4,302 files changed
680,306 insertions
82,500 deletions
606 migration files changed: 596 added, 6 deleted, 4 modified
```

Subject-keyword signals below overlap by design. They measure how often commit subjects directly advertise a category, not an exclusive partition of all 1,186 commits.

| Category | Subject signal count | Audit classification |
| --- | ---: | --- |
| Security and access control | 58 | Dependency CVEs, secret redaction, production auth hardening, runtime/task authorization, sandboxing, credential handling |
| Performance | 58 | Database indexes, bounded scans and replay, batching, cache and fanout reduction, CI caching |
| Architecture | 55 | Plugin system rebuild, Public API v1, daemon workflow consolidation, frontend cleanup, runtime-management restructuring |
| Database | 52 | Schema evolution, migration safety, index lifecycle, UUIDv7 adoption, task and runtime ledgers |
| Daemon and runtime | 402 | Agent backends, runtime discovery, process ownership, session lifecycle, timeout policy, MCP wiring, model catalogs |
| Frontend | 326 | Web, desktop, mobile, chat, agent, runtime, issue, editor, and settings surfaces |
| Tests and CI | 76 | Test isolation and determinism, race fixes, CI sharding/path filters, generated-code and migration gates |

### Security fixes

Representative high-value changes:

- `658b0b7d9f8b3c1444fa160ce67cf7521e0eb5ec` fails fast in production when JWT secrets use insecure defaults.
- `3c3e77b00c14fc0159870948f1a23af6ea3d37af` redacts secrets from provider command logs.
- `a3bd38da6a6915879e1e1eec74051a4888539e5d` redacts autopilot webhook credentials.
- `c599f47ba42b6d8baa3d6f97467aa10c0fa1b4a9` expands redaction to GitHub fine-grained PATs and Google API keys.
- `5f767d671a0b153dbeaa77bea75177e3c1734807` covers additional credential formats that had escaped redaction.
- `bbba9c5460d45803a559ffd7af7238f3628490dc` enforces runtime access on task claims, while `2284420966f12a192d343b6c36675c0dd6e57f2c` authorizes squad activity using task provenance.
- `8f48c380d496fbd1418e266c7d61a9cd97618883` enables Electron renderer sandboxing.
- `8f176e96f37a7a794d7992d151fabfc63e145770`, `62c5ba119eedbc0912c848ebfdf8df5709522326`, `79d13b6b79f60b41af77640494b1fccf322adb60`, and `4c1c8709c3010b18a7c88073f27c95f16e7ac394` address named dependency vulnerabilities.
- `e5e278a1a26f07d390e23001aa37a6382b27154e` and later runtime-specific equivalents move prompts to stdin so CLI-shaped prompt content cannot be re-tokenized as flags.

These changes are already present in `fork/main`. Any reconstruction of provider or operational behavior must preserve the current redaction, auth, sandbox, and prompt-delivery boundaries.

### Performance work

Representative changes:

- `3fecc553e24cffd55eebd034c470bfbffc6f0193` adds an index for GitHub PR head SHA lookups.
- `6e2b2877b7aa3164e8de8d6891b36f5809c321f4` reduces runtime sweeper scan cost.
- `003a505b87a287e988f84cc7520b558b3f7a55ad` reduces stale dispatch reclaim load.
- `115bfab7592a880e16b0a0b7e4b24e1a26661ba0` persists task message batches in one statement.
- `0c0baf050552da7442a3f05cd8b442770330579e` stops task-message realtime fanout from flooding every client.
- `ea07a291387abdcff2a4286ddfb3f07a0bdc4932` uses UUIDv7 for task-message IDs, part of broader append-heavy table locality work.
- `d4dac0e77c727d1408daada45aeb3068cdbe33ad` moves latest-terminal lookup onto an index-backed path.
- `982a8c578470d7916b27ca7bb2ee7dc2574eb0b8` splits frontend CI jobs and caches Turborepo results; `4040345e09d058d5f79891c7c4d023bdc0a1461c` path-filters backend CI jobs.

Operational-mode integration should not reintroduce broad runtime scans, per-row action-log loops, or unbounded event retrieval. Action auditing should use bounded queries and targeted indexes designed under current migration rules.

### Architecture changes

The upstream interval is a substantial architecture transition, not a patch train:

- `6a17c001442da8169f2cbb405be062dac2ee0d43`, `97195885fae5e8a69182380e1a002692047348c6`, `659bb41abd609d6dd1727b965950f60571e8ceb3`, and `4bf3c73a150eecdb16d43cffeddd560b8b937f4b` rebuild the plugin system from foundations through Action API, hooks, skill resources, agent triggers, and MCP transport.
- `8ade96e671bca7d84fd2d764ffca305e2fdc358d` establishes Public API v1 and `a4e1ea7140c46b245ab0b384a6a565ab981cd513` exposes a globally versioned plugin API.
- `4f56785b02dd16312cc391729b07a1bb16b4c1c4` merges Reply and Ownership turn modes into one fact-driven daemon workflow. A long sequence of daemon prompt and brief refactors follows.
- `b47e835d7d6bdcc4f872eb1a0ab02908be7db7c1` reorganizes runtime management by machine.
- `41ddf6c0a1bc22ba256852442f43d5ff1990a073` removes dead frontend components and adds dependency-usage enforcement with Knip.
- Agent creation, runtime profiles, MCP configuration, workspace skills, plugins, task attribution, and source context all gained new contracts.

The operational branch predates these contracts. Its older mode-switch design cannot be transplanted by preferring the custom side of merge conflicts.

### Database migrations

The interval changes 606 migration files, including 596 additions. Current `fork/main` reaches migration `441_runtime_profile_api_provider`, added by the fork provider implementation.

The old operational baseline owns migration numbers 134 through 136:

```text
134_agent_mode
135_agent_action_log
136_agent_allowed_tools
```

Current upstream and fork reuse those historical numbers for different migrations:

```text
134_runtime_profile_add_qoder
135_comment_workspace_index
136_runtime_profile_add_traecli
137_search_index_pg_trgm_extension
```

This is a hard blocker to replaying the old SQL files. Operational schema work must be restated as new migrations after 441, with regenerated sqlc output. It must also follow the current rules: no foreign keys or cascades, every new index uses `CONCURRENTLY`, and each concurrent index is isolated in a single-statement migration file.

### Daemon and runtime changes

This is the highest-volume and highest-conflict category. The historical interval contains 348 commits that touch `server/internal/daemon`, `server/internal/handler/daemon.go`, or `server/pkg/agent`. It includes:

- New native runtimes and protocols, including QwenPaw, Reasonix, Oh-My-Pi, DeepSeek Harness, MiniMax Code ACP, Dim, ZeroClaw, and workspace MCP configuration.
- Runtime discovery, model catalogs, custom-provider routing, and per-agent MCP assignments.
- Session retirement and resume behavior, process-tree ownership on every backend, task-specific environment roots, garbage collection, runtime sweeper changes, and Windows process semantics.
- New inactivity and tool budgets culminating in `dcc2d582512ce1f2a1de9de9ddbdb10d1c838ff8` and `64ec7f54163d918d5d7fd4dcae857f241b7842d0`.
- Fact-driven daemon prompt and workflow changes that replace assumptions embedded in the early operational-mode branch.

The fork provider support changes 48 files with 4,850 insertions and 242 deletions. It already sits on the current upstream daemon and Session contract, which makes it the correct technical base.

### Frontend changes

The interval heavily changes agent creation and inspection, runtime setup and catalogs, chat streaming, issue surfaces, settings, desktop routing, mobile parity, shared API schemas, and all four locale sets. The highest-risk frontend files for operational work are:

- `packages/core/types/agent.ts`
- `packages/core/agents/mcp-support.ts`
- `packages/views/agents/components/create-agent-dialog.tsx`
- `packages/views/agents/components/agent-detail-inspector.tsx`
- `packages/views/agents/components/agent-overview-pane.tsx`
- `packages/views/runtimes/components/runtime-profiles-dialog.tsx`
- `packages/core/api/client.ts` and the current schema parsers
- `packages/views/locales/{en,ja,ko,zh-Hans}/agents.json`

Any new API field must use current schema parsing and fallback conventions. Operational controls should be added to shared views and core types, not app-specific copies.

### Tests and CI improvements

Representative changes include:

- `36533bbc2b6bb85eaba7d047e512efdf3a6516f8` prevents default tests from executing installed agent CLIs.
- `78591f60225d157cfbb6add03f53b9c31dd53e80` caps `pkg/agent` concurrency under the race detector.
- `877a71568f6d5d9375fdaa36414f4c9189144652` adds shared server test utilities and migrates the heaviest handler suites.
- `b946f3a729e5fb85e1f270cdc15cd72999368047` splits view tests onto a dedicated runner.
- Multiple deterministic race fixes cover daemon output, prompt writes, desktop preference loading, async media, and chat state reconciliation.
- CI adds path filters, Turborepo caching, generated artifact checks, migration safeguards, and image-size gates.

The custom provider live smoke test is correctly isolated behind the `agentintegration` build tag and an explicit environment opt-in. Future operational-mode tests should preserve the default-suite prohibition on ambient agent CLI and authenticated account use.

## Conflict and subsystem audit

Read-only `git merge-tree --write-tree --messages` simulations were used to measure textual conflicts. No branch or working tree was modified.

### Operational baseline against current fork

All 24 files changed by the six-commit operational baseline were also changed in the 1,186-commit upstream interval. A direct merge simulation reports 11 content conflicts:

```text
packages/core/types/agent.ts
packages/views/agents/components/agent-detail-inspector.tsx
packages/views/agents/components/create-agent-dialog.tsx
server/internal/daemon/daemon.go
server/internal/daemon/execenv/runtime_config.go
server/internal/daemon/execenv/runtime_config_sections.go
server/internal/handler/agent.go
server/internal/handler/daemon.go
server/pkg/db/generated/agent.sql.go
server/pkg/db/generated/models.go
server/pkg/db/queries/agent.sql
```

The migration files do not appear as textual conflicts because upstream deleted the custom files and reused their numbers. Semantically, that is a more serious collision than an ordinary conflict.

### Phase 3 operational controls branch against current fork

`feat/phase3-operational-controls` changes 67 files. Forty-five overlap the historical upstream delta, and a direct merge simulation reports 26 conflicts. Beyond the baseline list, the major conflict surfaces include:

- MCP support and tests
- `packages/core/types/index.ts`
- agent overview and locale files
- server router and daemon configuration/tests
- `server/pkg/agent/agent.go` and supported-runtime tests
- Claude and CodeBuddy backends/tests

This branch also contains the old OmniRoute implementation and an approval-queue design document. It should not be merged as one unit.

### Replacement provider branch against current fork

`feat/replacement-runtime-providers-main` is based on the refreshed upstream tip and contains five custom commits. It overlaps the current fork provider implementation in five paths. A direct merge simulation reports four add/add conflicts:

```text
server/pkg/agent/openai_compatible.go
server/pkg/agent/openai_compatible_test.go
server/pkg/agent/provider_catalog.go
server/pkg/agent/provider_catalog_test.go
```

`server/pkg/agent/agent.go` auto-merges in the simulation, but still requires semantic review. The replacement branch is therefore a competing provider implementation, not an upstream delta. Its credential-redaction cases and capability-gate tests are candidates for manual porting only after comparing them with the fork implementation.

## Highest-risk areas

1. **Migration ledger and sqlc generation:** old migration numbers collide, generated files changed extensively, and runtime profile persistence now ends at migration 441.
2. **Daemon task lifecycle:** current code includes new timeout, session, process ownership, workspace context, GC, and queue semantics. Operational-mode branching inside this lifecycle can bypass current safeguards.
3. **Agent backend interfaces:** `server/pkg/agent/agent.go`, the OpenAI-compatible adapter, provider catalog, built-in runtimes, model discovery, MCP, and stream terminal events form one contract.
4. **Runtime profile API and credential custody:** migration 441, runtime-profile handlers, daemon probes, and provider discovery must agree. Provider keys must remain daemon-owned and redacted.
5. **Agent API and shared frontend types:** mode, allowlist, service-tier, thinking-level, skills, visibility, and runtime fields now coexist in the same request and response surfaces.
6. **Prompt and policy composition:** daemon prompt code and the built-in operational workflow skill changed repeatedly. A separate legacy prompt path can drift from current authorization and context rules.
7. **Tests with real providers:** authenticated smoke tests must stay opt-in and tagged. Default tests must use fixtures and cannot resolve ambient agent executables.

## Prioritized integration recommendation

### Priority 0: Do not integrate upstream

There is nothing to merge from `origin/main` into `fork/main` at the audited refs. Record the hashes above as the reconciliation checkpoint and continue from `fork/main`.

### Priority 1: Keep the fork provider implementation as the base

The two fork commits are already built on the exact upstream tip and include daemon registration, migration 441, frontend setup, model discovery, MCP behavior, unit coverage, and opt-in live smoke coverage. Do not merge the competing replacement-provider branch. Review its five commits as a semantic checklist, especially redirect refusal, bounded SSE/error handling, credential redaction, and capability gates, then port only any missing tests or defenses.

### Priority 2: Rebuild operational controls as current-main slices

Use the old baseline as requirements evidence. Reimplement in small, reviewable slices on top of `fork/main`:

1. Define current data semantics for mode, allowed tools, approval state, and action audit.
2. Add new migrations after 441 and regenerate sqlc output.
3. Add API schemas and handler behavior using current workspace, permission, and compatibility patterns.
4. Add daemon enforcement at current lifecycle boundaries, preserving timeouts, process ownership, session retirement, MCP allowlists, and redaction.
5. Add shared frontend controls and locale keys after the server contract stabilizes.
6. Add bounded action-log queries, focused unit tests, and explicit negative authorization cases.

### Priority 3: Separate approval queue and dashboard work

Treat approval queue persistence, operational-mode enforcement, and the operations dashboard as separate changes. This limits the blast radius across database, daemon, API, and UI boundaries and avoids recreating the 26-conflict Phase 3 branch.

### Priority 4: Gate completion with current repository checks

For each slice, run focused Go and TypeScript tests while iterating, then the broader checks justified by the changed surface. Include migration verification, sqlc regeneration checks, default-test ambient-CLI protection, provider fixture tests, and the explicitly authorized live smoke only when required.

## Audit commands

The following commands provide the core reproducible evidence:

```bash
git fetch --prune origin main
git fetch --prune fork main
git rev-parse origin/main fork/main
git merge-base origin/main fork/main
git rev-list --left-right --count origin/main...fork/main
git merge-base --is-ancestor origin/main fork/main
git merge-base 7c93031e63d2fa230344751eff7335e86c42cd85 origin/main
git rev-list --left-right --count 7c93031e63d2fa230344751eff7335e86c42cd85...origin/main
git rev-list --count 7c93031e63d2fa230344751eff7335e86c42cd85..origin/main
git diff --shortstat 7c93031e63d2fa230344751eff7335e86c42cd85..origin/main
git merge-tree --write-tree --messages fork/main feat/operational-agent-mode
git merge-tree --write-tree --messages fork/main feat/phase3-operational-controls
git merge-tree --write-tree --messages fork/main feat/replacement-runtime-providers-main
```

## Blockers

No blocker prevents this audit from completing. The only integration blockers discovered are design and history constraints for later implementation: the old operational migration numbers collide with upstream, the operational branches have broad textual conflicts, and the replacement-provider branch competes with the provider implementation already on `fork/main`.
