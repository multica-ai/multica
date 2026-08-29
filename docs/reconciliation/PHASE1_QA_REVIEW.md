# Phase 1 Independent QA Review

Date: 2026-08-29

Role: Independent QA Lead

Audited report integration tip: `db4fdb69b4fe314d85c8e56159094426086bb0e7`

Scope: independent verification of the five Phase 1 reports against live remote heads, local Git objects, registered worktrees, preserved side refs, and the current codebase. No application code, branch, ref, stash, worktree, credential value, or external provider was changed or invoked.

## Executive conclusion

Phase 1 is sufficient to start two controlled Phase 2 implementation tracks, but it does not authorize an upstream merge, a live-provider claim, or branch cleanup.

- `fork/main` is exactly 2 commits ahead and 0 behind `origin/main` at the verified remote heads. There is no upstream change to integrate at this checkpoint.
- The 48-path provider runtime delta on `fork/main` is the protected implementation base.
- Provider validation hardening may proceed, but every provider remains unverified live until provider-specific, explicitly authorized evidence is recorded.
- Provider-neutral policy, approval, and metadata-only audit controls may be rebuilt on current seams. Legacy operational modes are not a security boundary and are excluded from that security implementation track.
- Branch cleanup is no-go. The retirement snapshot is already stale, ten worktrees remain registered, one legacy worktree is dirty, `refs/stash` is uniquely reachable, and no archival tag exists for the protected tips.

## 1. Confirmed Git evidence

### 1.1 Five-report completion gate

All five reports are committed in a linear documentation-only chain above `fork/main`:

| Report | Integrated commit |
| --- | --- |
| `TEAM_ALPHA_UPSTREAM_AUDIT.md` | `db45728c143f6f2e9cc0beb0aa1c354401cb0e04` |
| `TEAM_GAMMA_PROVIDER_VALIDATION.md` | `956be4e2ff04f692dfe656245ee1db39e06e55bf` |
| `DO_NOT_OVERWRITE.md` | `9d9716af3a2d3522c31cb2585d0e4231e5d1480d` |
| `TEAM_DELTA_OPERATIONAL_CONTROLS.md` | `db3dcd14dbe564f3a810c3e234465c60397bcedb` |
| `TEAM_EPSILON_BRANCH_RETIREMENT.md` | `db4fdb69b4fe314d85c8e56159094426086bb0e7` |

Before this QA report, `git diff --name-status fork/main..HEAD` contained exactly those five added Markdown files and no application changes.

### 1.2 Current upstream relationship

Live `git ls-remote` results matched the local remote-tracking refs:

| Evidence | Verified value |
| --- | --- |
| Live and local `origin/main` | `64ec7f54163d918d5d7fd4dcae857f241b7842d0` |
| Live and local `fork/main` | `240d4b9bb69df1d2fb1bf179668216b7c68d48c1` |
| `git merge-base origin/main fork/main` | `64ec7f54163d918d5d7fd4dcae857f241b7842d0` |
| `git rev-list --left-right --count origin/main...fork/main` | `0 2` |
| `origin/main` ancestor of `fork/main` | yes |
| `fork/main` ancestor of `origin/main` | no |

The two fork-only commits are:

1. `cdba5480000c1a49ceb1a03c3fc52befdcbbaaab`, provider runtime support.
2. `240d4b9bb69df1d2fb1bf179668216b7c68d48c1`, opt-in provider smoke coverage.

Decision consequence: an upstream merge or rebase has no content to add. Phase 2 must branch from the exact `fork/main` tip above after one final live ref check.

### 1.3 Historical divergence and corrected range semantics

The operational baseline `7c93031e63d2fa230344751eff7335e86c42cd85` and current upstream diverge at:

```text
a06fc273406d75ff67b07a052333c99870be2d39
```

Verified symmetric counts are:

| Comparison | Left only | Right only |
| --- | ---: | ---: |
| `7c93031e...origin/main` | 6 | 1,186 |
| `7c93031e...fork/main` | 6 | 1,188 |
| `da4bd0380...fork/main` | 7 | 1,188 |
| `284facd68...fork/main` | 18 | 1,188 |
| `d60346841...fork/main` | 5 | 2 |

Team Alpha's endpoint comparison is reproducible, but its label is too broad. `git diff 7c93031e..origin/main` is not a pure upstream-only delta because it also reverses the six custom baseline commits. The exact distinction is:

| Range | Files | Insertions | Deletions | Migration paths |
| --- | ---: | ---: | ---: | --- |
| Endpoint comparison `7c93031e..origin/main` | 4,302 | 680,306 | 82,500 | 606: 596 added, 4 modified, 6 deleted |
| True upstream side `a06fc273..origin/main` | 4,292 | 680,305 | 81,959 | 600: 596 added, 4 modified |

