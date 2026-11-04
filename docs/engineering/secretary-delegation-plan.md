# Plan: the Secretary as chief of staff (phase 5, WS13)

Spec: [secretary-delegation.md](secretary-delegation.md). Same layout as
[digital-company-plan.md](digital-company-plan.md): numbered steps with scope, files and tests.
The value of this phase is in the agents' behaviour, which lives in skills and instructions, so
the plan builds the thin platform parts first and then spends most of its effort on the skills
and on measuring them.

Rules for every step (`AGENTS.md`): no foreign keys; any index in its own `CONCURRENTLY`
migration; `make sqlc` after SQL; UI-consumed JSON through zod and `parseWithFallback`; shared
views in `packages/views`, stores in `packages/core`; copy in six locales; tests beside the code;
report what ran and what was skipped.

## Order

```
13.0 Spikes ─┬─> 13.1 chat reply ──────────┐
             ├─> 13.2 org map ─────────────┤
             ├─> 13.3 request linkage ─────┼─> 13.6 Skills and templates ─> 13.8 Evaluation + acceptance + docs
             ├─> 13.4 directive authority ─┤          ▲
             └─> 13.5 leader note ─────────┘          │
                                         13.7 Guardrails, 13.9 Views  (parallel to 13.6)
```

13.1 to 13.5 are independent and small. 13.6 needs them in a branch to run end to end. The
evaluation set (13.8.1) is started as soon as 13.1 to 13.3 work, so skill drafts are measured
from the first version.

## WS13

### 13.0 Spikes (decide before building)

Results are written into the status table below. A failed spike changes only the step named in
"Fallback".

| # | Question | How | Pass | Fallback |
|---|---|---|---|---|
| S1 | Can a run that is not a chat turn append an assistant message to a chat session, shown live and unread in web and desktop? | A branch endpoint writing the message from a task token linked to the session. | Appears live; session unread. | None; the design needs it. Revisit the spec. |
| S2 | Does a session bound to Telegram, Slack, Lark, DingTalk or WeCom send such a message outward? | Read `integrations/channel/engine/session.go`; recorded adapter responses. | Delivered through the existing outbound path. | Web chat and inbox only in v1; channel path becomes 13.1b. |
| S3 | Does `child_done` fire for a parent assigned to the Secretary, and does that run keep `originator_user_id` = the boss? And does a one-shot time wakeup on the parent do the same? | Go test. | One run per settle; originator kept. | Enqueue the Secretary run server-side when the last child closes. |
| S4 | Can an agent run started from the boss's chat create issues assigned to a squad whose leader is a **private** agent? | Extend `squad_private_leader_test.go` with an agent actor and a live originator. | Allowed for an owner/admin originator; refused for a member. | Adjust the Secretary path only; do not widen private-leader access in general. |
| S5 | With real Hermes through the daemon: a chat turn that creates a request ends in under a minute, and the later wakeup run completes; how long does a real cross-squad analysis take? | Manual on the boss's machine. | Both complete; memory is intact. | Tighten the bound on the analysis run; keep concurrency 1. |
| S6 | Can company setup install workspace skills and attach them to the Secretary, floor chiefs and leaders, and does a skill reach a Hermes run? | Read `service/company_setup.go` and the skill attach path; run one skill on Hermes. | Skills installed and visible to the run. | Seed skills by a setup step in `company-setup-mutations`. |

### 13.1 `multica chat reply`

| # | Step | Files | Tests |
|---|---|---|---|
| 13.1.1 | Endpoint and CLI: add a server-written assistant message to a session. Allowed only for the Secretary agent, only in a run linked to the session through the request's metadata or a chat turn on it, only if the run's originator owns the session. Marks unread, emits the chat event. Refused for an archived session (the CLI says so; the Secretary falls back to the inbox). Rate limited. | `handler/chat_reply.go`, `service/chat_reply.go`, `cmd/multica/cmd_chat.go` | Linked session only; Secretary only; other agent refused; other session refused; archived fallback; unread; event; rate limit |
| 13.1.2 | Core: chat query cache patched or invalidated by the event; a message with an unknown kind renders as markdown. | `packages/core/chat/*`, `packages/views/chat/*` | Cache patch; unknown kind |
| 13.1.b | (If S2 passed) the message leaves through the bound channel. | `integrations/channel/*` | Recorded adapter responses |

