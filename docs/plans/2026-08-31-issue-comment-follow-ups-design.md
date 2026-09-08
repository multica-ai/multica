# Issue Comment Suggested Follow-ups

## Problem

Direct Chat already gives a user three contextual follow-up suggestions after an
assistant reply. Issue workflows have the same conversational shape, but an
agent-authored task comment currently leaves the user to infer the next step and
compose another instruction manually. This is particularly confusing after a
long result or in a multi-stage review workflow.

The feature should close that parity gap without introducing a second dispatch
engine, executable HTML in comments, or issue mutations hidden behind a button.

## V1 Scope

- Generate up to three contextual follow-up suggestions after an eligible
  agent-authored issue comment.
- Mark exactly one suggestion as primary when at least one suggestion exists.
- Render suggestions under the latest actionable agent comment in a thread.
- Show the complete prompt before activation through a tooltip.
- On activation, post an ordinary reply that targets the agent responsible for
  the source task.
- Reuse the existing comment-to-mention-to-task path for permission checks,
  attribution, pending-task coalescing, runtime deferral, execution logs, and
  failure reporting.
- Generate suggestions asynchronously so task completion is not delayed.

V1 does not directly change issue state, delete data, execute arbitrary client
HTML, choose an arbitrary target, or complete a multi-step workflow. A handoff
suggestion remains a natural-language instruction sent to the same responsible
agent, which may coordinate the next role through existing mechanisms.

## Alternatives Considered

### Structured server-owned suggestions (selected)

Store validated suggestions as issue-comment data. Clients render trusted UI,
and activation sends only an opaque suggestion id. This provides durable,
auditable behavior and keeps authority on the server.

### Agent-authored Markdown or HTML controls

This reduces the initial backend surface, but comment sanitization intentionally
removes executable behavior. Allowing active HTML would create XSS, forged
target, permission, and cross-client consistency problems. A custom Markdown
protocol would still require product code and would make an untrusted comment
body the authority for an action.

### A separate workflow action engine

This could support arbitrary transitions, but would duplicate dispatch,
permission, coalescing, and failure semantics that already exist. It conflicts
with the architecture established by Issue Quick Actions.

## Data Model

Add `suggested_follow_ups JSONB NOT NULL DEFAULT '[]'::jsonb` to `comment`.
Each stored entry contains:

```json
{
  "id": "uuid",
  "label": "Review the result",
  "prompt": "Review the current result and list any remaining correctness risks.",
  "primary": true
}
```

The field is server-owned. Generic create and update comment APIs do not accept
it. The comment response and realtime payload expose the validated value.

The existing workspace `quick_action` catalog remains separate: catalog actions
are reusable, user-configured commands, while suggested follow-ups are ephemeral
recommendations anchored to one completed task comment.

## Eligibility and Lifecycle

A comment is eligible only when all of the following hold:

- it was authored by an agent;
- it has a valid `source_task_id`;
- the source task completed successfully and wrote non-empty text;
- the server LLM layer is configured;
- it is not a failure/system notice.

Suggestions are generated in a bounded asynchronous post-completion pass using
the existing LLM integration and sanitizer principles from Chat Quick Actions.
An empty or invalid model response stores no suggestions. V1 deliberately does
not invent generic fallback actions, matching current Chat behavior.

The UI renders suggestions only while their anchor is the latest actionable
agent comment in its thread. A newer human reply makes older suggestions stale.
The activation endpoint verifies freshness again, so stale clients cannot run an
outdated suggestion.

Activation serializes clicks per anchor using the existing transaction-scoped
advisory lock, then locks the parent issue before the source comment. This
matches ordinary comment creation, editing, and issue teardown; holding a
comment lock before touching its issue would deadlock against those paths.
Freshness and the stored action are rechecked while these locks are held.
Dispatch still happens through the normal trigger path after commit; this is
not a new exactly-once delivery or workflow engine.

The `comment:follow_ups_updated` enrichment event invalidates the matching
timeline query. It does not patch action IDs into the cache: the event has no
source revision and can arrive after an edit has cleared the suggestions.
One refetch per enrichment event is the deliberate tradeoff for reusing the
authoritative snapshot without adding another cross-client version protocol.

## Activation Flow

```text
click suggestion
  -> POST issue/comment/follow-up run endpoint with suggestion id
  -> load issue, anchor comment, source task, and stored suggestion
  -> derive the responsible agent or squad from trusted task lineage
  -> verify membership and invoke permission
  -> reject stale, missing, or unavailable targets
  -> create an ordinary member reply under the anchor comment
  -> prepend one server-built target mention
  -> call the existing comment trigger path
  -> return ordinary CommentResponse with trigger_outcomes
```

The client never submits a target id or replacement prompt. The stored prompt is
also validated to reject trigger-capable agent and squad mention markup, so one
suggestion can never fan out beyond its derived target.

