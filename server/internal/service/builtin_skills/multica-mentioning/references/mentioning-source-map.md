# Mentioning — source map

Every claim in `SKILL.md` traces to a line below. Re-derive against the current
tree before trusting any line number; the behavior is the contract, the line is
a pointer.

## The mention grammar (what parses)

| Fact | Source |
| --- | --- |
| `MentionRe` — the only recognizer of a mention link | `server/internal/util/mention.go:16` |
| Pattern: `` `\[@?(.+?)\]\(mention://(member\|agent\|squad\|issue\|all)/([0-9a-fA-F-]+\|all)\)` `` | `server/internal/util/mention.go:16` |
| `<type>` group = `member \| agent \| squad \| issue \| all` | `server/internal/util/mention.go:16` |
| `<id>` group = `[0-9a-fA-F-]+` (hex + dashes) **or** the literal `all` — so a typical name with non-hex letters never matches | `server/internal/util/mention.go:16` |
| `ParseMentions` extracts and dedups `{Type, ID}` from `m[2]`/`m[3]` | `server/internal/util/mention.go:24-37` |
| `Mention.Type` doc enum = "member", "agent", "issue", or "all" (squad added in regex) | `server/internal/util/mention.go:7` |
| `HasMentionAll` reports whether any parsed mention is `all` | `server/internal/util/mention.go:40-47` |

### Parser behavior tests (pin the example shapes the skill uses)

| Case proven | Source |
| --- | --- |
| `mention://member/<real-uuid>` parses to `{member, uuid}` | `server/internal/util/mention_test.go:42-45` |
| `mention://all/all` parses to `{all, all}` | `server/internal/util/mention_test.go:47-50` |
| `mention://agent/<uuid>` parses; label may contain `[brackets]` | `server/internal/util/mention_test.go:13-35` |
| plain text with no `mention://` parses to `nil` | `server/internal/util/mention_test.go:57-60` |
| Skill eval: a name where a UUID belongs (`mention://member/Alice`) parses to `nil`; a bare `@name` parses to `nil`; a real UUID parses; `@all` → `{all, all}`; a **wrong** type with a real UUID still parses (points at the wrong entity) | `server/internal/service/builtin_skills_test.go:101-157` |

## What each mention type enqueues

| Fact | Source |
| --- | --- |
| `computeCommentAgentTriggers` is the shared comment trigger computation used by preview and enqueueing | `server/internal/handler/comment.go:1159-1195` |
| `computeMentionedAgentCommentTriggers` builds the mention trigger set; `enqueueCommentAgentTriggers` is the shared enqueue helper | `server/internal/handler/comment.go:1381-1467,1124-1157` |
| Comment creation runs `triggerTasksForComment`, which computes triggers, applies suppressions, then enqueues | `server/internal/handler/comment.go:1069,1092-1098` |
| Comment edit re-triggering also runs `triggerTasksForComment` after cancelling old tasks for the edited comment | `server/internal/handler/comment.go:1577-1594` |
| `squad` branch: resolve squad in workspace, read `LeaderID`, add the leader trigger | `server/internal/handler/comment.go:1397-1435` |
| `squad` → shared enqueue helper calls `EnqueueTaskForSquadLeader` | `server/internal/handler/comment.go:1141-1147` |
| Everything not `agent` after the squad branch is skipped: `if m.Type != "agent" { continue }` | `server/internal/handler/comment.go:1437-1439` |
| `agent` branch: load agent in workspace, then add the agent trigger | `server/internal/handler/comment.go:1440-1464` |
| `agent` → shared enqueue helper calls `EnqueueTaskForMention` (a run for that agent) | `server/internal/handler/comment.go:1148-1154` |
| **`member` and `issue` mentions reach neither branch — they enqueue NOTHING.** A `member` mention fails the `!= "agent"` skip at lines 1437-1439 (the squad branch above it only matches `squad`); an `issue` mention does the same. | `server/internal/handler/comment.go:1397,1437-1439` |

## Preview and suppression

| Fact | Source |
| --- | --- |
| Preview route: `POST /api/issues/{id}/comments/trigger-preview` | `server/cmd/server/router.go:707` |
| Preview handler loads the issue, expands issue identifiers, then calls `computeCommentAgentTriggers` | `server/internal/handler/comment.go:837-911` |
| Preview request accepts `content`, optional `parent_id`, and optional `editing_comment_id` | `server/internal/handler/comment.go:778-782` |
| Preview response returns agent `id`, `name`, optional `avatar_url`, `source`, and `reason` | `server/internal/handler/comment.go:784-793` |
| `editing_comment_id` is parsed as UUID input, scoped to the same workspace and issue, and used as `ExcludeTriggerCommentID` | `server/internal/handler/comment.go:855-872` |
| Preview validates or derives the parent context for an edit | `server/internal/handler/comment.go:874-897` |
| `CreateCommentRequest` accepts optional `suppress_agent_ids` | `server/internal/handler/comment.go:770-776` |
| `UpdateComment` accepts optional `suppress_agent_ids` | `server/internal/handler/comment.go:1509-1513` |
| Create-comment `suppress_agent_ids` is parsed as request-boundary UUID input | `server/internal/handler/comment.go:957-964` |
| Update-comment `suppress_agent_ids` is parsed as request-boundary UUID input | `server/internal/handler/comment.go:1523-1535` |
| Create and edit trigger paths compute the full trigger set, then apply `filterSuppressedCommentAgentTriggers` before enqueueing | `server/internal/handler/comment.go:1092-1122,1594` |
| Frontend API sends `editing_comment_id` for preview and `suppress_agent_ids` for update when present | `packages/core/api/client.ts:664-700` |
| Edit UI calls preview with `editingCommentId`, renders trigger chips, tracks suppressed agents, and submits suppressions on save | `packages/views/issues/components/comment-card.tsx:269-274,300-315,359-367,578-582,858-862` |
| Preview hook includes `editingCommentId` in its query key and sends it to the API | `packages/views/issues/hooks/use-comment-trigger-preview.ts:58-80` |
| Timeline edit mutation passes suppressed agent IDs through to the API layer | `packages/views/issues/hooks/use-issue-timeline.ts:299-302` |

