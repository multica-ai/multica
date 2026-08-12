# Mentioning — source map

Every `SKILL.md` claim traces below; re-derive current tree before trusting a
line number — behavior contract, line pointer.

## mention grammar (what parses)

| Fact | Source |
|---|---|
| `MentionRe` — only recognizer of mention links | `server/internal/util/mention.go:16` |
| Pattern: `` `\[@?(.+?)\]\(mention://(member\|agent\|squad\|issue\|all)/([0-9a-fA-F-]+\|all)` `` | `server/internal/util/mention.go:16` |
| `<type>` = `member\|agent\|squad\|issue\|all`; `<id>` = `[0-9a-fA-F-]+` or literal `all` — names fail | `server/internal/util/mention.go` |
| `ParseMentions` extracts/dedups `{Type, ID}` | `server/internal/util/mention.go` |
| `Mention.Type` enum = "member", "agent", "issue", "all" (squad in regex) | `server/internal/util/mention.go` |
| `HasMentionAll` reports any parsed `all` | `server/internal/util/mention.go` |
| **`project` NOT in type group** — `[Label](mention://project/<uuid>)` never parses, can enqueue nothing | `server/internal/util/mention.go:16` |

Parser behavior tests (pin the shapes the skill teaches):
`server/internal/util/mention_test.go`: `mention://member/<real-uuid>` parses `{member, uuid}`;
`mention://all/all` parses `{all, all}` (47-50); `mention://agent/<uuid>`
parses with `[brackets]` labels; name form (`mention://member/Alice`) parses
nothing; wrong type with real UUID still parses.

## Enqueue branches (`server/internal/handler/comment.go`)

| Fact | Source |
|---|---|
| `computeMentionedAgentCommentTriggers` iterates `mention://` hits in comment order | comment.go |
| `squad` branch: resolve squad, read `LeaderID`, add leader trigger | comment.go |
| `squad` → `EnqueueTaskForSquadLeader` | comment.go |
| Everything not `agent` skipped: `if m.Type != "agent" { continue }` | comment.go |
| `agent` branch: load agent in workspace, add agent trigger; → `EnqueueTaskForMention` (a run) | comment.go |
| **`member`/`issue` reach neither branch — enqueue NOTHING** | comment.go |

## Preview suppression

| Fact | Source |
|---|---|
| Preview route: `POST /api/issues/{id}/comments/trigger-preview` | `server/cmd/server/router.go` |
| Preview loads issue, expands identifiers, calls `computeCommentAgentTriggers` | `server/internal/handler/comment.go` |
| Preview accepts `content`, optional `parent_id`, `editing_comment_id`; response = agent `id`, `name`, `avatar_url`, `source`, `reason` | comment.go |
| `editing_comment_id` UUID-scoped same workspace/issue; validates/derives parent context | comment.go |
| `CreateCommentRequest` / `UpdateComment` accept optional `suppress_agent_ids` | comment.go |
| Suppression parsed at request boundary; full trigger set computed, then `filterSuppressedCommentAgentTriggers` | comment.go |
| Edit-preview dedup excludes only tasks whose `trigger_comment_id` equals the edited comment | `hasPendingTaskForIssueAndAgent` (comment-scoped exclusion) |
| Dedup shared helper used for: agent-assignee (issue.go), assigned squad leader, mentioned squad leader, direct agent mention | comment.go / issue.go |
| Regression tests: 4 edit-preview trigger sources positive; other-comment pending task still dedupes (negative); `suppress_agent_ids` filters update-triggered tasks | comment tests |

## Plain replies and implicit routing

| Fact | Source |
|---|---|
| Direct reply to an agent resolves through `routeReplyToParentAuthor` before any assignee fallback | `server/internal/handler/comment.go` (`parentComment.AuthorType == "agent"`) |
| Member-authored thread with an explicit or task-derived agent owner resolves via `routeThreadRootOwners`, returns before the final fallback | comment.go (`routeThreadRootOwners`) |
| Direct parent is a member and no thread owner handled it → no trigger, no `routeAssigneeFallback` | comment.go (plain member-to-member reply) |
| Top-level member comments keep the final agent/squad assignee fallback (no parent) | comment.go (final `routeAssigneeFallback`) |
| Regression: agent and squad assignees through trigger preview + real creation/enqueue | `server/internal/handler/comment_trigger_preview_test.go` (`PlainReplyToUnownedMemberRootSkipsAssigneeFallback`) |

## Guards and outcomes

A parsed mention is never silently dropped: every guard either records a
blocked outcome with a stable `reason_code`, or hands off to enqueue →
`queued` / `coalesced` / `deferred`. Only a mention never parsed at all is a
true silent no-op.

| Guard | Outcome | Source |
|---|---|---|
| mentioned agent archived / no runtime bound | blocked `target_unavailable` / `runtime_offline` | comment.go |
| squad leader archived / no runtime bound | blocked `target_unavailable` / `runtime_offline` | comment.go |
| actor cannot INVOKE private agent (`canInvokeAgent`, not `canAccessPrivateAgent` — MUL-3963) | blocked `invocation_not_allowed` | comment.go |
| actor cannot INVOKE private squad leader | blocked `invocation_not_allowed` | comment.go |
| well-formed UUID resolves no agent in workspace | blocked `invocation_not_allowed` (deliberately ambiguous) | comment.go |
| id not a valid UUID (`mention://agent/-`) | blocked `target_unavailable` | comment.go |
| `canEnqueueSquadLeader` (loads leader, delegates `canInvokeAgent`) — squad assignment/promote path, NOT mention gate | definition | comment.go |
| autopilot-delegation invoke authority: unattributed autopilot run delegating on its own issue falls back to autopilot creator | comment.go |
| autopilot-delegation on DEFERRED path: busy-target delegation replays at completion under same authority | comment.go |
| authority lineage persisted per-action: only the agent editing its OWN comment re-stamps lineage | comment.go |

## @all broadcast suppression

| Fact | Source |
|---|---|
| `HasMentionAll` reports any parsed `all` | `server/internal/util/mention.go` |
| `@all` without explicit `@agent`/`@squad` suppresses every implicit route (assignee, thread parent, conversation owner) | `server/internal/handler/comment.go` |
| `@all` does NOT suppress EXPLICIT mentions in the same comment — explicit branch evaluated before the `@all` short-circuit (MUL-5411) | comment.go |
| `@all` never enqueues a specific agent (neither `squad` nor `agent` branch) | comment.go |
| Tests: `@all` alone → 0 agents; `@all`+`@agent` → mentioned agent only; `@all`+`@squad` → leader only | comment tests |

## CLI id sources

| List command | Field used as mention id | Source |
|---|---|---|
| `workspace member list` | `user_id` (NOT membership-row id) | `server/cmd/multica/cmd_workspace.go` |
| `agent list` | `id` | `server/cmd/multica/cmd_agent.go:365` |
| `squad list` | `id` | `server/cmd/multica/cmd_squad.go:57` |
| Member mentions use `user_id`; confirmed by backend roster formatter | | `server/internal/handler/squad.go` |

Non-claim: member-notification delivery does not exist in the Go comment
handler (`server/internal/handler/comment.go:1397,1437-1439` — only an
unrelated "log spam" avoidance on unchanged threads). Verified contract is
narrow: `member`/`issue` mentions render as links and enqueue no agent run;
only `agent` and `squad` mentions enqueue work. If notification UX exists, it
is not in the handler, so this skill makes no claim about it.