> Built (13.1.1): the link is `task.chat_session_id` only; the request-issue path waits for 13.3. A reply carries no `task_id`, so it is not mistaken for the turn's outcome. The channel path (13.1.b) is not built. Cap: 10 assistant messages per session per minute.

### 13.2 Org map

| # | Step | Files | Tests |
|---|---|---|---|
| 13.2.1 | Query and service: floor, floor chief, squad, charter, leader and runtime status, members and skills, the squad's project and workflow, open work counts. Bounded. | `server/pkg/db/queries/org_map.sql`, `service/org_map.go` | Joins; workspace isolation; a squad with no project; an unseated squad |
| 13.2.2 | `GET /api/company/org-map` (any member), `multica company map [--floor] [--output]`. | `handler/org_map.go`, `cmd/multica/cmd_company_map.go`, router | Handler and CLI |
| 13.2.3 | Core type, zod schema, query hook (for the Desk list and the room panel). | `packages/core/company/org-map-*.ts` | Fallbacks |

> Built (13.2.1-13.2.2): open issues count issues assigned to the squad or in its project, excluding done and closed categories. Caps: 200 squads, 50 members per squad, with truncation flags. The web hook (13.2.3) is not built.

### 13.3 Request linkage

| # | Step | Files | Tests |
|---|---|---|---|
| 13.3.1 | Convention, documented and used by the template: the parent request issue (assigned to the Secretary, in a `todo` state) carries metadata `chat_session_id`, `asked_by`, `deadline`; children use `--assignee-squad`/`--assignee` with `--parent`, which gives `squad_request` for squads. The deadline is a one-shot time wakeup on the parent. | template; docs | Contract test with the CLI: parent, two children, wakeup, `child_done` |
| 13.3.2 | Setup creates the "Secretary desk" project and, for existing workspaces, repairs it. The Secretary's requests are filed there. | `service/company_setup.go`, `packages/core/company/setup-*.ts` | Setup creates it once; idempotent re-run |
| 13.3.3 | Check that a leader's comment mentioning the Secretary on an item or directive issue wakes it, and that the Secretary can comment on a leader's issue and wake the leader (the challenge). | Existing handlers | Tests documenting both paths; fix only if one fails |

### 13.4 Directive authority

| # | Step | Files | Tests |
|---|---|---|---|
| 13.4.1 | Allow `origin_type = directive` on issue creation. Accept it only from the Secretary, only inside a run triggered by a chat message in a live session written by an owner or admin who is the run's originator. Store the message id as `origin_id` and the quote in metadata; refuse otherwise with a clear message. Max 5 per boss message. | `handler/issue.go`, `service/directive_authority.go` | Refused from a wakeup, an autopilot, a stale message, an archived session, a non-admin originator; quote stored; cap |
| 13.4.2 | A directive cannot target a Launch stage gate or a proposal approval: refused with a pointer to the right flow. | `service/directive_authority.go` | Launch refusal; proposal refusal |
| 13.4.3 | Company controls and budgets apply to the leader's runs unchanged (held state shown on the issue). | existing | Regression test with a paused company |

### 13.5 The leader note

| # | Step | Files | Tests |
|---|---|---|---|
| 13.5.1 | A short system block on issues the Secretary created (parent `squad_request` from the Secretary, or `directive`): who the answer is for; analyse directly or delegate the gathering and write the assessment yourself; point to the `squad-reporting` or `directive-execution` skill when installed (never name a skill the agent lacks); ask the Secretary, not the boss. Absent on every other issue. | `handler/squad_briefing.go` | Text test; absent on an ordinary issue; present on each kind; skill not named when absent |

### 13.6 Skills and templates

The core of the phase. Draft, run on the evaluation set (13.8.1), revise.