## Edit-preview pending-task dedup

| Fact | Source |
| --- | --- |
| Default dedup query skips any queued or dispatched task for the issue and agent | `server/pkg/db/queries/agent.sql:544-548` |
| Edit-preview dedup query excludes only tasks whose `trigger_comment_id` equals the edited comment | `server/pkg/db/queries/agent.sql:550-558` |
| `hasPendingTaskForIssueAndAgent` selects the comment-scoped exclusion only when `ExcludeTriggerCommentID` is valid | `server/internal/handler/comment.go:1232-1244` |
| Agent-assignee on-comment dedup uses the shared helper | `server/internal/handler/issue.go:2576-2594` |
| Assigned squad leader on-comment dedup uses the shared helper | `server/internal/handler/comment.go:1197-1229` |
| Mentioned squad leader dedup uses the shared helper | `server/internal/handler/comment.go:1397-1435` |
| Direct agent mention dedup uses the shared helper | `server/internal/handler/comment.go:1440-1464` |
| Positive regression test covers all four edit-preview trigger sources | `server/internal/handler/comment_trigger_preview_test.go:179-265` |
| Negative regression test proves another comment's pending task still dedupes the preview | `server/internal/handler/comment_trigger_preview_test.go:267-290` |
| Edit-submit regression test proves `suppress_agent_ids` filters update-triggered tasks | `server/internal/handler/comment_trigger_preview_test.go:292-316` |

## Guards that make a valid mention a silent no-op

| Guard | Source |
| --- | --- |
| agent archived / no runtime → `continue` (`RuntimeID` invalid or `ArchivedAt` set) | `server/internal/handler/comment.go:1451-1452` |
| squad leader archived / no runtime → `continue` | `server/internal/handler/comment.go:1417-1423` |
| private agent the actor cannot access → `continue` (`canAccessPrivateAgent`) | `server/internal/handler/comment.go:1454-1458` |
| private squad leader the actor cannot trigger → `continue` (`canAccessPrivateAgent`) | `server/internal/handler/comment.go:1425-1428` |
| already-pending dedup (agent) → shared pending-task helper → `continue` | `server/internal/handler/comment.go:1459-1463` |
| already-pending dedup (squad leader) → shared pending-task helper → `continue` | `server/internal/handler/comment.go:1429-1433` |
| `canAccessPrivateAgent` definition | `server/internal/handler/agent_access.go` (search `func (h *Handler) canAccessPrivateAgent`) |
| `canEnqueueSquadLeader` (loads leader, delegates to `canAccessPrivateAgent`) | `server/internal/handler/agent_access.go:82-91` |

## @all broadcast and assignee-trigger suppression

| Fact | Source |
| --- | --- |
| `commentMentionsOthersButNotAssignee` — decides whether to suppress the assignee's on-comment trigger | `server/internal/handler/comment.go:1246-1288` |
| `@all` is treated as a broadcast → returns true → assignee auto-trigger suppressed | `server/internal/handler/comment.go:1257-1261` |
| Comment-flow computation that consults it | `server/internal/handler/comment.go:1175-1177` |
| `@all` never enqueues a specific agent: it is neither `squad` nor `agent`, so it is skipped in the mention trigger computation | `server/internal/handler/comment.go:1437-1439` |

## CLI id sources (where the UUID comes from)

| List command | Field used as mention id | Source |
| --- | --- | --- |
| `workspace member list` | `user_id` (NOT the membership-row id) | `server/cmd/multica/cmd_workspace.go:465` |
| `agent list` | `id` | `server/cmd/multica/cmd_agent.go:365` |
| `squad list` | `id` | `server/cmd/multica/cmd_squad.go:57` |
| Member mention uses `user_id`, confirmed by the backend roster formatter: `formatMention(user.Name, "member", userID)` where `userID = UUIDToString(m.MemberID)` | `server/internal/handler/squad_briefing.go:189-190` |
| `formatMention` emits `[@<name>](mention://<type>/<id>)` | `server/internal/handler/squad_briefing.go:216-218` |

## Explicit non-claim: no member-notification path in the Go comment handler

The skill deliberately does **not** assert that a `member` mention "sends a
notification." `server/internal/handler/comment.go` has no notification
delivery path for member (or issue) mentions: `computeMentionedAgentCommentTriggers`
branches only on `squad` and `agent`
(`server/internal/handler/comment.go:1397,1437-1439`), and a grep of the file for
`notif` returns only an unrelated comment about avoiding "log spam" on
unchanged threads — no member-notification call. The verified contract is
narrow: a `member` or `issue` mention renders as a link and enqueues no agent
run; only `agent` and `squad` mentions enqueue work. If a notification UX
exists, it is not in this handler, so this skill makes no claim about it.
