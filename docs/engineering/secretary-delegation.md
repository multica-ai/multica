# The Secretary as chief of staff: agents ask agents, the platform carries the conversation

Phase 5 of the digital company. The boss works mainly with one agent, the Secretary, in
chat. The Secretary does not read a database and fill a template. It **assigns work to the
squad leaders and floor chiefs, who run their own models to analyse their own part of the
company**, reads what they say with judgment, challenges what looks thin, and comes back to
the boss with one assessment and a recommendation for each open problem. The boss decides in
the same chat. The Secretary hands the decisions back to the leaders, who plan and run them.

This is the spec. The build order is in [secretary-delegation-plan.md](secretary-delegation-plan.md).
It builds on [digital-company.md](digital-company.md) and [issue-wakeups.md](issue-wakeups.md);
read "The Secretary" in the first. The list of what this changes there is in
[What changes in the existing spec](#what-changes-in-the-existing-spec).

## Design stance

An earlier draft of this spec made the platform do the thinking: a fixed report form, a
validator that rejected a report for a missing section, an evidence checker, tables for every
step. That makes reports rigid and shallow, and it wastes the thing the product has: agents
that can read context, weigh it and explain it. This version reverses the split.

| The agents do (model, skills, instructions) | The platform does (code) |
|---|---|
| Decide what the boss's question needs and who must answer | Say who the people are: the org map |
| Gather facts, assess completion, schedule, quality, cost, risk, dependencies, capacity | Carry the request to the right agent as an ordinary issue |
| Choose the shape and depth of every answer to fit the question | Wake the Secretary when the answers are in, or when time is up |
| Challenge a vague or unsupported answer, in their own words | Let the Secretary answer in the boss's chat |
| Judge what is worth the boss's attention | Prove that a directive came from the boss, and keep the quote |
| Break a decision into work and run it through the workflow | Apply budgets, company controls, privacy and the audit trail as for any issue |

**Domain knowledge lives in skills, not in code.** How to assess a project's completion, what
a risk entry contains, how to talk about confidence, how to spot a slipping schedule, how to
write for a boss with two minutes: these are skills the leaders and the Secretary carry. The
boss edits them on the Skills page. Changing how reports think does not need a release.

## Principles

1. **The boss talks to one agent.** The Secretary is the front desk. Leaders reach the boss
   only through the Secretary, and only when it is needed.
2. **Leaders run the model.** A leader asked to report on its squad reads its issues, runs,
   pull requests and documents, may ask members to look at something, and writes its own
   assessment. The Secretary does not pre-digest a leader's facts for it.
3. **Flexible by default.** No required headings, no rejected submissions. The skills
   suggest dimensions and the model picks the ones that fit the question.
4. **Judgment is checked by judgment, and by the audit trail.** The Secretary reads critically,
   can open any issue the leaders cite, and asks again in free text. Every request, answer and
   challenge is an issue and a comment the boss can open.
5. **Everything is an ordinary issue.** Wakeups, workflows, budgets, company controls,
   privacy and the activity timeline apply unchanged. Nothing runs in a side channel.
6. **The boss's word is the authority, and it is recorded.** Work is handed to a leader as a
   directive only because the boss said so in chat. This is a safety rule, not a format.
7. **Asynchronous.** The Secretary runs one thing at a time (Hermes memory is
   last-writer-wins, see "The agent" in the existing spec). A chat turn ends quickly; answers
   arrive in later runs and are spoken into the same chat.

## Roles

| Role | Does | Does not |
|---|---|---|
| **Boss** | Asks in chat, decides, talks to a leader only when the Secretary says it is needed or when they choose to. | Fill forms. |
| **Secretary** (Hermes) | Understands the ask; reads the org map and what it already knows; decides whom to ask and what to ask; delegates; reads the answers critically and challenges; analyses across squads (consistency, concentration of risk, dependencies between squads, trends against earlier reports); writes the assessment and the recommendations; records the boss's decisions; hands them to leaders; triages leaders' questions before they reach the boss. | Do a department's work, change statuses, decide proposals, or create work the boss did not ask for. |
| **Floor chief** | Rolls up its floor: asks its squad leaders, reads their answers, returns one floor-level assessment. Can be addressed by the Secretary like a leader. | Run the squads. |
| **Squad leader** | Analyses and reports on its squad when asked; plans and runs directives; raises what it cannot resolve to the Secretary. | Report unprompted outside its routines, or address the boss directly. |

The addressee is the Secretary's choice. A narrow question goes to a squad leader. A question
about a whole floor goes to the floor chief when the floor has one and to its leaders when it
does not. A company-wide question goes to the chiefs and to the leaders of squads outside any
chiefed floor. The Secretary may ask both a squad and a squad it depends on.

## The loop

```
Boss (chat)             Secretary                       Leader / chief                   Platform
   │ "report on X"         │ read org map, memory, KB      │                                 │
   ├──────────────────────>│ choose whom to ask, and how   │                                 │
   │                       │ create request issue + asks ──┼────────────────────────────────>│ issues, wakeups
   │<─ "I asked A, B, C ─┤ (turn ends)                   │<── asked ──────────────────────│
   │    about …, back by …"│                               │ run the model: read, assess,    │
   │                       │                               │ ask members, write the answer   │
   │                       │<── all answered, or time up ──┼────────────────────────────────┤ child_done / time wakeup
   │                       │ read critically, compare;     │                                 │
   │                       │ challenge ────────────────────>│ answer again                    │
   │<─ assessment ─────────┤ write assessment, say it ─────┼────────────────────────────────>│ chat reply, ad hoc brief
   │ "D1: B, D2: hold"     │                               │                                 │
   ├──────────────────────>│ confirm; issue directives ────>│ plan, decompose, run workflow   │ issue + boss quote
   │                       │<── leader needs the boss ─────┤ question to the Secretary       │
   │<─ only if needed ─────┤ answer it itself, or ask      │                                 │
```

### 1. The ask

The Secretary's chat turn does what a good chief of staff would do before leaving the room.

- It works out what the boss wants: a number, an assessment, a decision list, a comparison, an
  early warning. A question about a fact it can answer from the digest, the knowledge base or
  its own earlier briefs it answers at once, saying where the answer came from.
- If the scope is genuinely ambiguous (which project? which period?) it asks the boss one
  short question and stops. It does not guess and fan out.
- Otherwise it reads the org map, decides whom to ask, and **writes each ask in its own words,
  for that addressee**: the boss's question, why it matters, the period, what depth is useful,
  what the Secretary already knows (so the leader does not repeat it), and when it wants the
  answer. It creates a parent request issue and one child issue per addressee.
- It replies in chat: who it asked, about what, and when to expect the answer. It may say it is
  skipping a squad and why. It does not wait.

Examples of how the same Secretary adapts:

| Boss says | The Secretary asks | And will look for |
|---|---|---|
| "Summarise today's business report." | The leaders and chiefs behind the business results; a quiet squad it can see is idle it need not wake. Asks each for results, trend against the last report, and what they are worried about. | A one-page picture with the numbers that matter, the exceptions, and the two or three things to decide. |
| "How is project X doing?" | The squad that holds X, and a squad X depends on if the first mentions it. Asks for completion against the plan, schedule confidence, quality, blockers, and a view on risk. | A candid completion estimate with a reason, a ranked risk list with likelihood and impact, and what would change the date. |
| "Are we on track for the launch?" | The owning leaders for each stage and the Release leader, asked for gaps and the reasoning behind their confidence. | Contradictions between the answers, and an overall verdict with the evidence behind it. |
| "What went wrong last week?" | The leaders of squads with failures or slips, asked for causes and what they would change. | Patterns across squads, not a list of incidents. |

The Secretary decides these things by judgment from its instructions and skill. The platform
does not hold a table of question types.

### 2. The leader's answer

The leader is woken on its child issue like on any assignment. A short system note on issues
created by the Secretary says whom the answer is for and that, for this issue, **the leader
analyses directly or delegates the gathering to a member, and writes its own assessment**; it
lifts, for these issues only, the usual rule that a leader must not do the work itself.

The leader's skill, `squad-reporting`, shapes the answer; the question shapes how much of it
is used. It covers, as a menu and not a form:

- **Completion**: against the plan or the acceptance criteria, as a range with the reason, and
  what remains. Not "80%" with nothing behind it.
- **Schedule**: ahead, on track, slipping, and the leading indicators (stale work, review
  queues, retries, blocked time).
- **Quality**: failed runs, rework, review findings, tests, escaped defects when known.
- **Risk**: each risk with cause, likelihood, impact, owner, a proposed response, and whether
  it needs the boss. Separate observed from suspected.
- **Dependencies**: on other squads, people, providers; requests overdue.
- **Capacity and cost**: who is stretched, budget burn and projection when relevant.
- **Outcomes** for squads with business goals: the metric, the trend, the reading.
- **Confidence**: how sure the leader is, what it has not checked, what would raise it.
- **Recommendations**: concrete, with an owner, and what the leader will do without the boss.
- **Evidence habit**: cite the issue, run, document or comment a claim rests on, in plain
  links; say "I did not check" when that is the truth.

The leader posts the answer as a comment on its item issue, or as a document it links when it
is long, and closes the item. It decides the form. If it needs something from the boss it
**asks the Secretary**, not the boss (see [Questions go up](#questions-go-up)).

### 3. Reading the answers

The request settles when every child is closed (the platform's `child_done` rule wakes the
parent's assignee, the Secretary) or when the Secretary's deadline passes (a one-shot time
wakeup on the parent). The Secretary then does the part only a model can:

- Reads every answer critically. A claim that sounds too tidy, a "green" that sits beside a
  stale queue, two squads that contradict each other, a risk with no owner, a number without
  a source: it opens the issue the leader cites, or its comments, with the same read access it
  has everywhere.
- **Challenges in free text**: it comments on the child issue and the leader is woken (the
  issue is reopened by the comment as for any request to the squad). No fixed number of
  rounds; a runaway guard stops it after a generous limit (see [Guardrails](#guardrails)).
- Analyses **across** answers: where risk is concentrated, which squad is the bottleneck for
  others, whether progress claims add up to the date being promised, what changed against its
  earlier briefs (it reads them with `multica brief list`).
- Writes the assessment for this boss and this question. Where something could not be
  confirmed it says so in the text; where a leader did not answer in time it says that too and
  does not fill the gap with a guess.
- Sends it with `multica chat reply` into the boss's chat and publishes it as an ad hoc brief
  (the existing table, so it is stored, searchable and has decision buttons for proposals).

What the assessment contains is the Secretary's call. Its skill, `secretary-analysis`,
describes the repertoire: a headline the boss can read in ten seconds; the answer to the
question first; an overall rating of progress and of risk with the reason; the items that need
the boss, each with the situation, options, a recommendation and an owner, numbered so the boss
can answer "D1: B, D3: hold"; the risks ranked; the leaders' disagreements named; what was not
checked. A quiet question gets a short answer.

### 4. The hand-over

The boss answers in chat in ordinary language. The Secretary:

1. Restates what it understood for each decision and asks if it is wrong. A one-line
   confirmation, not a form.
2. Creates a **directive** for each: an issue for the addressee leader or chief, in the
   project the squad holds, carrying the **boss's own words** with the objective, the
   constraints the boss gave, the priority and date, the report it came from, and what the
   Secretary thinks "done" means. It writes it for that leader, as in the ask.
3. Tells the boss which leader has what.

The leader's skill, `directive-execution`, has it **acknowledge, plan and run**: say in a line
what it understood; decompose the objective into issues in the project, with owners and
stages; let the project's workflow (the delivery workflow, for example) carry each one through
its steps; keep the Secretary informed at milestones, in a comment, not a form; and raise a
blocker as soon as it is real. The usual coordinator rules for dispatch still apply.

Later reports and chat answers say how each directive is going: the Secretary reads the
directive issues and what the leaders posted.

### Questions go up

A leader that needs a decision, a missing fact or a change of scope it cannot get from its own
squad asks the **Secretary**, in a comment addressed to it on the directive or item issue. The
Secretary is woken as an ordinary run. It either **answers from what it knows** (the boss's
earlier words, the knowledge base, another leader's answer), or **asks another leader**, or
**brings it to the boss** in chat, with its recommendation, and relays the answer. The boss is
only asked what the Secretary cannot settle itself. If the boss chooses to open a leader's chat
or issue directly, nothing prevents it.

A decision that really is a proposal (a squad asking the boss for money, scope or an exception)
still uses the existing proposal flow and the Approve and Reject buttons.

## Skills

Shipped with the starter company, installed as workspace skills (editable, versioned like any
skill), and attached to the agents that need them. Each is a `SKILL.md` plus reference files.

| Skill | Attached to | Holds |
|---|---|---|
| `secretary-analysis` | Secretary | How to understand an ask and choose addressees; how to write an ask a leader can act on; economy (do not wake a squad whose answer you can already read); what to read critically and how to challenge; cross-squad analysis checklist; report craft for a busy reader; uncertainty language; how to number, confirm and record decisions; how to turn a decision into a directive; how to triage a leader's question before the boss sees it. |
| `squad-reporting` | Squad leaders, floor chiefs | The menu in step 2, with worked examples of a good and a weak answer for each dimension; how to gather cheaply (what to read first, when to ask a member); calibrated confidence; how a chief rolls up its leaders. |
| `directive-execution` | Squad leaders, floor chiefs | Acknowledge, plan, decompose, align with the project workflow, milestones, escalation to the Secretary, and how to close. |
| `business-metrics` (optional) | Squads with goals | How to read the company's goals and key results, and report outcome movement honestly. |

The boss tunes behaviour here: "always lead with decisions", "do not report on squad Y unless it
is red", "use our risk scale". A change to a skill takes effect on the next run.

## What the platform adds

Deliberately small. Each item exists because an agent cannot do it from the model side.

| # | Item | Why it is code | Notes |
|---|---|---|---|
| P1 | **`multica chat reply`** (Secretary only) | Today only a chat turn can write into a chat; `multica chat` reads. The Secretary must answer when a *later* run finishes. | Writes an assistant message into a session the run is linked to. See G1. |
| P2 | **Org map**: `GET /api/company/org-map`, `multica company map` | The Secretary must know the structure from data, not memory. The directory lacks leader, members, project and workflow. | Read-only, bounded, any member. |
| P3 | **Request linkage** | The Secretary's later run must know which chat to answer and which asks belong together. | Issue metadata on the parent request (`chat_session_id`, `asked_by`, `deadline`); children carry the parent and the existing `squad_request` origin. No new tables. |
| P4 | **Directive authority** | An agent must not create work on its own. | An issue created with `origin_type = directive` is accepted only from the Secretary, only inside a run triggered by a chat message from an owner or admin who is the run's originator, and stores that message's id and quote. |
| P5 | **Leader note** | Leaders are coordinators; for the Secretary's issues they must analyse. | A short system block on issues the Secretary created, in `handler/squad_briefing.go`; absent on any other issue. |
| P6 | **Question routing** | A leader's question must reach the Secretary. | A leader's comment mentioning the Secretary on a directive or item issue wakes it; existing mention path. Check only. |
| P7 | **Guardrails and visibility** | Cost and safety. | See below. |

What this spec **does not build** (and an earlier draft did): a report form, a section
validator, an evidence checker that rejects a report, report and item tables, a clarification
state machine, a fixed question-type router, digest-only routing rules.

Platform facts it reuses (verified in the code): `issue create --assignee-squad` with
`--parent`; the `squad_request` origin on that pair; `child_done`; one-shot time wakeups;
issue metadata; the brief table with kind `ad_hoc`; `originator_user_id` on every task and
`invokeOriginatorFromRequest`; the squad directory; `project.squad_id`; the `floor_chief`
template; skills; the proposal flow.

### G1. Answering from a run that is not a chat turn

Chat messages are written when a chat task is enqueued (user) or finalised (assistant).
`multica chat reply --session <id> --file <md>` adds a server-written assistant message from a
run that is *linked* to that session: the request issue's metadata names the session and the
run's originator is its owner. It marks the session unread, emits the usual chat event and
posts through a channel (Telegram, Slack, Lark, DingTalk, WeCom) when the session is bound to
one, if the outbound path can carry it (spike S2). It is refused for any other agent, any other
session, and an archived session (the Secretary then falls back to the inbox). Rate limited. The
Secretary decides when and what to say, including progress notes and questions.

### G2. Directives and the boss's word

P4 is the only place the platform insists on a form, and for security: the boss must be able to
trust that nothing was started that they did not ask for. The rule is that the quote is stored,
the leader sees it, and a run with no live boss message behind it cannot create the issue. The
content of the directive is still free text written by the Secretary.

## Guardrails

Generous limits that stop a runaway, not a style. Workspace settings with these defaults.

- At most 20 addressees per request and 8 open requests; depth of delegation two (Secretary to
  chief to leader).
- At most 8 challenge comments per child issue, then the Secretary must close the item with
  "unresolved" and say so.
- At most 5 directives per boss message.
- A request's deadline defaults to 30 minutes for a status question and 2 hours for analysis,
  chosen by the Secretary and shown to the boss.
- Company controls hold leader runs when paused or in quiet hours; the Secretary then says the
  answer is partial and why.
- Budgets and holds apply to every leader run as usual. The Secretary reports what a request
  cost (runs and tokens from the usage tables) at the foot of its assessment.
- A directive cannot approve a proposal or a stage gate, and cannot start a Launch (an
  irreversible outside effect). Those keep their own decision flow.

**Known limits, decided on review (2026-11-04):**

- *Who sent the boss's message.* `chat_message` has no author column. The directive check
  therefore requires the chat session's creator to be the run's originator. Chat sessions are
  listed and read by their creator (`ListChatSessionsByCreator`), so only that person can write
  to the session; the creator check is equivalent to the author check for every session the
  product creates today. If a shared session type is added, this check must be revisited.
- *The per-message cap is not locked.* The cap counts directives already filed from the message,
  then inserts. Two concurrent creates could both pass at four. The Secretary runs one task at a
  time (`max_concurrent_tasks = 1`), so the only way to race is two CLI processes inside one
  run. The cap is a runaway guard, not a security boundary; a transactional lock is the fix if
  that changes.

## Visibility

- **Chat** is where the boss lives. The Secretary's replies are markdown with links to issues.
- **Secretary desk**: requests live in a workspace project "Secretary desk", so the existing
  board, filters and issue pages show every request, child, challenge and directive with no new
  route. A small read-only list in the Company header (requests, directives, status, owner) is a
  later convenience.
- **Squad room panel** shows a squad's open items from the Secretary.
- No new copy beyond that list; all six locales when it lands.

## Authority

| The Secretary may | The Secretary may not |
|---|---|
| Read the whole workspace | Decide a proposal or a stage gate |
| Create request issues for leaders and chiefs and comment on them | Ask without a boss message, a routine or a digest flag behind it |
| Challenge a leader without a fixed limit up to the guardrail | Present an unconfirmed claim as fact |
| Compile, publish and speak assessments in the boss's chat | Speak in any chat that is not linked to the run |
| Create a directive from a validated boss chat message | Create a directive from a wakeup, an autopilot or its own idea |
| Triage leaders' questions and relay | Change an issue's status or assignee, or delete anything |
| | Do a department's work, or dictate how a leader splits it |

## What changes in the existing spec

| Existing text | From phase 5 |
|---|---|
| "Pull, not push" | Still true for the daily brief. An on-demand assessment starts from the boss's question and the Secretary delegates to the people who know. |
| "The Secretary may not create work unprompted"; "may create an issue when the boss asks in chat" | The second becomes the directive with the P4 check. |
| Inquiries: 3 per brief, comments | Kept for the daily brief. On-demand asks use request issues. |
| Leader briefing: coordinate, do not work | Unchanged, with the exception in P5 for the Secretary's issues. |
| Floor chief: publishes a floor brief | Also answers the Secretary and rolls up its leaders. |
| Secretary template | Revised; carries the `secretary-analysis` skill. |

## Out of scope

- The Secretary deciding proposals or stage gates for the boss.
- Mobile.
- Replacing the daily brief and its autopilot.
- A structured report schema. If a squad later wants one, it is a squad skill, not a platform rule.

## Testing and evaluation

Deterministic parts get ordinary tests: the org map joins and isolation; `chat reply` (linked
session only, Secretary only, archived fallback, unread, channel path); directive authority
(refused from a wakeup, an autopilot, a stale message, a non-admin originator, an archived
session; the quote is stored; leader sees it); `child_done` and the deadline wakeup on a
Secretary-assigned parent keep the originator; a private leader is reachable through the boss
originator; guardrail caps; the leader note present only on the Secretary's issues; company
pause yields a partial answer.

The agent behaviour is **not deterministic**, so quality is measured, not asserted:

- A **scenario set** with scripted company data (fixture issues, runs, a slipping project, a
  hidden dependency, a risk nobody owned). Leaders and the Secretary run on a real model.
- A **rubric** the boss can read: did it name the real problem; is completion justified; are
  risks ranked with owners; are claims traceable; did it skip a quiet squad sensibly; did it
  challenge the weak answer; is it short; is the language the boss's.
- Each skill change reruns the set, and the results are saved next to the skill version. A
  regression on the rubric blocks the change.
- Scripted leaders are used only to test plumbing; the runbook with Hermes is the acceptance.

## Decisions for the boss (accepted 2026-11-04, as recommended)

| # | Question | Recommendation |
|---|---|---|
| D1 | May a leader reach the boss directly? | **Only through the Secretary**, unless the boss opens the leader's chat or issue themself. |
| D2 | May the Secretary decide a proposal when the boss says "approve P-12" in chat? | **Not yet.** Keep buttons. Revisit after directives have run for a few weeks. |
| D3 | Does the Secretary skip a quiet squad? | **Yes, by judgment, and says who it skipped.** |
| D4 | Default deadlines | **30 min status, 2 h analysis**, set per request. |
| D5 | Floor chiefs as a hop | **Used whenever the floor has one** and the question is floor-wide. |
| D6 | The daily brief | **Unchanged for now.** Revisit once the on-demand flow is measured. |
| D7 | Document language | **English**, with the product copy translated per the conventions. |

## Open risks

| Risk | Mitigation |
|---|---|
| Free-form answers can be confident and wrong. | The Secretary reads critically and opens the cited issues; the skills require evidence links and "not checked"; the audit trail is open; the evaluation set tests for it. |
| Cost: every ask is a model run for each leader. | The Secretary is taught economy; guardrails; the cost line; the boss sees runs and tokens per request. |
| The Secretary cannot answer in chat from a run (G1). | Spikes S1 and S2 decide first. Fallback: inbox plus a banner in the chat. |
| Hermes concurrency 1: a long analysis delays the next chat turn. | The analysis is a bounded run; a chat turn queues behind it and says so; measure in S5. |
| Skills drift or are edited into harm. | Skills are versioned; the evaluation set runs on every change; the starter version can be restored. |
| Leaders ignore the note and only dispatch. | The note and the skill say what to do; the Secretary challenges a non-answer; the evaluation set includes it. |