## UI

Reuse the visual language and interaction behavior of Chat Quick Actions:

- at most three compact pills below the comment body;
- primary action uses the existing brand-subtle treatment;
- full prompt appears in a tooltip;
- all actions disable while one activation is pending;
- outcome messages distinguish `queued`, `coalesced`, `deferred`, and `blocked`;
- no raw control syntax is rendered in the comment body.

The first delivery targets shared Web/Desktop issue views. Mobile remains
unchanged, matching the current platform boundary for Issue Quick Actions.

## Error and Security Rules

- Suggestions are display data, never executable code.
- Server-generated ids address suggestions; array positions are not trusted.
- The target is derived from source-task lineage, not comment text or client
  input.
- Permission is checked in the existing invocation gate.
- Private target existence is not disclosed through error detail.
- Agent/squad mentions inside suggested prompts are rejected.
- Unknown dispatch outcomes render as neutral, never as success.
- Provider failure is best effort and must not fail task completion.

## Verification

Automated coverage is split by layer; passing a lower layer is not evidence of
an end-to-end runtime execution:

- Service sanitizer tests cover bounded, safe actions and primary selection.
- Database-backed tests in `issue_comment_follow_up_test.go` cover generation
  persistence and event publication, disabled/ineligible inputs, provider failure,
  malformed output, and an edit during generation. The provider is an in-process
  fake and never contacts an external model.
- Handler tests cover concurrent clicks plus subsequent retries (one reply and
  one enqueued task), edit/delete/reply while activation waits for the issue
  lock, stale/missing/unsafe sources, private-agent refusal, machine-actor
  middleware refusal, and routing through source agent or squad lineage.
- The shared comment-mutation lock-order test includes follow-up activation
  versus issue teardown.
- UI tests cover action-ID submission, inactive-tail hiding, pending disablement,
  queued/coalesced/deferred/blocked/unknown feedback, and stale-action errors.
  Timeline hook tests cover revisionless enrichment refetch and issue scoping.

- The opt-in Playwright test `e2e/issue-comment-follow-ups.spec.ts` exercises a
  real browser, API, PostgreSQL and WebSocket connection. It authenticates a
  synthetic member, registers a deterministic worker, completes a source task,
  observes generated suggestions, hovers the full prompt, clicks an action,
  verifies its reply and queued task, rejects a repeated activation, and checks
  the claimed task's completion in both the database and original thread. It
  also checks that old pills disappear and queue badges settle after completion.
  The model HTTP provider and daemon claim/start/complete caller are protocol
  fixtures; this does not execute a user-installed Codex CLI.

Manual acceptance still includes native Desktop, WebSocket reconnection and a
configured external LLM -> click -> real runtime completion. Mocked
dispatch-status UI tests do not prove every backend scheduler outcome. Handler
machine-actor tests exercise the actual guard with server-stamped identity;
the browser test additionally uses real synthetic-member authentication.

### Running the opt-in browser test

Use a dedicated, disposable PostgreSQL database with all migrations applied,
a current-source development API, and the Web app pointed at that API and its
WebSocket endpoint. Do not reuse production databases, SMTP, channel keys or
user runtimes. The existing E2E login helper requires development email-code
authentication. Install Chromium with `pnpm exec playwright install chromium`.

For the API, set `MULTICA_LLM_API_KEY=e2e-only`,
`MULTICA_LLM_DEFAULT_MODEL=e2e-fixture`, `MULTICA_LLM_MAX_RETRIES=0`, and
`MULTICA_LLM_BASE_URL=http://127.0.0.1:55436/v1`. If the API runs in Docker on
macOS, use `host.docker.internal` for this provider address. The test starts
and stops the local provider itself; it must be reachable from the API.
Set the API's frontend origin/CORS to the isolated Web origin.

From the repository root, substituting only isolated test-service addresses:

```sh
MULTICA_E2E_FOLLOW_UPS=1 \
DATABASE_URL='postgres://postgres:local-e2e@127.0.0.1:55433/pr7839_e2e?sslmode=disable' \
NEXT_PUBLIC_API_URL=http://127.0.0.1:55434 \
PLAYWRIGHT_BASE_URL=http://127.0.0.1:55435 \
pnpm exec playwright test e2e/issue-comment-follow-ups.spec.ts \
  --project=chromium --workers=1 --trace=on
```

The test removes its issue, tasks, agent and runtime. Dispose of the test
database afterwards to remove the synthetic member/workspace retained by the
shared login helper. Traces contain test authentication traffic and should stay
local; only synthetic-data screenshots are suitable for a public PR.

Record actual commands, tested commit, results and exclusions for each run.
The PR migration is currently `451_comment_suggested_follow_ups`; verify its
number against main before rebasing. No migration change is needed for the
lock-order or cache-invalidation fixes.