| # | Step | Files | Tests |
|---|---|---|---|
| 13.6.1 | Skill `secretary-analysis`: understanding the ask, choosing addressees, writing an ask a leader can act on, economy, critical reading and challenge, cross-squad analysis, report craft, uncertainty language, numbering and confirming decisions, writing a directive, triaging a leader's question. With worked good/weak examples. | `packages/core/squads/skills/secretary-analysis/*` | Frontmatter and file-shape test; evaluation set |
| 13.6.2 | Skill `squad-reporting` (leaders and chiefs): the menu of dimensions with examples, cheap gathering, calibrated confidence, chief roll-up. | `.../squad-reporting/*` | Same |
| 13.6.3 | Skill `directive-execution`: acknowledge, plan, decompose, align with the project workflow, milestones, escalation, closing. | `.../directive-execution/*` | Same |
| 13.6.4 | Optional skill `business-metrics`, attached to squads with goals. | `.../business-metrics/*` | Same |
| 13.6.5 | `secretary` template v2: short; points to the skill; states authority, the chat rule (end quickly), memory (preferences only), language. `floor_chief` template: answers the Secretary and rolls up. | `packages/core/agents/instruction-templates.ts` | Template tests pin the rules, not the prose |
| 13.6.6 | Starter company and setup install the skills and attach them: Secretary, chiefs, and each leader. Existing workspaces get a repair action. A boss edit to a skill is never overwritten by a re-run. | `packages/core/squads/company-template.ts`, `service/company_setup.go` | Install once; attach; no overwrite of edits; idempotent |

### 13.7 Guardrails

| # | Step | Files | Tests |
|---|---|---|---|
| 13.7.1 | Workspace settings and enforcement: 20 addressees and 8 open requests, delegation depth two, 8 challenge comments per child, 5 directives per boss message, default deadlines. The Secretary sees the refusals as plain errors it can explain. | `service/request_guardrails.go`, settings | One test per cap; defaults; settings change |
| 13.7.2 | Cost line: the Secretary can read runs and tokens spent on a request's issues (`multica company usage --parent <issue>`). | `handler/usage*.go`, CLI | Arithmetic; isolation |
| 13.7.3 | Digest risk lines: a request past its deadline with no assessment; a directive blocked longer than its due date. | `service/digest.go` | Thresholds |

### 13.8 Evaluation, acceptance and documentation

| # | Step | Files | Tests |
|---|---|---|---|
| 13.8.1 | **Evaluation set**: fixture company data (a slipping project with a hidden dependency, a risk nobody owns, a quiet squad, a leader who claims "green" over a stale queue, a business squad with a metric trend) and the rubric from the spec. A runner that executes the Secretary and leaders on a real model and saves outputs and a rubric score beside the skill version. Used manually and before changing a skill. | `e2e/secretary-eval/*`, `docs/engineering/runbooks/phase5-eval.md` | The runner runs against scripted leaders in CI (plumbing only); real-model runs recorded in the runbook |
| 13.8.2 | Scripted API scenarios (plumbing): a request to three squads and a chief, `child_done` and the deadline wakeup, the Secretary's challenge, the chat reply, a directive that lands in a project on a delivery workflow, a leader question relayed, a paused company yielding a partial answer, a non-admin refused, a wakeup unable to create a directive. | `e2e/phase5-secretary.spec.ts` with `TestApiClient` | Playwright |
| 13.8.3 | Runbook for the boss's machine: Hermes through the daemon, three real questions of increasing breadth, one hand-over, one leader question relayed, Telegram if S2 passed. Unsigned until run. | `docs/engineering/runbooks/phase5.md` | Executed once and signed off |
| 13.8.4 | `apps/docs`: asking the Secretary, what it delegates and how, directives, the skills and how to tune them, the org map. English, then fr, ja, ko, zh. Update the existing spec's Authority and Inquiries per the spec's table. | `apps/docs/content/docs/*`, `docs/engineering/digital-company.md` | Link, frontmatter, navigation |
| 13.8.5 | Performance: org map for 50 squads; a 20-addressee request end to end under a stub clock. | `server/internal/service/*_perf_test.go` | Numbers recorded |

### 13.9 Views

