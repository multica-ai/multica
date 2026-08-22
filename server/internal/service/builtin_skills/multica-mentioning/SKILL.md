---
name: multica-mentioning
description: "Use when an issue comment needs to @mention someone — link to a person, trigger another agent, hand work to a squad, or broadcast with @all. Whether to mention at all is covered by the runtime brief, not here."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Mentioning & Delegating

This skill states WHAT a mention link does in the Multica backend, traced to
source. WHETHER to mention at all — loop avoidance, staying silent on
acknowledgements — is in your runtime brief's Mentions section; follow that and
do not repeat it here.

Every claim below is pinned to source in
`references/mentioning-source-map.md`. If behavior ever differs from this
document, the source map is where to re-check it.

## A mention link is built from a real UUID

The backend recognizes a mention only through this Markdown shape:

    [@Label](mention://<type>/<id>)

The parser (`util.MentionRe` in `server/internal/util/mention.go`) accepts
exactly four `<type>` values plus the `all` sentinel, and the `<id>` group
accepts only hex characters and dashes, OR the literal string `all`:

    (member|agent|squad|issue|all)/([0-9a-fA-F-]+|all)

So the link target is a real entity UUID (or `all`), never a display name. The
label between the brackets is free text — that is where the human-readable name
goes.

One `mention://` form deliberately sits OUTSIDE this parser:
`[Label](mention://project/<uuid>)`. `project` is absent from the type group
above, so the backend never parses it and it can enqueue nothing — it is a
render-only link every client makes navigable (a chip on web and desktop, an
ordinary link that opens the project on tap on mobile). That is the whole point:
a project reference should never be able to start a run. Use it freely to point
at a project (see the multica-projects-and-resources skill); everything else in
this document is about the four types (plus `all`) the parser does recognize.

## Step 1 — look up the UUID with `--output json`

A name is not a UUID. Look the UUID up first, from the matching list command:

- a person → `multica workspace member list --output json` → use `user_id`
- an agent → `multica agent list --output json` → use `id`
- a squad  → `multica squad list --output json` → use `id`

For a person the mention id is the `user_id`, NOT the membership-row id — the
backend's own roster formatter uses `user_id` for member mentions. Match by
display name. If the name is ambiguous or absent, do not guess — say so in your
comment instead of emitting a broken link.

## Step 2 — the four types and exactly what each enqueues

Format: `[@Name](mention://<type>/<uuid>)`. The `<type>` and the id source must
match, or the link resolves to the wrong entity (or to nothing).

| To…                  | type     | uuid from       | What the backend does                                    |
| -------------------- | -------- | --------------- | -------------------------------------------------------- |
| trigger an agent     | `agent`  | agent.id        | enqueues a run for that agent (`EnqueueTaskForMention`)  |
| hand work to a squad | `squad`  | squad.id        | resolves the squad's `leader_id` and enqueues a run for the LEADER agent |
| link a person        | `member` | member.user_id  | renders a link; enqueues NOTHING — no agent run          |
| reference an issue   | `issue`  | issue.id        | renders a link; enqueues NOTHING — always safe           |

The mention trigger set is computed by `computeMentionedAgentCommentTriggers`
(`server/internal/handler/comment.go`); the comment path folds that result into
`computeCommentAgentTriggers` and enqueues it via `enqueueCommentAgentTriggers`.
It acts on two types only: the `squad` branch resolves the squad and adds its
leader to the trigger set; everything that is not `agent` after that is skipped
(`if m.Type != "agent" { continue }`), then the `agent` branch adds that agent.
A `member` or `issue` mention reaches neither branch, so it enqueues no task.

A `member` mention therefore does NOT make a person "run", and this skill does
NOT claim it delivers a notification through the Go comment handler — there is
no such code path in that handler (see the source map). What is verified is the
contract above: only `agent` and `squad` mentions enqueue work.

## Preview and per-comment suppression

Newer clients can call `POST /api/issues/{id}/comments/trigger-preview` before
creating or editing a comment. The preview endpoint uses the same
`computeCommentAgentTriggers` function as create and edit re-triggering, so the
displayed agent chips come from backend rules, not from a client-side
reimplementation.

When previewing an edit, clients may send `editing_comment_id`. The server
validates that the comment belongs to the same workspace and issue, derives or
checks the edit's parent comment context, and excludes only pending tasks whose
`trigger_comment_id` is that same comment. Pending tasks from any other comment
on the issue still dedupe the preview.

When creating or editing a comment, clients may send an optional
`suppress_agent_ids` array. The server still computes the full trigger set
first, then removes those agent IDs as a post-filter. A missing or empty field
preserves the old behavior. A valid UUID that is not in the computed trigger set
is a no-op; a malformed UUID is rejected at the request boundary.

## @all is the broadcast type

`@all` uses the literal `all`, never a UUID:

    [@all](mention://all/all)

It addresses everyone on the issue. It does NOT make any specific agent run.
And it is special at trigger time: a comment that carries an `@all` mention is
treated as a broadcast that SUPPRESSES the issue assignee's automatic
on-comment trigger (and the other implicit routing fallbacks — thread parent /
conversation owner). Use `@all` to announce, not to request work from the
assignee.

`@all` only suppresses those IMPLICIT routes. An EXPLICIT `@agent` / `@squad`
mention in the same comment still fires normally (MUL-5411): a comment reading
`[@all](mention://all/all) heads up — [@Preflight](mention://agent/<uuid>)
please take this` enqueues Preflight and nobody else. Explicit mentions win over
the broadcast; see `computeCommentAgentTriggers` in
`server/internal/handler/comment.go`, where the explicit-mention branch is
evaluated BEFORE the `@all` short-circuit.

## What does NOT happen (so the result doesn't surprise you)

None of these start a fresh run, and none produce an error response — but they
are three different things, and the response tells you which. A mention that
never parsed is a truly silent no-op. One that parsed and was refused comes back
in `trigger_outcomes` as `status: "blocked"` with a `reason_code`. One whose
target is already busy comes back `coalesced` or `deferred`: no second run, but
your comment IS folded into the task that is already running, so it still gets
read. Read that array after posting — it is the only place any of this shows up.

- **A name where a UUID belongs.** `mention://member/Alice` is dead. The id
  group accepts only hex+dashes or `all`; the non-hex letters in a typical name
  make the whole pattern fail to match, so the parser returns nothing.
- **A hex-ish but wrong UUID.** A well-formed-looking UUID that no entity owns
  DOES parse, then no-ops at lookup: the workspace-scoped query finds no agent
  and the mention is reported blocked with `invocation_not_allowed`. That code
  is deliberately ambiguous — **a typo'd UUID and a genuine permission denial
  look identical on purpose**, because the id you typed could name a private
  agent in another workspace and the reason must not confirm that it exists.
  **So when you see `invocation_not_allowed`, check the UUID against the live
  roster BEFORE you touch any visibility or invocation setting** (MUL-5548);
  `multica squad member list <squad-id> --output json` returns the `member_id`
  to build the mention from. An id that matches the pattern but is NOT a valid
  UUID at all (`mention://agent/-`) is rejected by the id parser and blocked
  with `target_unavailable` instead — a non-UUID names no entity anywhere, so
  it conceals nothing. Neither case is ever an error response.
- **An already-pending task.** Even a correct `@agent`/`@squad` starts no second
  run when the target already has a pending task on this issue
  (`HasPendingTaskForIssueAndAgent`). This is a fold, not a drop: the comment
  merges into that task and the outcome is `coalesced` (same reviewed head) or
  `deferred` (different head) — do NOT re-post it as "the mention didn't work".
  Edit preview is the only exception: `editing_comment_id` ignores pending tasks
  from the same comment being edited, because save cancels those old tasks
  before it re-computes triggers. It is still comment-scoped, not an agent-wide
  bypass.
- **An archived agent, or one with no runtime bound** (likewise a squad whose
  leader is): blocked with `target_unavailable` and `runtime_offline`
  respectively. Both are checked only AFTER the invoke gate, so a caller who may
  not invoke the target never learns its state.
- **A private agent you cannot invoke:** blocked — the mention path gates on
  `canInvokeAgent` for both `@agent` and `@squad`. That is the *run* gate, not
  the *see* gate: since MUL-3963 a workspace admin who can open a private agent
  in the UI still may not trigger it, so being able to view the target says
  nothing about being able to mention it. (The `canEnqueueSquadLeader` wrapper
  is the squad assignment/promote path, not this one; the child-done wake is
  ungated — see the multica-squads skill.)

A chain that crosses issues keeps its human (MUL-6490). The A2A gate judges the
human at the top of your chain, and that human travels on the comment you write:
the comment records the run that authored it, so the run it wakes inherits your
originator. This holds when you comment on a DIFFERENT issue than the one you are
running on — the ordinary "create issue Y, then coordinate there" flow — so a
delegation that works on your own issue keeps working on the issue you just
created. It does not go the other way: nothing ever substitutes a different human
(your agent's owner, or the target issue's originator), so if your chain has no
human at its top, member-scoped allow-lists stay closed no matter which issue you
move to.

One nuance for automation (MUL-4857): when an UNATTRIBUTED autopilot run (a
schedule/webhook dispatch has no human originator, so the A2A gate has no human
to key on) delegates by `@mention` while working on the issue that autopilot
created, the invoke gate falls back to the **autopilot creator** as the effective
invoking user — the same principal that admitted the first dispatch. So a mid-run
`@agent` / `@squad` delegation fires exactly when the autopilot creator could
invoke that target (owner / `public_to` match), and stays skipped otherwise. It
is authorization only — the enqueued run's originator/attribution is unchanged.
This fallback is bound to verified task lineage: it applies only when the
delegating run's own task is the one working on that autopilot issue (author ==
task agent, `task.issue_id` == this issue), so a run doing work elsewhere can
never borrow another autopilot creator's authority by commenting on its issue.
The same authority carries the plain assigned-squad-leader wake (a worker's
result comment on the autopilot issue can still wake the leader), and it survives
a busy target: if the mentioned agent is already running, the delegation is
replayed at that run's completion under the same authority, so it is never lost.
An edit is treated as a fresh action — it re-derives the comment's lineage from
the editing action. Only the agent author editing its OWN comment re-stamps the
lineage to the editing task; any other editor — including a workspace owner/admin
editing an agent's comment — CLEARS it. So editing an old autopilot comment from
an unrelated issue, or an admin editing an agent's comment (manage rights, not
invoke rights), fails closed at the deferred completion-reconcile instead of
reusing the original run's authority.

## Incorrect → Correct

Incorrect: `@alice please review`
  → plain text, no link, parses to nothing, nobody is reached.

Incorrect: `[@Alice](mention://member/Alice) please review`
  → "Alice" is not a UUID; the id group rejects the non-hex letters, the
  pattern does not match, the link is silently dead.

Correct:
  1. `multica workspace member list --output json`  → Alice's `user_id` = 7f3a…
  2. `[@Alice](mention://member/7f3a…) please review`
     → a real `user_id` parses; the link renders and resolves to Alice.

@all broadcast: `[@all](mention://all/all) heads up` — addresses everyone,
runs no specific agent, and suppresses the assignee auto-trigger.

These exact shapes are pinned by a Go behavior test
(`TestMentioningSkillTeachesTheParserContract`) that feeds them through
`util.ParseMentions`: the name form parses to nothing, the real-UUID form
parses, `@all` parses to `{all, all}`, and a wrong `type` with a real UUID
still parses (which is why the type must match the id source).

## References

`references/mentioning-source-map.md` — file:line evidence for the regex, the
enqueue branches, the @all suppression, and the CLI id-source mapping, plus the
explicit note that no member-notification delivery path exists in the Go
comment handler.
