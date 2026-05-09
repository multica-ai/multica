# RFC: Comment Thread Resolve

**Status:** Draft
**Issue:** MUL-1895
**Author:** Lambda
**Date:** 2026-05-09

## Summary

Add a Linear-style "resolve" action to comment threads. A resolved thread is hidden from the activity feed by default and replaced with a single collapsed bar (e.g. "1 resolved comment from jiayuan"). Clicking the bar expands the thread; an "Unresolve" action restores it to the normal feed.

## Motivation

Long-lived issues accumulate review-style discussions ("can you change X?", "fixed", "lgtm") that are noise once the point is settled. Today every comment stays inline forever, so the activity feed is dominated by historical chatter and the actionable signal — current status, latest decision — gets buried. Linear's resolve mechanic is the established pattern for this and is what users expect.

## Goals

- A thread author or any workspace member can resolve / unresolve any thread.
- Resolved threads collapse into a single aggregated row, like the screenshot in MUL-1895.
- Resolution state is server-persisted and synced via WebSocket — not just local UI state.
- Clear audit trail: who resolved, when.

## Non-goals

- Resolving an individual reply within a thread. Resolution operates at thread (root comment) granularity only — same as Linear.
- "Required reviewers must resolve" gating. Anyone with comment access can resolve.
- Per-line / inline-code resolves (out of scope; this is an issue-level discussion model, not a diff review tool).
- Backfilling old comments as resolved.

## UX

### Resolve action

- Each root comment (`parent_id IS NULL`) gains a "Resolve" item in its overflow menu, alongside Edit / Delete.
- Replies do not have their own resolve action.
- Resolving a thread is optimistic: the thread immediately collapses; rollback on failure.

### Collapsed presentation

- A resolved thread renders as a single row with a check-circle icon, `"<N> resolved comment<s> from <author-list>"`, and a chevron toggle.
- `<N>` counts the root + its descendants. `<author-list>` is the deduplicated set of authors across the thread; show up to 2 names with an "and N others" suffix when needed.
- Clicking the row expands the thread inline. Expanded state is per-user, ephemeral, not persisted (matches Linear).
- Inside the expanded thread, an "Unresolve" action appears on the root comment.

### Ordering

- Resolved bars stay in their original chronological position in the feed. They do not get hoisted to the top or pushed to the bottom — moving them would break the audit narrative ("Linear set SLA → SLA breached → discussion → moved to Done").

### Filter affordance (out of initial scope, captured for future)

A future "Show resolved" toggle at the top of the activity feed could expand all resolved threads at once. Not part of this RFC.

## Data Model

Comments already have `parent_id` (migrations 017, 018) so threading is in place.

### New columns on `comment`

```sql
ALTER TABLE comment
  ADD COLUMN resolved_at  TIMESTAMPTZ NULL,
  ADD COLUMN resolved_by_type TEXT NULL,   -- 'member' | 'agent', polymorphic like author
  ADD COLUMN resolved_by_id UUID NULL;

ALTER TABLE comment
  ADD CONSTRAINT comment_resolved_consistency
    CHECK (
      (resolved_at IS NULL AND resolved_by_type IS NULL AND resolved_by_id IS NULL)
      OR
      (resolved_at IS NOT NULL AND resolved_by_type IS NOT NULL AND resolved_by_id IS NOT NULL)
    );

-- Application-level invariant: only root comments (parent_id IS NULL) may carry resolved_at.
-- We do NOT enforce this with a partial CHECK because parent_id can be set after creation
-- in edge cases; instead the handler rejects resolve on non-root comments.

CREATE INDEX comment_issue_resolved_at_idx
  ON comment (issue_id, resolved_at);
```

The index supports the typical "list issue comments, group resolved into one row" path in the API.

Migration file: `server/migrations/079_comment_resolved_at.{up,down}.sql`. (Last existing migration is 078.)

### Why a timestamp instead of a boolean

`resolved_at` lets us order events ("resolved most recently"), audit ("resolved 5d ago"), and is trivially indexable. A boolean throws information away. This matches the existing pattern (e.g. `deleted_at`-style soft state elsewhere).

## API

All routes are under `/api`, scoped by `X-Workspace-ID`.

### New endpoints

```
POST   /api/comments/{id}/resolve     -> 200 { comment: CommentResponse }
DELETE /api/comments/{id}/resolve     -> 200 { comment: CommentResponse }
```

Both are idempotent. `POST` on an already-resolved thread is a no-op success; `DELETE` on an unresolved thread is the same.

Validation rules (handler side):

- Comment must exist in caller's workspace (use `loadCommentForUser` pattern, mirroring `loadIssueForUser`).
- Comment must have `parent_id IS NULL`. Otherwise return `400 Bad Request: only root comments can be resolved`.
- `parseUUIDOrBadRequest` for `{id}` per backend conventions.

### Response shape

`CommentResponse` gains:

```ts
resolved_at: string | null;       // RFC3339
resolved_by: { type: "member" | "agent"; id: string } | null;
```

Existing fields unchanged. A new field is purely additive; older desktop clients ignore it and render threads as today (degraded gracefully — no resolve UI, but no breakage). This is consistent with the API Response Compatibility rules in CLAUDE.md.

### Why two endpoints rather than one PUT toggle

Idempotent verbs make retries safe and the audit trail unambiguous. A `PUT /resolve` with body `{resolved: true|false}` would couple two semantically different operations (set vs. clear) and require both server and clients to disambiguate the body — more code, more drift surface, no benefit.

## Events

Add to `server/pkg/protocol/events.go`:

```go
EventCommentResolved   = "comment:resolved"
EventCommentUnresolved = "comment:unresolved"
```

Handlers publish after successful DB write. Frontend WS listeners invalidate the issue's comment query (`["issues", wsId, issueId, "comments"]`) — no direct cache writes (per state-management rules).

