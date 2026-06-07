# Mentioning — source map

Every claim in `SKILL.md` traces to a line below. Re-derive against the current
tree before trusting any line number; the behavior is the contract, the line is
a pointer. Branch where verified: `feat/builtin-skills`.

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
| `enqueueMentionedAgentTasks` is the dispatch loop over parsed mentions | `server/internal/handler/comment.go:1082` |
| It is called for every comment via `triggerTasksForComment` | `server/internal/handler/comment.go:944` |
| `squad` branch: resolve squad in workspace, read `LeaderID`, enqueue the leader | `server/internal/handler/comment.go:1089-1128` |
| `squad` → `EnqueueTaskForSquadLeader` | `server/internal/handler/comment.go:1128` |
| Everything not `agent` after the squad branch is skipped: `if m.Type != "agent" { continue }` | `server/internal/handler/comment.go:1133` |
| `agent` branch: load agent in workspace, then enqueue | `server/internal/handler/comment.go:1143-1167` |
| `agent` → `EnqueueTaskForMention` (a run for that agent) | `server/internal/handler/comment.go:1165` |
| **`member` and `issue` mentions reach neither branch — they enqueue NOTHING.** A `member` mention fails the `!= "agent"` skip at line 1133 (the squad branch above it only matches `squad`); an `issue` mention does the same. | `server/internal/handler/comment.go:1089,1133` |

## Guards that make a valid mention a silent no-op

| Guard | Source |
| --- | --- |
| agent archived / no runtime → `continue` (`RuntimeID` invalid or `ArchivedAt` set) | `server/internal/handler/comment.go:1147` |
| squad leader archived / no runtime → `continue` | `server/internal/handler/comment.go:1113` |
| private agent the actor cannot access → `continue` (`canAccessPrivateAgent`) | `server/internal/handler/comment.go:1152` |
| private squad leader the actor cannot trigger → `continue` (`canAccessPrivateAgent` via squad gate) | `server/internal/handler/comment.go:1117` |
| already-pending dedup (agent) → `HasPendingTaskForIssueAndAgent` → `continue` | `server/internal/handler/comment.go:1156-1162` |
| already-pending dedup (squad leader) → `continue` | `server/internal/handler/comment.go:1121-1127` |
| `canAccessPrivateAgent` definition | `server/internal/handler/agent_access.go` (search `func (h *Handler) canAccessPrivateAgent`) |
| `canEnqueueSquadLeader` (loads leader, delegates to `canAccessPrivateAgent`) | `server/internal/handler/agent_access.go:82-91` |

## @all broadcast and assignee-trigger suppression

| Fact | Source |
| --- | --- |
| `commentMentionsOthersButNotAssignee` — decides whether to suppress the assignee's on-comment trigger | `server/internal/handler/comment.go:953` |
| `@all` is treated as a broadcast → returns true → assignee auto-trigger suppressed | `server/internal/handler/comment.go:967-969` |
| Comment-flow call site that consults it | `server/internal/handler/comment.go:933` |
| `@all` never enqueues a specific agent: it is neither `squad` nor `agent`, so it is skipped in the enqueue loop | `server/internal/handler/comment.go:1133` |

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
delivery path for member (or issue) mentions: `enqueueMentionedAgentTasks`
branches only on `squad` and `agent`
(`server/internal/handler/comment.go:1089,1133`), and a grep of the file for
`notif` returns only an unrelated comment about avoiding "log spam" on
unchanged threads — no member-notification call. The verified contract is
narrow: a `member` or `issue` mention renders as a link and enqueues no agent
run; only `agent` and `squad` mentions enqueue work. If a notification UX
exists, it is not in this handler, so this skill makes no claim about it.