| # | Step | Files | Tests |
|---|---|---|---|
| 13.9.1 | Chat renders the Secretary's replies; no new card. Links to issues are plain links. | existing | Regression |
| 13.9.2 | Secretary desk list in the Company header: requests and directives with status, owner and age, from the Secretary desk project. Read-only. The room panel lists a squad's open items. Wired in web and desktop. | `packages/views/company/components/secretary-desk-dialog.tsx`, header, panel, `apps/web`, `apps/desktop` | View tests; no `next/*` or router imports |
| 13.9.3 | Copy in en, fr, ja, ko, zh-Hans, zh-Hant per "UI descriptions" in `conventions.mdx`. | `packages/views/locales/*` | Locale parity |

## Risks carried from the spec

- S1 and S2 decide whether the Secretary can speak into the chat from a later run. Do them first.
- Quality depends on the skills. The evaluation set is the control; do not tune a skill without it.
- Hermes concurrency 1 and a long analysis: measure in S5 before allowing nested delegation.

## Spike results (2026-11-04)

Read-only investigation of the code. Handler tests were not run: the handler test fixture
fails against this checkout's dev database (`agent_owner_id_fkey` when it removes its user),
and the dev database holds real data, so no test was forced. The evidence is the code and the
names of existing tests; each "pass by reading" needs its test in the step named.

| # | Result | Evidence and consequence |
|---|---|---|
| S1 | **Buildable, nothing exists.** | Chat rows are written only on enqueue (user) and finalise (assistant) in `service/task.go`; `multica chat` only reads. A server-side write of an assistant row plus the existing chat events is a small addition (13.1.1). Which event the web client needs for a message with no task (`chat:message` or `chat:done`) is checked in 13.1.2. |
| S2 | **Not free; needs a deliberate delivery snapshot.** | Telegram and WeCom outbound only fire on `EventChatDone` carrying a task id, and resolve the destination from `channel_task_delivery` for that task. Their code states that a task without a snapshot "must not reach an external conversation, even if its Chat once had a Telegram route". `CopyChannelTaskDelivery(child, parent)` and `CreateChannelTaskDeliveryFromSession` exist (used for retries and chat enqueue). Design for 13.1.b: when a request is made from a channel-bound chat turn, copy that turn's delivery snapshot to the Secretary's later run and publish the reply as `EventChatDone` for that run. Web chat does not depend on this. |
| S3 | **Pass by reading.** | `child_done` is on by default (only an explicit `false` disables it) and wakes an agent assignee (`TestChildDoneWakesAgentAssignee`). The run is attributed through `attributionForIssueTask`, which falls back to the parent issue's provenance (creator, agent-create origin) and, last, to the agent's owner. For a Secretary-created parent that is the boss either way. Two things to handle: the default instruction ("move it forward") is generic, so the request must carry its own instruction (13.3.1); and the rule is per stage, so a request with children in one stage fires once when all close. A test for originator and for the one-shot deadline wakeup is added in 13.3.1. |
| S4 | **Likely pass, untested for this exact path.** | `TestCreateIssue_SquadPrivateLeader_OwnerAllowed`, `TestComment_SquadPrivateLeader_AgentActorAllowed` and `TestChildDone_SquadPrivateLeader_AgentActorWakesLeader` exist. Creating an issue as an agent actor with a live boss originator is not covered; added in 13.4.1. |
| S5 | **Open, manual.** | Needs Hermes through the daemon on the boss's machine (`multica daemon status` shows the daemon stopped here). |
| S6 | **Partly passes.** | A Hermes agent with skills bound gets a `HERMES_HOME` overlay holding them (`execenv.HermesHome`, `hermes_home.go`), so the runtime side works. Company setup installs no skills today (nothing in `service/company_setup.go` or `setup-*.ts`), so 13.6.6 is real work: create the skills with `multica skill create`/`files upsert` (or import) and attach them with `multica agent skills add`. |

Net effect on the plan: S1 and S3 to S4 do not change it; S2 turns 13.1.b from a maybe into a
defined step (snapshot copy, task-id event); S6 confirms 13.6.6; S5 remains the one thing only
the boss's machine can answer.

## Status

| Area | Status | Notes |
|---|---|---|
| Spec | Rewritten around agents doing the analysis; D1 to D7 accepted | [secretary-delegation.md](secretary-delegation.md); the boss accepted the recommended choices. |
| 13.0 Spikes | Done except S5 | See the table above. |
| 13.1 to 13.9 | Open | Nothing built. |