We deliberately do NOT piggyback on `EventCommentUpdated`, because update events are also fired for content edits and reaction changes; consumers that only care about resolve transitions (e.g. a notification rule "ping me when my thread is resolved") would have to re-diff the comment to figure out which axis changed.

## Frontend

### Types

`packages/core/types/comment.ts` — extend the `Comment` interface with `resolved_at` and `resolved_by` (parsed via the existing `parseWithFallback` schema, defaulting to `null`).

### Mutations

`packages/core/issues/mutations.ts` — add `useResolveCommentMutation` and `useUnresolveCommentMutation`. Optimistic: patch the comment in the cache, fire the request, roll back on error, invalidate on settle. Mirror `useDeleteCommentMutation`.

### Rendering

`packages/views/issues/components/issue-detail.tsx` is the activity feed:

1. Fold consecutive comments by thread (root + descendants by `parent_id`). Threading folding likely already exists; if not, this RFC introduces it.
2. For each thread, check `root.resolved_at`. If set, render `<ResolvedThreadBar />` instead of the full thread.
3. `<ResolvedThreadBar />` is a new component in `packages/views/issues/components/resolved-thread-bar.tsx`. Click → toggle local "expanded" state for this thread on this user's session (in-memory `useState`; or extend `comment-collapse-store` with a new `expandedResolved: Record<issueId, string[]>` map for cross-tab persistence — leaning toward the simpler in-memory approach to match Linear's behavior).
4. When expanded, render the normal thread plus an "Unresolve" entry on the root.

`comment-card.tsx` adds the menu items and a small "Resolved by X · 2d ago" footer when the comment is shown inside an expanded resolved thread.

### Empty state for expand toggle

If a resolved thread has no replies, the bar still says "1 resolved comment from <author>"; expanding shows just the root. This matches the screenshot.

## Permissions

- Resolve / unresolve: any workspace member or agent with comment access (today, all members of the workspace).
- We do NOT restrict to author-only. Linear allows anyone to resolve, which is the right default for small teams (2–10 people, per the product overview). If we get reports of abuse we can tighten later.

## Edge Cases

- **Replying to a resolved thread.** Posting a reply to any comment in a resolved thread automatically unresolves the thread. This is what users expect — a new reply means the discussion is alive again. Implemented in the `CreateComment` handler: if `parent_id` resolves to a thread whose root has `resolved_at IS NOT NULL`, clear `resolved_at` in the same transaction and emit `EventCommentUnresolved`.
- **Editing a comment in a resolved thread.** Edits do not unresolve. (Editing for typos shouldn't reopen.)
- **Deleting the root of a resolved thread.** Cascade applies (FK ON DELETE CASCADE on `parent_id`, set up in migration 018) — the entire thread disappears. No special handling.
- **Race: two users resolve at once.** Last write wins on `resolved_at`, but since both writes set it to a non-null value the observable result is identical. Resolve / unresolve race: optimistic UI may briefly disagree until the WS event invalidates and reconciles.
- **Older desktop clients.** They don't know about `resolved_at`. They render every thread inline, which is the current behavior — degraded but correct. New clients hitting older servers see `resolved_at: null` everywhere (field absent) and likewise show the current behavior. Both directions are safe.

## Migration & Rollout

1. Land the migration (`079_comment_resolved_at`) and backend handlers behind no flag — additive columns and new endpoints don't affect existing flows.
2. Land frontend changes. They're feature-detected by the presence of the `resolved_at` field on the comment response (defaulting to `null`).
3. No backfill. All historical comments stay unresolved.

## Alternatives Considered

**Per-comment resolve, not per-thread.** Rejected: matches neither the Linear UX users asked for nor the "1 resolved comment from X" aggregation in the screenshot. Adds significant rendering complexity (interleaved resolved + unresolved within one thread).

**Client-side-only collapse via `comment-collapse-store`.** Rejected: doesn't sync across devices, doesn't support agent-driven resolves, leaves no audit trail, can't drive notifications. `comment-collapse-store` stays as the *expand-after-resolve* UI state holder, not as the resolution truth.

**Single `PUT /comments/{id}` with `resolved` boolean.** Rejected — see "Why two endpoints" above.

**Storing resolution as a separate `comment_resolution` table.** Rejected: 1:1 with comment, no historical resolution log we'd want to keep (resolve / unresolve is a toggle, the latest state is what matters). A second table buys nothing and costs a join on the hot read path.

## Open Questions

1. Should resolving a thread also dismiss inbox notifications referencing comments inside it? (Probable yes, but inbox model needs its own pass — leaving out of this RFC.)
2. Should we expose resolution state via the public CLI (`multica issue comment list` showing a `resolved_at` column, a `multica issue comment resolve <id>` verb)? Recommended yes, mirroring the API additions; can be a follow-up PR.
3. Linear distinguishes "resolved" from "resolved-and-hidden" — collapsed but still scannable vs. hidden behind a filter. We're doing the former. Worth confirming with the user before implementation.

## Implementation Plan (after RFC approval)

1. Migration 079 + sqlc queries (`ResolveComment`, `UnresolveComment`, updated `ListIssueComments` to include the new columns).
2. Backend: `loadCommentForUser` helper, two new handler endpoints, event constants, auto-unresolve on reply, response schema update.
3. Frontend: type + zod schema update, mutations, `<ResolvedThreadBar />`, comment-card menu items, activity-feed thread folding.
4. Tests: Go handler tests for resolve / unresolve / non-root rejection / auto-unresolve-on-reply. Vitest tests for `<ResolvedThreadBar />` rendering, optimistic mutation rollback. One e2e covering resolve → bar appears → reply → bar disappears.
5. CLI verbs (follow-up).