The six migration deletions in the endpoint comparison are custom-only files, not upstream deletions. Migration numbers 134 through 138 still collide semantically because upstream reused those numeric prefixes under different filenames.

### 1.4 Conflict evidence

Fresh set comparisons and read-only `git merge-tree --write-tree --messages` simulations confirmed:

| Side line merged into `fork/main` | Side paths | Exact path overlap with upstream-touched paths | Direct textual conflicts |
| --- | ---: | ---: | ---: |
| Baseline `7c93031e` | 24 | 14 | 11 content conflicts |
| Phase 3 tip `284facd68` | 67 | 35 | 26 total: 25 content, 1 add/add |
| Replacement tip `d60346841` | 8 | 5 with the fork provider delta | 4 add/add |

The 10 paths changed by both the Phase 3 line and the fork-only provider commits are already included within the 35 upstream-touched overlaps. Team Alpha's 24-of-24 and 45-of-67 overlap statements are overcounts. Its conflict counts and its conclusion against whole-branch merges remain correct.

### 1.5 Preservation and retirement evidence

- The provider delta from `origin/main` to `fork/main` is exactly 48 paths, 4,850 insertions, and 242 deletions. The inventory in `DO_NOT_OVERWRITE.md` matches all 48 paths with no additions or omissions.
- The operational delta through `da4bd0380` is exactly 48 paths, 1,472 insertions, and 39 deletions. Its preservation inventory also matches exactly.
- All seven operational commits and all five replacement-runtime commits remain patch-inequivalent to `fork/main`.
- `refs/stash` remains the only ref containing `0f5927be70ff1ad3c639c7efa6a81517436a15b4`. It contains 20 paths, including its third-parent untracked tree.
- The replacement-runtime worktree still has one unstaged file with 58 insertions. Its working blob remains `5a502749d58fc19af1eff0a0078d440e710db778`; its HEAD blob remains `66c7982dc23f9e18880a7cb3abd7effe04019b35`.
- None of the seven protected tips, including the stash tip, has an archival tag.
- The ignored Phase 3 `.env.worktree` exists and was not inspected. Its current filesystem mode is `0644`; any authorized private archive copy must be permission-restricted and must never enter Git.

## 2. Cross-report conflicts and omissions

### QA-01: Upstream delta arithmetic is partly mislabeled

Risk: medium.

Team Alpha correctly proves the ancestry and commit counts, but it treats an endpoint tree comparison as if every change came from upstream. The corrected true-upstream statistics and overlap counts are in section 1. This does not change the integration decision, but Phase 2 must use the merge base for historical-side accounting.

### QA-02: Operational mode and security controls have conflicting dispositions

Risk: high.

`DO_NOT_OVERWRITE.md` treats `coding`, `operational`, and `hybrid` modes plus the operational workflow skill as concepts to port. Team Delta explicitly discards both from the control-plane implementation.

QA resolution:

1. Preserve the mode commits, UI semantics, prompt behavior, and workflow skill as historical product evidence.
2. Do not port them as part of Phase 2 security controls.
3. Treat mode as user-facing workflow intent only. It must never grant, deny, approve, isolate, or audit a tool call.
4. Enforce security through provider-neutral policy, exact schema identity, human-only decisions, and a managed pre-transport gate regardless of mode.
5. If operational and hybrid modes remain a product requirement, open a separate product slice after an explicit scope decision. That slice may select a default policy template, but it cannot weaken or substitute for the policy boundary.

This resolves the reports without deleting the historical work or allowing prompt wording to masquerade as authorization.

### QA-03: The provider preservation checklist is necessary but not sufficient

Risk: critical.

Team Gamma identified release blockers that the preservation manifest does not make explicit enough:

- Hosted credential-bearing base URL overrides accept any HTTPS host rather than a provider-bound host or trusted operator override.
- API execution accepts the selected model without rechecking the discovered catalog.
- OpenCode Zen and Go route unknown model families to Chat Completions instead of failing closed.
- Every API descriptor advertises `usage`, `tools`, and `mcp` through one shared capability list without provider and model-specific proof.
- The generic live harness copies the whole process environment and silently defaults both provider and model.
- Native OpenCode exists in the backend catalog but is absent from the frontend catalog.
- Claude, Antigravity, Cursor, and Grok can send unsanitized provider diagnostics to logs. Some result paths are sanitized, but the logging path remains unsafe.
- Failed optional provider probes are omitted without a sanitized offline reason.

These are Phase 2 hardening requirements. Preserving current provider behavior does not mean preserving these gaps.

### QA-04: Operational controls are design-ready, not implementation-complete

Risk: critical.

`git grep` at `fork/main` found none of the proposed general policy, approval request, action event, or `operational_controls_v1` symbols. Current task-message persistence, plugin MCP schema approvals, `RequireHumanActor`, dashboard patterns, and the managed remote MCP broker are foundations only.

