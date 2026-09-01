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

Backend tests cover eligibility, parsing and sanitization, persistence, stale
activation, target derivation, permission refusal, mention fanout prevention,
queued/coalesced/deferred outcomes, and realtime supplementation.

Frontend tests cover primary styling, prompt tooltip, pending state, activation,
stale hiding, and honest outcome feedback. Typecheck, lint, focused suites, Go
tests, SQL generation checks, and the repository verification pipeline run before
submission.
