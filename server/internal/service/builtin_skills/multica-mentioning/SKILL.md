---
name: multica-mentioning
description: "Use when an issue comment needs to @mention someone — link to a person, trigger another agent, or hand work to a squad."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Mentioning & Delegating

WHAT a mention link does in the Multica backend, source-traced in
`references/mentioning-source-map.md`. WHETHER to mention (loop avoidance,
silence on acknowledgements) lives in the brief's Mentions section — follow it.

## Mention = real UUID

The backend recognizes mentions only as
`[@Label](mention://<type>/<id>)`. The parser (`util.MentionRe` in
`server/internal/util/mention.go`) accepts four `<type>` values plus the `all`
sentinel; `<id>` = hex + dashes or the literal `all`:

```
(member|agent|squad|issue|all)/([0-9a-fA-F-]+|all)
```

So the target is a real entity UUID (or `all`), never a display name; the
label is free text. `[Label](mention://project/<uuid>)` sits OUTSIDE the
parser — render-only, can enqueue nothing, and that is the point: a project
reference must never start a run.

UUID lookup (name is not a UUID):

- person → `multica workspace member list --output json` → **`user_id`** (NOT the membership-row id)
- agent → `multica agent list --output json` → `id`
- squad → `multica squad list --output json` → `id`

Ambiguous/absent name → say so in the comment; never emit a broken link.

## Four types and what each enqueues

`[@Name](mention://<type>/<uuid>)`; type and id source must match.

| To… | type | uuid from | Backend does |
|---|---|---|---|
| trigger an agent | `agent` | agent.id | enqueues a run |
| hand to a squad | `squad` | squad.id | enqueues the squad's `leader_id` |
| link a person | `member` | member.user_id | renders; enqueues NOTHING |
| reference an issue | `issue` | issue.id | renders; enqueues NOTHING |

`computeMentionedAgentCommentTriggers` (`server/internal/handler/comment.go`)
folds into `computeCommentAgentTriggers`, enqueued via
`enqueueCommentAgentTriggers`. Only two types act: `squad` resolves the
leader; everything non-`agent` is skipped (`if m.Type != "agent"`). A `member`
mention does NOT make the person "run" — there is no member-notification path
in the Go comment handler (see source map).

## Preview & suppression

`POST /api/issues/{id}/comments/trigger-preview` uses the same
`computeCommentAgentTriggers` as create/edit, so displayed chips are backend
truth. `editing_comment_id` (edit preview) excludes only pending tasks whose
`trigger_comment_id` is that comment; others still dedupe. Optional
`suppress_agent_ids` array: full trigger set is computed first, then the
listed agent IDs are removed as a post-filter; malformed UUID → rejected at
the boundary, missing/empty → old behavior.

## @all broadcast

`[@all](mention://all/all)` addresses everyone, runs no specific agent, and
SUPPRESSES the assignee's implicit on-comment trigger (and other implicit
fallbacks). Use it to announce, not to delegate. It suppresses only IMPLICIT
routes: an explicit `@agent`/`@squad` in the same comment still fires
(MUL-5411) — the explicit-mention branch is evaluated BEFORE the `@all`
short-circuit in `computeCommentAgentTriggers`.

## What does NOT happen

None start a run and none error — but they differ, and `trigger_outcomes`
after posting tells you which: never parsed = silent no-op; parsed but
refused = `status: "blocked"` + `reason_code`; busy target = `coalesced` or
`deferred` (comment folded into the running task, still read).

- **Name where a UUID belongs** — `mention://member/Alice` is dead: non-hex
  letters fail the pattern.
- **Hex-ish wrong UUID** — parses, no-ops at lookup, reported blocked with
  `invocation_not_allowed`. Deliberately ambiguous: a typo'd UUID and a real
  permission denial look identical (the id could name a private agent in
  another workspace). Check the UUID against the live roster BEFORE touching
  visibility settings (MUL-5548); `multica squad member list <squad-id> --output json`
  returns `member_id` for building the mention. A pattern-but-not-UUID value
  (`mention://agent/-`) is rejected with `target_unavailable` — conceals
  nothing. Neither is ever an error response.
- **Already-pending task** — correct `@agent`/`@squad` starts no second run
  (`HasPendingTaskForIssueAndAgent`); it FOLDS: outcome `coalesced` (same
  head) or `deferred` (different head). Do not re-post. Only edit-preview
  (`editing_comment_id`) ignores the same comment's pending tasks — saving
  cancels them first; still comment-scoped.
- **Archived or runtime-unbound agent** (or squad leader) — blocked with
  `target_unavailable` / `runtime_offline`; checked only AFTER the invoke
  gate, so non-invokers never learn the state.
- **Private agent you cannot invoke** — blocked: gated on `canInvokeAgent`
  for `@agent` and `@squad` (run gate, not see gate; MUL-3963).
  `canEnqueueSquadLeader` is the squad assignment path, not this one.

**Autopilot delegations (MUL-4857):** an unattributed schedule/webhook
autopilot has no human originator. When its run delegates by `@mention` while
working on the autopilot-created issue, the invoke gate falls back to the
**autopilot creator** as effective invoker — fires exactly when the creator
could invoke the target (owner / `public_to` match). Authorization only;
originator/attribution of the enqueued run unchanged. Bound to verified
lineage (author == task agent, `task.issue_id` == issue), so work elsewhere
can't borrow the authority; survives busy targets (replayed on completion);
edits re-derive lineage — only the agent editing its OWN comment re-stamps,
any other editor CLEARS it (fails closed at deferred reconcile).

## Incorrect → Correct

- `@alice please review` → plain text, parses nothing.
- `[@Alice](mention://member/Alice)` → non-hex, dead link.
- Correct: `multica workspace member list --output json` → `user_id`, then
  `[@Alice](mention://member/<user_id>) please review`.
- Broadcast: `[@all](mention://all/all) heads up`.

Pinned by `TestMentioningSkillTeachesTheParserContract` via
`util.ParseMentions`: name form parses nothing, real-UUID parses, `@all` →
`{all, all}`, wrong `type` with real UUID still parses — type must match id
source.

## References

`references/mentioning-source-map.md` — file:line evidence for the regex,
enqueue branches, `@all` suppression, id-source mapping; no
member-notification delivery path exists in the Go comment handler.