The legacy implementation is correctly rejected as source code. Independent inspection confirmed forbidden foreign keys and cascade actions, nonconcurrent indexes, raw argument summaries, a missing direct workspace key, and `ON CONFLICT DO NOTHING RETURNING *`, which cannot return the existing approval row.

Team Delta's plan still needs implementation-time decisions for the final migration block, retention period, exact heartbeat capability names, approval wait transport, and sanitized malformed-response logging. Those must be frozen in the first Phase 2 contract slice before parallel implementation.

### QA-05: The retirement report is a snapshot, not an executable current plan

Risk: critical.

Team Epsilon correctly requires a fresh quiesced preflight. That preflight is now mandatory because current state has already moved:

- Ten worktrees are registered, not the eight or nine observed during its snapshots.
- The report branches have advanced to their committed report tips.
- `fork/CCRBrad/multica-upstream-reconciliation` now points to `db4fdb69b4fe314d85c8e56159094426086bb0e7`, not the expected `db45728c...` in the sample deletion command.
- The QA worktree is now another active preservation dependency.
- The dirty W2 file, unique stash ref, ignored environment file, and absence of archival tags remain unchanged.

No command containing the old expected remote tip is authorized. Cleanup remains blocked until workers are quiesced, a new inventory is signed, archives are verified, and accepted or rejected semantics are recorded.

## 3. Signed-off Phase 2 preservation boundary

### 3.1 Must survive intact or by proven equivalent behavior

The protected base is `fork/main` at `240d4b9bb69df1d2fb1bf179668216b7c68d48c1` and its exact 48-path provider delta.

Phase 2 must preserve:

- Backend provider identity and fail-closed catalog authority.
- Daemon-only credential value custody and secret-free profile, API, log, and UI contracts.
- Provider-specific credential variable allowlists.
- Current URL shape and loopback controls, redirect refusal, bounded probes and responses, optional-provider isolation, and custom environment blocklists.
- Output redaction, explicit protocol routing, Gemini rejection, terminal stream checks, bounded tool turns, cancellation, and stateless API resume behavior.
- Migration 441 schema meaning, current workspace and role authorization, sqlc regeneration discipline, and all four locale surfaces.
- Opt-in `agentintegration` gates. Real providers must never run in default tests.

Equivalence requires focused tests on the reconciled tree. Whole-file conflict selection is not evidence of equivalence.

### 3.2 May be reimplemented from requirements only

- Exact provider-neutral tool policy with absence preserving compatibility and an active policy defaulting to deny.
- Schema-digest pinning and drift denial.
- Metadata-only, workspace-scoped action events.
- Human-only approval, denial, expiry, cancellation, and exactly-once consumption.
- Owner/admin approval queue and operations dashboard.
- Mode UI and workflow semantics only in a separately approved product slice, never as enforcement.

### 3.3 Excluded from Phase 2 source integration

- Every OmniRoute-specific commit, file, provider ID, migration, environment contract, session path, and MCP path.
- Legacy migrations 134 through 138 and every generated file derived from them.
- The archived approval schema and queries as implementation source.
- The replacement-runtime branch as a merge or cherry-pick source. Review only its missing defense cases and port a case only when the fork lacks equivalent coverage.
- Legacy operational UI component patches, broad Agent API policy fields, raw summaries, wildcard policies, best-effort security audit writes, and prompt-only safety claims.
- Any whole branch, whole generated file, or whole conflict side selected for convenience.

### 3.4 Refs and artifacts that cannot be retired yet

Preserve `fork/main`, every historical operational and replacement tip, `refs/stash` and all parents, W1, W2 and its dirty patch, the ignored W1 environment file through a private permission-restricted process, every active Orca report branch and worktree, and the current remote report branch. No deletion or force operation is part of Phase 2 implementation.

### QA sign-off

Signed on 2026-08-29 by the Independent QA Lead for Phase 1. This sign-off authorizes only the scoped Phase 2 implementation paths above, anchored to the exact origin, fork, and report hashes in this review. It does not authorize live provider calls, production rollout, archive publication, branch deletion, worktree removal, stash mutation, or credential inspection.

## 4. Risk-ranked test plan for the reconciled branch

### P0: Merge and security gates, required before review approval

