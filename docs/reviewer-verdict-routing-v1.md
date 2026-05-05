# Reviewer Verdict Routing — Driver-Plane State Machine v1

Related: [DRV-78](mention://issue/68455f33-73d7-4a56-aad5-fb2eccfd0734) · [DRV-79](mention://issue/3ea0dab1-52ff-4b1a-942c-64eeeb38c0bd) · [DRV-80](mention://issue/0a2dcef5-5ed1-4965-b876-b4b039fc66d7)

## Problem

Review verdicts (REQUEST_CHANGES / BLOCKER) currently require a human `@mention` to wake the implementing agent. This doc specifies the driver-plane state machine that routes verdicts to fix tasks automatically — no mention-pong, no polling.

---

## 1. States

| State | Issue status | Meaning |
|---|---|---|
| `IMPLEMENTING` | `in_progress` | Implementer writing/fixing code |
| `AWAITING_REVIEW` | `in_review` | PR open, reviewer agent running or queued |
| `FIX_IN_PROGRESS` | `in_review` | Fix task created; implementer working on changes |
| `ESCALATED` | `blocked` | Exceeded max fix cycles, or BLOCKER on scope/auth/human-decision |
| `MERGE_READY` | `in_review` | APPROVED; waiting for merge gate |
| `DONE` | `done` | Merged and closed |

`in_review` covers both AWAITING_REVIEW and FIX_IN_PROGRESS because the parent issue is still under review in both cases. `blocked` is reserved for cases where a human must unblock.

---

## 2. Transitions

```
IMPLEMENTING ──(push PR + request review)──▶ AWAITING_REVIEW

AWAITING_REVIEW ──(APPROVED)──────────────▶ MERGE_READY
AWAITING_REVIEW ──(WARNING)───────────────▶ MERGE_READY        (non-blocking; audit trail only)
AWAITING_REVIEW ──(REQUEST_CHANGES)───────▶ FIX_IN_PROGRESS    (create/reuse fix task)
AWAITING_REVIEW ──(BLOCKER)───────────────▶ ESCALATED          (if scope/auth/human-decision)
AWAITING_REVIEW ──(BLOCKER, code fix ok)──▶ FIX_IN_PROGRESS    (create/reuse fix task)

FIX_IN_PROGRESS ──(implementer pushes)────▶ AWAITING_REVIEW    (re-request review)
FIX_IN_PROGRESS ──(N ≥ 3 failed cycles)──▶ ESCALATED

MERGE_READY ──(merge succeeds)────────────▶ DONE
MERGE_READY ──(merge fails / CI red)──────▶ FIX_IN_PROGRESS

ESCALATED ──(human resolves + reassigns)──▶ IMPLEMENTING
```

---

## 3. Idempotency Keys

Fix tasks are keyed by `{head_sha}:{verdict_id}` to prevent duplicates.

| Scenario | Driver behavior |
|---|---|
| Same verdict re-posted (same `verdict_id`) | Look up task by idempotency key → reuse existing; do NOT create second task |
| Same HEAD SHA, new verdict ID | New idempotency key → create new fix task (reviewer ran again on same commit) |
| New HEAD SHA, new verdict ID | New idempotency key → new fix task (implementer pushed; new review cycle) |
| New HEAD SHA, stale verdict ID re-used | Should not happen; `verdict_id` is UUID per emission — treat as duplicate of old verdict |

**Storage:** idempotency key is written to the fix task description as a machine-readable tag:

```
<!-- multica:fix-task-key {head_sha}:{verdict_id} -->
```

Driver scans existing child issues for this tag before creating a new task.

---

## 4. BLOCKER Routing Decision Tree

Not all BLOCKERs warrant a fix task. Driver must classify before routing:

```
BLOCKER finding
├── check = "scope-change" | "product-ambiguity"  → ESCALATED (no fix task)
├── check = "destructive-operation"                → ESCALATED (no fix task)
├── check = "auth-failure" | "permission-denied"  → ESCALATED (no fix task)
├── finding.required_action contains "human"      → ESCALATED (no fix task)
└── otherwise                                      → FIX_IN_PROGRESS (fix task)
```

Escalated BLOCKERs set issue to `blocked` and post an audit comment with the human escalation reason. No agent mention link — human discovers it via issue status change.

---

## 5. Fix Task Shape

When the driver creates a fix task:

```
Title:       fix(scope): address {VERDICT} from review {verdict_id[:8]}
Parent:      parent issue ID (same as the issue under review)
Status:      todo
Priority:    inherit from parent
Assigned to: implementer agent ID (same agent that opened the PR)
Project:     same project as parent
Description:
  Parent review: {parent_issue_identifier} PR: {pr_url} @ {head_sha[:7]}

  <!-- multica:fix-task-key {head_sha}:{verdict_id} -->

  ## Findings to address

  {for each finding with severity >= high:}
  - [{severity}] `{check}` — {rationale}
    File: {location.file}:{location.line}
    Fix: {required_action}

  {if truncated:}
  … plus {N} lower-severity findings. See verdict comment for full list.

  ## Verifications that must pass before re-review

  {for each verification that failed:}
  - [ ] {verification.name}

  Re-push when fixed. No @mention needed — assignment wakes this task automatically.
```

Assignment (not mention) wakes the implementer. The issue `Wakeup` mechanism already fires a task on assignment to an agent.

---

## 6. Audit Comment Format

Driver posts exactly ONE comment per review iteration. Contents:

```
**Review iteration {N}** · verdict `{VERDICT}` · {verdict_id[:8]}

{if APPROVED:}
All checks passed. Routing to merge gate.

{if WARNING:}
{M} low/medium finding(s). Non-blocking — proceeding to merge gate.
{finding list: "- [{severity}] {check}: {rationale[:120]}"}

{if REQUEST_CHANGES:}
{M} high finding(s) require fixes. Fix task: [{fix_task_identifier}](mention://issue/{fix_task_id}).
Implementer will push when done; reviewer will be re-requested automatically.

{if BLOCKER (fix task created):}
{M} critical finding(s) block merge. Fix task: [{fix_task_identifier}](mention://issue/{fix_task_id}).

{if BLOCKER (escalated):}
Escalated: {escalation_reason}. Human decision required before this can proceed.

{if cycle >= 3:}
⚠ {N} fix cycles completed without resolution. Escalating for human review.
```

**No `mention://agent` link in this comment.** Issue-mention links (`mention://issue/...`) are safe (no side effect).

---

## 7. Cycle Counter and Escalation

The driver tracks `review_cycle` as an integer in the issue description or a label:

```
<!-- multica:review-cycle {N} -->
```

Increment on each AWAITING_REVIEW → FIX_IN_PROGRESS transition. On `N ≥ 3` REQUEST_CHANGES/BLOCKER: skip fix task, post escalation comment, set issue to `blocked`.

Label `review-escalated` is attached when escalated so humans can filter.

---

## 8. Malformed Verdict Handling

Per DRV-79 §6 — driver behavior when envelope parse fails:

| Failure | Derived verdict | Driver action |
|---|---|---|
| JSON parse fails | WARNING | Log `malformed-envelope` finding; do NOT create fix task; post audit comment |
| Unknown `schema_version` | WARNING | Log `unsupported-schema-version`; treat as prose fallback |
| `verdict` inconsistent with `max(severity)` | Derived from findings | Override; log `verdict-severity-mismatch` |
| Missing required fields | BLOCKER | Escalate; log `incomplete-envelope` |
| Findings exceed cap | — | Trim to cap; add `cap-exceeded` info finding |

Driver never auto-retriggers reviewer on malformed verdict (loop risk). Post one audit comment; stop.

---

## 9. No-Mention Wake Protocol

The platform wakes an agent when a task is assigned to it (see `EnqueueTaskForIssue` / `ClaimTask`). Driver exploits this:

1. Create fix task assigned to implementer agent → platform enqueues a run.
2. Implementer runs, fixes, pushes, then updates fix task status to `done`.
3. Implementer also transitions parent issue back to `in_review`.
4. Platform wake on parent issue `in_review` + reviewer autopilot → reviewer runs.
5. Reviewer posts new verdict comment → driver processes it (driver is the assignee of the parent issue → new comment triggers new run).

No `mention://agent` at any step. Assignment + status change + comment on assigned issue are sufficient triggers.

---

## 10. Test Plan

### 10.1 Unit tests (`packages/core/verdict-router.test.ts`)

| Test | Input | Expected |
|---|---|---|
| Parse valid APPROVED envelope | Comment with fenced `multica:reviewer-verdict v1` block | Returns parsed `VerdictEnvelope` with verdict=APPROVED |
| Parse valid REQUEST_CHANGES envelope | Same | verdict=REQUEST_CHANGES, findings non-empty |
| Parse valid BLOCKER envelope | Same | verdict=BLOCKER, findings with critical severity |
| Parse valid WARNING envelope | Same | verdict=WARNING |
| Prose-only comment (no envelope) | Plain text "LGTM" | Returns `null` envelope; fallback WARNING decision |
| Malformed JSON in envelope | Garbled JSON | `ParseResult.error = 'malformed-envelope'` |
| Unknown schema version | `schema_version: "99"` | `ParseResult.error = 'unsupported-schema-version'` |
| Verdict/severity mismatch | verdict=APPROVED but finding.severity=critical | Derived verdict=BLOCKER; `mismatch` flag set |
| Missing required fields | Envelope without `verdict_id` | `ParseResult.error = 'incomplete-envelope'`; treated as BLOCKER |
| Cap exceeded | 25 findings | Trimmed to 20; `cap-exceeded` info finding appended |
| Derive idempotency key | head_sha="abc123", verdict_id="uuid-1" | `abc123:uuid-1` |

### 10.2 Routing tests (`packages/core/verdict-router.test.ts`)

| Test | Scenario | Expected |
|---|---|---|
| Duplicate verdict — same idempotency key | `routeVerdict` called twice with same head_sha+verdict_id, existing fix task present | Returns `action=REUSE_TASK`, same task ID |
| Moved head SHA — new commit, new verdict | New head_sha, new verdict_id; no existing task with key | Returns `action=CREATE_TASK` |
| New verdict on same SHA | Same head_sha, new verdict_id (reviewer ran again) | Returns `action=CREATE_TASK` (new verdict cycle) |
| APPROVED routing | verdict=APPROVED | Returns `action=MERGE_GATE` |
| WARNING routing | verdict=WARNING | Returns `action=MERGE_GATE` + findings surfaced |
| BLOCKER scope-change check | BLOCKER with `check=scope-change` | Returns `action=ESCALATE` |
| BLOCKER code fix | BLOCKER with `check=p0-cancellation-rethrow` | Returns `action=CREATE_TASK` |
| Cycle limit reached | `reviewCycle=3`, verdict=REQUEST_CHANGES | Returns `action=ESCALATE` |
| Malformed verdict | Parse error | Returns `action=AUDIT_ONLY` (no task, no escalation) |

### 10.3 Integration / manual test plan

1. **Happy path APPROVED**: open PR → reviewer posts APPROVED envelope → driver creates no fix task, transitions issue to merge-ready, posts audit comment with iteration + verdict_id.
2. **REQUEST_CHANGES cycle**: reviewer posts REQUEST_CHANGES → fix task created, assignee = implementer → implementer pushes → fix task marked done → reviewer re-runs → APPROVED.
3. **Duplicate verdict idempotency**: same verdict comment posted twice (e.g., accidental re-trigger) → second run finds existing fix task by key → no duplicate task created.
4. **Moved head SHA**: implementer pushes new commit → reviewer posts new verdict for new SHA → new fix task with new key → old fix task not touched.
5. **Escalation after 3 cycles**: 3 × REQUEST_CHANGES with no resolution → issue set to `blocked`, `review-escalated` label, no new fix task, audit comment says "escalating".
6. **BLOCKER scope-change**: reviewer posts BLOCKER with check=scope-change → no fix task, issue blocked, human escalation comment.
7. **Malformed envelope**: reviewer posts garbled JSON in fenced block → audit comment only, no fix task, no loop.