1. Recheck live `origin/main` and `fork/main`, merge base, and ahead/behind counts. Stop if either remote tip differs from this review.
2. Compare the reconciled provider surface against the 48-path manifest at symbol and invariant level. Assert no OmniRoute identifier and no legacy migration 134 through 138 file is introduced.
3. Add deterministic hosted-host binding tests, trusted-override tests, daemon credential precedence tests, model-catalog revalidation tests, and fail-closed Zen/Go unknown-family tests.
4. Split baseline API capabilities from provider and model-specific `usage`, `tools`, and `mcp` evidence. Test every negative capability path.
5. Add sanitizer canaries across native stderr, provider errors, API results, daemon logs, persisted rows, WebSocket payloads, and validation logs.
6. Run fresh-install, upgrade, down/up, duplicate-prefix, no-foreign-key, no-cascade, and one-concurrent-index-per-file migration tests before accepting generated sqlc output.
7. Test the full authorization matrix, including cross-workspace IDs and every human, agent, task, daemon, and unknown actor source.
8. Race approval, denial, expiry, cancellation, policy replacement, and consumption. Require exactly one winning transition and atomic paired audit records.
9. At the managed transport boundary, require zero upstream calls for deny, pending, expired, cancelled, schema drift, audit failure, and duplicate consume. Require exactly one upstream call after one valid consumption.

### P1: Integration, compatibility, and user-surface gates

1. Verify new and old server, daemon, and client combinations fail closed exactly as designed.
2. Verify tool invocation IDs correlate policy decisions, action events, and task transcripts without storing argument or result values.
3. Verify React Query owns all server state, workspace IDs are present in query keys, realtime events only invalidate authorized caches, reconnect invalidates the same keys, and role downgrade removes protected cache data.
4. Verify web and desktop routes, owner/admin gating, non-optimistic decisions, stale `409` handling, focus restoration, keyboard operation, accessible names, status announcements, localization, and malformed-response states.
5. Verify bounded pagination, query plans, retention cleanup, expiry sweeps, and dashboard coverage labels under realistic volume.

### P2: Explicitly authorized provider evidence and broad regression

1. Harden every live fixture so provider and model are explicit, the environment is allowlisted, processes and transports are bounded and cleaned up, and logs contain sanitized metadata only.
2. Run one provider and one protocol route at a time only after explicit authorization. Record exact commit, provider ID, model ID, marker result, duration, terminal status, and independently proven capability booleans.
3. Do not promote `usage`, `tools`, or `mcp` from a baseline completion. Each capability needs its own provider and model-specific green case.
4. Run the full Go, TypeScript, lint, typecheck, build, and relevant Playwright suites after focused tests pass. Run targeted Go race tests for the approval and daemon packages.

Suggested final command gate, adjusted to the files actually changed:

```bash
make test
pnpm typecheck
pnpm test
pnpm lint
make check
pnpm exec playwright test
```

## 5. Go or no-go decisions

| Track | Decision | Scope and gate |
| --- | --- | --- |
| Upstream integration | **NO-GO** | There are zero upstream-only commits at the verified refs. Do not merge or rebase. **GO** only for using the exact current `fork/main` tip as the Phase 2 base after a final live ref check. |
| Provider validation implementation | **GO WITH CONDITIONS** | Implement deterministic trust, capability, model, redaction, observability, and harness hardening. Live calls and any `verified` label remain **NO-GO** until separately authorized evidence passes. |
| Operational controls implementation | **GO WITH CONDITIONS** | Rebuild provider-neutral policy, approval, and metadata-only audit controls on current seams, beginning with a frozen contract slice. Legacy cherry-picks, mode-based security, native-transport overclaims, and production enablement remain **NO-GO**. |
| Branch and worktree cleanup | **NO-GO** | Current inventory is active and stale relative to Team Epsilon's command examples. Require quiescence, a fresh signed snapshot, verified private archive and secret scan, semantic acceptance or rejection records, clean worktrees, archival refs, and exact-tip guards first. |

## 6. Verification executed for this review

Read-only verification included live remote head queries, merge-base and symmetric-count checks, set-based path overlap checks, merge-tree conflict simulations, manifest-to-diff comparisons, ref reachability, worktree status, blob identity, migration inspection, and current-versus-legacy symbol searches.

Fresh deterministic tests passed:

```text
GOPROXY=off go test ./pkg/agent -run 'Test(Provider|APIProvider|OpenAICompatible|OpenCode|ListAPIModels|Opencode)' -count=1
ok github.com/multica-ai/multica/server/pkg/agent 3.047s

GOPROXY=off go test ./internal/daemon -run 'Test(ProbeAPI|APIProvider|RuntimeProfilesRegisterAPI|DetectBuiltinRuntimesRegistersAPI|IsBlockedEnvKey)' -count=1
ok github.com/multica-ai/multica/server/internal/daemon 0.313s

GOPROXY=off go test ./internal/handler -run 'Test.*RuntimeProfile' -count=1
ok github.com/multica-ai/multica/server/internal/handler 0.548s
```

The tagged Cursor, Grok, and generic API smoke tests compiled and skipped at their absent opt-in gates. No provider or local-model request was made.

The pre-commit release check passed with exactly one staged path, clean `git diff --cached --check` output, and zero em dash characters.
