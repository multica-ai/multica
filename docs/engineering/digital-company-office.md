# Digital company office: from squads on a floor to a governed digital company

Status: phase 1 and the first slices of phases 2 and 3 are implemented and merged
(PRs #25 and #26). Phases 2 (completion), 3 and 4 are specified below and not yet
built. [Roadmap](#roadmap-and-requirements-trace) maps each part of this document to
the boss's original requirements.

The [Decisions](#decisions) section records how the open questions of the first draft
were settled, and why. Where a section below changed because of one, it says so.

## Goal

Make the Office the place where the workspace reads as a company: floors hold
squads, squads sit in rooms, agents sit at desks, and the person running the
workspace (the boss) sees what each squad is doing and what is waiting on them.

This spec covers four slices that ship together, because the first alone only
redraws the product:

1. **Seat the squads.** The office is organised by floors and squads instead of
   four fixed per-agent rooms.
2. **One proactive loop.** A squad does recurring work on its own and brings the
   boss a structured proposal. The boss approves or rejects it from the office,
   and an approval hands work to another squad.
3. **A Secretary.** One agent, running on the Hermes engine, is the boss's single
   point of contact. Departments do not report to the boss; the Secretary reads
   what they did, writes one daily brief (work, blockers, recommended
   resolutions, decisions needed), and answers the boss's questions in chat. The
   boss goes to a department's agents only to drill into a specific item. See
   [The Secretary](#the-secretary).
4. **A knowledge base.** The company's durable record (policies, research,
   product documents, decisions, past briefs) lives in WeKnora, run as a sidecar
   behind a Multica-owned boundary. Agents read it through MCP, write to it
   through a governed draft-and-publish path, and the boss searches and
   curates it from a Knowledge page. See [The knowledge base](#the-knowledge-base).

The first four slices are phase 1 and the start of phases 2 and 3. The rest of the
target (a standing charter for every squad, a company pipeline from opportunity to
launch, goals, budgets, autonomy levels, performance reviews and scale) is specified
in [Phase 2: proactive operations](#phase-2-proactive-operations),
[Phase 3: the company operating system](#phase-3-the-company-operating-system) and
[Phase 4: governance](#phase-4-governance). [Out of scope](#out-of-scope) lists what is
still deliberately excluded.

The product line stays the one in `VISION.md`: people set direction and remain
accountable, agents keep work moving. Agents propose; only the boss's approval
starts new work.

## Roadmap and requirements trace

The boss asked for a digital company run by AI agents: many agents and squads that
research the market, find opportunities, propose products, analyse and plan them,
build them and deliver them to market; a boss who orchestrates and decides; agents that
are proactive, not only obedient; one Secretary (Hermes) that consolidates everything
into a daily report; and a company knowledge base that is a real digital asset.

| # | Requirement (from the boss) | Delivered by | Status |
|---|---|---|---|
| R1 | A digital office with many agents and squads | Phase 1: floors, rooms, desks, home squads | Built |
| R2 | A layout that holds many more squads later | Phase 1: floors of 6 rooms x 10 desks; phase 4: floor chiefs and scoped briefs | Built / phase 4 |
| R3 | The whole value chain: research, opportunity, idea, analysis, plan, build, launch | Phase 3: pipeline, initiatives, starter company | Phase 3 |
| R4 | The boss orchestrates and works with agents | Phase 1 panels and chat; phase 2 Decisions and briefs; phase 4 controls | Built / phase 4 |
| R5 | Proactive agents: report, research, propose without being asked | Phase 1 first loop; phase 2 charters, routines, operating reviews, adaptive attention | Partly built / phase 2 |
| R6 | One Hermes Secretary: daily consolidated report, boss decides, drill-down only on demand | Phase 1 Secretary and briefs; phase 2 delivery to chat apps, feedback, catch-up | Built / phase 2 |
| R7 | A company knowledge base as a digital asset (not a home-made RAG) | Phase 1 WeKnora integration; phase 3 taxonomy, librarian, wiki, artifacts | Built / phase 3 |
| R8 | The boss stays accountable; nothing runs away | Phase 2 attention limits; phase 4 goals, budgets, autonomy levels, pause, audit | Partly built / phase 4 |

### Acceptance: a company day

The finished system passes these six scenarios end to end (the plan's workstream 12
turns them into automated and manual checks):

1. **Morning.** At 08:00 the Secretary publishes a brief that the boss reads in under
   two minutes, in the Office and, if enabled, in a chat app. It names what was
   delivered, what is blocked and why, what is recommended, the goals' state, budget
   burn, and the decisions waiting.
2. **Discovery.** Over the week the Research floor's routines scan markets and
   competitors, write published-after-review reports into the knowledge base, and file
   at most a few opportunity proposals. The boss approves one from the brief.
3. **Venture.** Approval creates an initiative at the first pipeline stage. Squads on
   the Product, Build and Launch floors pick it up in order, each stage producing its
   required documents in the knowledge base, with a boss gate only where the pipeline
   says so.
4. **Ask.** The boss asks the Secretary "what do we know about X and what did we
   decide?" and gets an answer that cites knowledge-base documents and decisions.
5. **Control.** A squad nears its budget; the brief warns the boss; at the limit new
   non-essential runs hold with a visible reason until the boss extends or the period
   rolls. One click pauses the whole company.
6. **Improvement.** A weekly review scores squads and agents, a retrospective turns a
   failure into a knowledge document and a proposed instruction change, and the boss
   approves or rejects the change with a diff in front of them.

## What exists today

Verified against the code before this work started (`feat/office-agent-panel`). The
implementation has since replaced several of these facts; see
[Implementation deviations](#implementation-deviations).

- `/office` is a workspace-scoped page. `OfficePage` runs `IssueSurface` with
  `scope={{ type: "workspace" }}` and draws the board as the pixtuoid office.
  The project is an optional filter, not the scope.
- An agent is drawn only while it is assigned an issue in the filtered list
  (`OfficeScene` builds `agentIds` from the issues).
- The floor has four desk rooms in a 2x2 grid, four desks each. That is too few
  for the squads the product already ships: the `sdlc_greenfield` template has 9
  roles and `sdlc_maintenance` has 10 (`packages/core/squads/templates.ts`), and a
  squad has no member cap. Four desks seat less than half of one of them. The rooms are
  the `OFFICE_ROOMS` enum in `packages/core/agents/office-room.ts`, mirrored in
  `agentOfficeRooms` in `server/internal/handler/agent.go`, and stored per agent
  in `agent.office_room` (migration 600).
- The page controls seating completely. `WorkerSession` is
  `{ key, working, room }`, where `room` is an index 0 to 3. The engine takes
  sessions and picks the desk. Room names are drawn by the page from
  `roomNames`. The one thing the page cannot change is how many rooms and desks the engine
  lays out: that is fixed in the vendored Rust patch, and this spec changes it
  (see [Engine](#engine)).
- Squads are a leader, members (`squad_member`, agents or humans) and
  instructions. They do not appear on the office. A squad only runs when an
  issue is assigned to it or a comment wakes its leader.
- An autopilot can already be assigned to a squad
  (`autopilot.assignee_type = 'squad'`): it creates the issue assigned to the
  squad and the leader is enqueued through the normal squad chain.
- Agents can already create issues and assign them to members; the member gets
  an `issue_assigned` inbox item, which is already `action_required` severity.
  Proposals therefore need no inbox type of their own; the brief adds one.

## Model

```
Workspace (the company)
└─ Floor            named, ordered group of 6 squad rooms (3 x 2)
   └─ Squad room    one squad, in slot 0..5 of a floor
      └─ Desk       one of 10 desks (5 x 2) for an agent whose home squad this is
```

- **Floor.** A named group of six room slots. Floors are tabs on the office.
  Names are the boss's ("Research", "Product", "Build", "Launch").
- **Seat.** A squad holds at most one seat: `(floor_id, slot)`. A squad with no
  seat is *unseated* and is not drawn.
- **Home squad.** An agent is drawn at a desk of one squad only, its home
  squad. An agent can still belong to other squads (docs allow it); it is not
  drawn there. An agent with no home squad is *unassigned* and is not drawn.
- **Desks.** A room has ten desks, enough for both built-in SDLC squads. A squad
  with more than ten agent members seats the first ten, the leader first and the
  rest in the order members were added. The room shows a `+N` chip for the
  remainder, which opens the squad panel. Human members are never drawn. Empty
  desks stay empty; a small squad does not rearrange the room.
- **Proposal.** A structured request from a squad for a decision by the boss,
  backed by an ordinary issue so comments and history work unchanged. It waits
  quietly in a queue and reaches the boss through the Secretary's brief, not
  through its own notification.
- **Secretary.** One agent per workspace, named by a workspace setting. It sits
  in the Executive Office squad (it leads it) on the Executive floor.
- **Brief.** The Secretary's written report to the boss for a period.

## Data model

All of this follows the repo rules: no foreign keys, every index
`CONCURRENTLY` in its own single-statement file, new tables get their primary
key through the 327-334 pattern, UUID relationships validated in handlers. The
numbers below are placeholders; use the next free numbers.

### Floors and seats

```sql
CREATE TABLE office_floor (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE squad ADD COLUMN floor_id UUID;
ALTER TABLE squad ADD COLUMN office_slot SMALLINT
    CHECK (office_slot IS NULL OR office_slot BETWEEN 0 AND 5);

-- one live squad per seat
CREATE UNIQUE INDEX CONCURRENTLY ... ON squad (floor_id, office_slot)
    WHERE archived_at IS NULL AND floor_id IS NOT NULL;

ALTER TABLE agent ADD COLUMN home_squad_id UUID;
```

Rules enforced in the handler and service, not by constraints:

- `floor_id` and `office_slot` are both set or both null.
- `home_squad_id` must be a squad the agent is a member of. Removing the agent
  from that squad, or archiving the squad, clears it. Adding an agent with no
  home to its first squad sets it.
- Deleting a floor requires it to be empty of live squads (their seats are
  cleared first by the caller in one request, never cascaded).
- At most 16 floors per workspace.

`agent.office_room` is replaced by the above. The column and `OFFICE_ROOMS` were
added in `fe7e59a59` and `9eb52ffe0`, which are not in an upstream release, so
the change removes them outright (migration drops the column; `agent.go`,
generated code, core types, `use-update-agent-office-room.ts` and
`office-room-menu.tsx` go with it). The old values (`product`, `research`,
`planning`, `general`) were room labels with no link to a squad, so there is
nothing to carry over. See [Decisions](#decisions), 1.

No backfill. Existing squads start unseated; see the unseated tray described under Views.

### Proposals

```sql
CREATE TABLE proposal (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    squad_id UUID NOT NULL,
    issue_id UUID NOT NULL,            -- the backing issue
    handoff_squad_id UUID,             -- squad that starts the work on approval
    urgent BOOLEAN NOT NULL DEFAULT false,
    urgent_reason TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'approved', 'rejected')),
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('agent', 'member')),
    created_by_id UUID NOT NULL,
    decided_by UUID,
    decided_at TIMESTAMPTZ,
    decision_note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- indexes, each its own file, CONCURRENTLY:
--   (workspace_id, state, created_at DESC)
--   (squad_id, state)
--   unique (issue_id)

ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN (... existing values ..., 'proposal'));
```

A proposal's issue is stamped `origin_type='proposal'`, `origin_id=<proposal.id>`.
The issue is **unassigned**, starts in the `in_review` category, and carries the
proposal body as its description. Unassigned means no inbox item and no run: the
proposal waits for the next brief. An **urgent** proposal (the proposing leader
must give a reason, and the cap below counts it separately) is assigned to the
boss, the oldest workspace owner, which raises the existing `issue_assigned`
inbox item (already `action_required` severity) at once. The state machine is
`pending -> approved | rejected`. "Ask for changes" is a rejection with a note
(see [Decide](#decide)); there is no `revising` state in this phase.

The proposal body is markdown with a fixed shape, rendered by the CLI so agents
cannot skip sections: **Summary**, **Evidence** (at least one link), 
**Recommendation**, **Options considered**, **Cost and risk**, **Ask**. Nothing
else is stored structurally; promote fields to columns only when something
queries them.

## API

All JSON read by the UI passes through zod + `parseWithFallback`
(`packages/core/api/schema.ts`), with defaults for optional fields and a
fallback for unknown `state` values, and malformed-response tests per endpoint.

Floors and seats (owner/admin):

| Endpoint | Purpose |
|---|---|
| `GET /api/office/floors` | Floors in `position` order. |
| `POST /api/office/floors`, `PUT/DELETE /api/office/floors/{id}` | Create, rename or reorder, delete (empty floors only). |
| `PUT /api/office/seats` | Body `[{ squad_id, floor_id \| null, slot \| null }]`, applied in one transaction. This is how a swap works: both squads in one request. A conflict returns 409 with the squad that holds the seat. |

Squads and agents:

- `POST /api/squads` seats the new squad in the lowest free slot, creating
  `Floor N` when every floor is full; a squad created by a template gets the
  template's floor name instead. A template that names a floor is placed there
  by the client: it creates the floor if missing, then moves the new squad to
  that floor's first free slot with `PUT /api/office/seats`. The response carries `floor_id` and
  `office_slot`. Squad create/update/list responses add both fields.
- `PUT /api/agents/{id}` accepts `home_squad_id`. Agent responses add it.
- Seat changes by someone who is not an admin require being the squad's creator,
  matching existing squad management (`memberCanWireAgent` family of checks).

Proposals:

| Endpoint | Purpose |
|---|---|
| `POST /api/proposals` | Create a proposal and its issue in one transaction. The actor must be a member of `squad_id` (an agent in a task, or a member). Body may carry `urgent` with `urgent_reason`. |
| `GET /api/proposals?state=&squad_id=` | List, newest first. |
| `GET /api/proposals/{id}` | One proposal with its issue id. |
| `POST /api/proposals/{id}/decision` | `{ decision: "approve" \| "reject", note }`. Owner or admin **members** only: an agent actor, the Secretary included, is refused. |

Handlers follow the UUID rules: `parseUUIDOrBadRequest` for request ids, workspace
filtering on every query, and the proposal's squad and handoff squad validated
to be live squads of the workspace.

### Attention limits

The boss's attention is the scarce resource, so the server enforces it rather
than trusting prompts:

- at most **3 pending** proposals per squad;
- at most **3 created per squad per rolling 7 days**;
- at most **10 pending** across the workspace;
- at most **1 urgent per squad per rolling 7 days**.

The workspace cap is what keeps the queue bounded as floors fill: with up to six
squads per floor, per-squad limits alone would let the boss's queue grow with
the number of squads. Exceeding any limit returns 409 with code `proposal_limit`
and a message the agent can act on ("decide the pending ones first"). The three
numbers are constants in one place in the service; they become workspace
settings only if real use shows they need tuning.

### Decide

In one transaction:

- **Approve:** `state='approved'`, `decided_*` set, the backing issue moves to
  the `done` category. If `handoff_squad_id` is set, create a project named after
  the proposal title and a kickoff issue in it, assigned to the handoff squad
  and linking back to the proposal issue. The kickoff goes through the same
  issue-creation service the autopilot uses, so the existing squad listeners wake
  the handoff leader; no new routing code.
- **Reject:** `state='rejected'`, issue to the `cancelled` category, and the note
  is posted as a comment that mentions the proposing squad's leader, which wakes
  the leader through the existing mention path. The note is how the boss asks for
  changes; the squad may file a new proposal that cites the old one.

Events are published after commit; the UI invalidates the proposal and issue
queries on them. No server payload is copied into Zustand.

## Core (`packages/core`)

New `packages/core/office/`: types, zod schemas, query options and mutations for
floors, seats and proposals. Query keys include `wsId`. Floors and seats use
optimistic updates only for a single predictable change with trivial rollback;
creates, deletes and decisions await the server.

`packages/core/agents/office-room.ts` is removed. Seat layout logic (which
agents sit in which room slot of a floor, who is overflow, who is unassigned)
is a pure function in core, `layoutFloor(floor, squads, agents)`, with its own
test matrix. Views only wire it.

## Views (`packages/views/office`)

`OfficeScene` is rebuilt around the current floor:

- **Sessions.** `layoutFloor` returns the sessions for the current floor:
  `{ key: agentId, working, room: slot }`. Seating no longer depends on the issue
  list. The issue list only feeds tag text.
- **Room names.** `roomNames` becomes the six squad names of the floor; an
  empty slot is labelled as empty and offers "Create squad".
- **Tags.** One function chooses the text, first match wins: `Needs you` (the
  agent is the leader of a squad with a pending proposal), the status of its
  focus issue (squad-assigned issues count for the leader), else `Idle`.
- **Floor tabs.** A tab strip with each floor's name and, per floor, a dot when a
  proposal is pending. Double-click a tab to rename it in place; a menu offers
  reorder and delete. A trailing `+` adds a floor. The last floor viewed is
  remembered per workspace as a client preference. A workspace with no seated
  squad shows a first-run card instead of an empty floor: pick the Research
  squad template, or create a squad.
- **Squad panel.** Clicking a room name opens `OfficeSquadPanel`: description
  (the squad's mission), members with their state, the squad's autopilots,
  pending proposals, and `+N` overflow. It sits beside `OfficeAgentPanel`; the
  agent panel loses its room tab and gains "Home squad".
- **Room badge.** A count over each room with pending proposals, placed from the
  `rooms` rectangles the engine already reports to overlays.
- **Decisions dialog.** Opened from a room badge or a header button, same pattern
  as `OfficeBoardDialog`. Lists pending proposals; each shows the rendered body,
  the proposing squad, the handoff squad, and Approve / Reject with a note field.
  Approve with a handoff says what it will start.
- **Unseated tray.** Squads with no seat and agents with no home squad, as chips
  in a collapsible strip. A menu on each seats the squad (floor and slot) or
  assigns the agent's home squad. "Seat all" places every unseated squad in
  order. For an owner or admin, a workspace with unseated squads shows a one-line
  banner with that same action, so existing squads reach the floor in one click and
  nothing is written until the boss asks.

Shared code stays in `packages/views`; no `next/*` or router imports. Copy is
added to all six locales (`en`, `fr`, `ja`, `ko`, `zh-Hans`, `zh-Hant`) under
`office.*` in `issues.json`, following the UI-descriptions rule: titles and
actions only, no restating help text.

`office.worker.ts` and the protocol change only in constants (room count, the
minimum floor size). New art (a boss office, a meeting room, a library) is a later
patch to the vendored engine, with art taken from the pixtuoid pack.

## Engine

The vendored pixtuoid patch (`patch/pixtuoid-web-issues.patch`) fixes the studio at
4 rooms of 4 desks in a 2x2 grid. It becomes **6 rooms of 10 desks**: a 3 x 2 grid
of rooms, each with a 5 x 2 pod of desks.

- The constants are `OFFICE_ROOMS`, `OFFICE_ROOM_DESKS`, `OFFICE_DESKS`,
  `STUDIO_DESKS` and the `STUDIO_ROOM_*` / `STUDIO_POD_*` sizes. `OFFICE_DESKS`
  becomes 61 (6 x 10 plus the one extra desk the existing assertion keeps).
- `studio_rooms` lays the grid out 3 across and 2 down; rooms are numbered west to
  east, top row first, which is the slot order.
- The page's minimum floor grows from 256 x 171 units to whatever the 3 x 2 grid
  needs (`MIN_UNITS_W/H` in `office-protocol.ts` and `STUDIO_MIN_W/H` in the
  patch change together). `unitsFor` already grows the width past the minimum on
  wide pages, so the cost is a slightly smaller figure on narrow ones.
- `issue_start` already takes a room index; the page's `OFFICE_ROOMS` array in
  core is the only other place the count appears.
- Acceptance, corrected after measurement: a worker figure is 12 layout units tall
  (`CHARACTER_SPRITE_H`), not 24. At 1280 x 720 the 6 x 10 floor is 416 x 234 units,
  about 3.1 px per unit, so a figure is about 37 px. The 64 px target would need a
  floor of about 240 x 135 units, which the 3 x 2 grid cannot fit; the old 2 x 2 floor
  gave about 50 px, so the target was never met. The 5 x 2 pod meets the 2.7 px per
  unit floor and is kept. Tags and room names must still be legible, and every
  worker must reach a desk (checked by the Rust layout test).
- The wasm is rebuilt by the steps in `lib/pixtuoid/README.md`; the README and the
  patch are updated in the same commit as the rebuilt binary. This needs
  `rustup target add wasm32-unknown-unknown`.
- If the 5 x 2 pod cannot meet the acceptance size, the fallback is a 4 x 2 pod
  (8 desks per room) with the same grid; the rest of the spec only reads the
  constant.

## Agent side

### Proposal command

`multica proposal create` renders the markdown body from flags and calls
`POST /api/proposals`:

```
multica proposal create --squad <id> --title "..." \
  --summary "..." --evidence <url> [--evidence <url> ...] \
  --recommendation "..." --option "..." [--option "..."] \
  --cost-risk "..." --ask "..." [--handoff-squad <id>]
```

It fails client-side when a required flag is missing, and prints the server's
`proposal_limit` message verbatim.

### Leader briefing

The squad leader's run already gets an Operating Protocol, a Roster and the
squad instructions. Add a fifth system-managed block, **Proposing to the boss**,
included only for squads that have an active autopilot: how to use the command,
the limits, that proposals queue for the Secretary's brief and `--urgent` is for
a decision that cannot wait a day, "no proposal is a valid result when nothing
clears the bar", and
that a no-op is recorded with the existing `multica squad activity ... --reason`
(see `squad_no_action.go`). The exact text lives beside the existing protocol in
`server/internal/daemon/execenv/runtime_config_sections.go` with a test like its
neighbours.

## The first loop

Ships as a squad template, `market_research`, in `packages/core/squads/templates.ts`
(today's templates are the two SDLC ones), created by the existing quickstart.

- **Floor.** Named `Research`.
- **Roles.** Research lead (leader), market scout, competitor analyst, and a
  critic whose job is to challenge evidence before the leader files anything.
  New agent instruction templates for each, in the same style as the current
  ones.
- **Routine.** The quickstart also creates one autopilot assigned to the squad,
  weekly on Monday morning in the workspace timezone, issue-creating, with a
  title template ("Market scan, week of ...") and a description that states the
  scan scope and the bar for a proposal. This uses the autopilot as it is.
- **Seat.** The quickstart creates the squad through `POST /api/squads`, so it
  is seated automatically on the `Research` floor.

End to end: Monday the autopilot opens an issue assigned to the squad. The
leader is woken, delegates scouting and competitor analysis by mention, has the
critic review, then files zero to three proposals, which queue without pinging
anyone. The room badge and the leader's `Needs you` tag appear on the Research
floor. Tuesday's 08:00 brief lists them under "Needs your decision" with the
Secretary's recommendation, and the boss gets one inbox item for the whole brief.
The boss approves one from the brief with a handoff to the Product Discovery
squad, and that squad's leader is woken on a new project.

## The Secretary

### Why this design

The boss does not want a report per department; that is the same flood of
status the product exists to remove. So the reporting direction is **pull, not
push**: departments do their work in issues, projects and proposals as they do
today, and one agent reads it all and tells the boss what matters. This also
keeps the departments cheap, because no squad spends runs writing a status
nobody asked for.

### The agent

- **Provider: `hermes`.** Hermes is already a built-in Multica runtime (see
  `providers.mdx` and `server/pkg/agent/hermes.go`; the daemon speaks ACP to it,
  passes MCP servers over the ACP session, and gives each Hermes agent a persistent
  memory store). Nothing in the daemon needs to change; the boss installs Hermes on
  the machine that runs the Multica daemon, and the runtime appears like any
  other (`MULTICA_HERMES_PATH` if it is not on `PATH`).
- **Why Hermes suits this role.** It is the one provider here with agent-curated
  long-term memory and searchable past sessions, which is what a secretary needs:
  it learns what the boss wants in a brief, which projects matter to them, what
  to skip, and can answer "what did we decide about X last month". It can also
  write its own skills, so its briefs can improve with use.
- **What we do not use from Hermes.** Its own cron scheduler and messaging gateway.
  The daemon builds a per-run `HERMES_HOME` and stops the run's gateway, so
  nothing of Hermes' would survive between runs anyway. **Scheduling is a Multica
  autopilot; delivery is Multica** (the brief, the inbox, and chat channels). This
  keeps everything the boss relies on visible, editable and pausable in the
  product.
- **Constraints that follow from Hermes' memory model** (`CLI_AND_DAEMON.md`,
  "Hermes agent memory"):
  - Memory is per agent and **local to one runtime**. The Secretary is bound to
    one runtime, the boss's own machine, and is not given a second one.
  - Concurrent runs are last-writer-wins on memory, so the agent has
    `max_concurrent_tasks = 1`. A brief and a chat then queue instead of racing.
  - Memory can be lost (reclaimed after 90 idle days, or by moving machines).
    **The platform is the source of truth, never memory.** Memory holds the boss's
    preferences and standing instructions; every fact about the company comes
    from the digest and the issues at the time of the run.
- **Visibility: private.** The Secretary can read the whole workspace, so only the
  boss may run it. Autopilots run it under the existing private-agent admission
  rules.
- **Squad: yes, as leader of an `Executive Office` squad** on a floor named
  `Executive`. The boss is a human member. This costs nothing new: the Secretary
  gets a seat on the floor through the normal home-squad rule, and the boss
  can assign or `@`-mention the squad with a request that does not belong to any
  department, and the Secretary routes it to the right squad with the usual
  delegation protocol. The **daily brief autopilot targets the agent, not the
  squad**, because the squad protocol is a dispatch-and-stop frame and the brief
  is a read-and-write task.
- **Naming.** The default name is `Secretary`, editable. Not the built-in `mika`.

The Secretary is identified by a workspace setting, `secretary_agent_id` (a
column, or the existing settings field if the workspace table has one),
validated to be a live agent of the workspace. Publishing a brief requires being
that agent or an admin member.

### The digest: facts without an LLM

`GET /api/office/digest?since=<timestamp>` and `multica brief digest --since 24h
--json` return the facts, computed by SQL, bounded in size:

| Level | Content |
|---|---|
| Workspace | Runs and cost since `since` (from the runtime usage tables), runtimes offline, agents with failed runs, unseated squads. |
| Per squad (ordered by what needs attention first) | Issues done since, issues in progress, blocked (the `blocked` status category), stale (in progress with no activity for 3 days), failed runs, leader no-action count, pending proposals (ids and titles). Up to 5 most recent completed issues as mention links. |
| Quiet squads | A squad with nothing new collapses to one line in a single `quiet` list. |

Caps keep it small regardless of company size: at most 20 squads in full, the
rest ranked and cut with a count. The Secretary never scans the raw board. It
opens at most a few issues to understand the items the digest flags.

### The brief

```sql
CREATE TABLE brief (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('daily', 'weekly', 'ad_hoc')),
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    body TEXT NOT NULL CHECK (char_length(body) <= 20000),
    proposal_ids UUID[] NOT NULL DEFAULT '{}',
    created_by_agent_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- indexes, each its own file, CONCURRENTLY: (workspace_id, created_at DESC)
```

Why a table, not an issue per day: a daily issue is 365 rows a year that need a
status they do not have, and it cannot carry decision buttons. Why not only a chat
message: it is not durable, searchable or addressable, and it cannot ping.

The body is markdown with these sections, in this order, validated by
`multica brief publish` (a required heading missing is an error the agent can
fix):

1. **Headline**: at most three lines; on a quiet day, "Nothing needs you today."
2. **Needs your decision**: each item has the situation, the options, the
   Secretary's recommendation and a link; pending proposals plus any blocker only
   the boss can clear. At most five; the rest are counted.
3. **Progress**: by floor, only squads with news; quiet squads are one line.
4. **Blockers and risks**: each with a proposed way out.
5. **Watch**: market and product notes from the Research floor since the last brief.
6. **FYI**: cost, runtime or failure anomalies.

Phase 3 extends this list (see the order in 3.7): `Pipeline`, `Goals`, `Budget`,
`People` are added as optional sections, and `Watch` becomes optional, because a company
with no market notes should not pad one. The validator in `multica brief publish` is
updated in plan step 10.5.

Target: readable in two minutes. A quiet day is a short brief, not a padded one.
`proposal_ids` lists the proposals the brief raises, so the app can render them
with Approve and Reject beneath the text.

`POST /api/briefs` (the Secretary or an admin member), `GET /api/briefs`,
`GET /api/briefs/{id}`. Publishing creates one inbox item of a new type,
`brief_ready` (severity `attention`), for each owner. This is the boss's only
routine notification; it follows the existing notification-preference
mechanism.

### Inquiries

When the digest flags something it cannot explain, the Secretary may ask a squad
leader by commenting on the flagged issue with the leader's mention, which wakes
the leader through the existing path. At most three per brief; the reply is
read in the next run or in chat. This is the only time a department spends effort
on the Secretary's behalf.

### Authority

| The Secretary may | The Secretary may not |
|---|---|
| Read the whole workspace through the CLI | Decide a proposal (the endpoint refuses an agent) |
| Publish briefs | Change an issue's status or assignee, or delete anything |
| Comment on an issue to ask a leader (3 per brief) | Create work unprompted |
| Create an issue for a squad when the boss asks in chat | Contact anything outside the workspace |

Decisions stay the boss's: in chat the Secretary answers "approve it" with a
link to the brief or the Decisions dialog. Letting it decide on the boss's
behalf would need the run's originator (`invokeOriginatorFromRequest`) to prove
the boss asked; that is a later change, only if the originator is reliable.

### When it runs

| Trigger | How |
|---|---|
| Every day 08:00 in the boss's timezone | An autopilot assigned to the Secretary agent, `run_only`, with an editable description (scope, language, tone). |
| Friday 17:00 | A weekly-review autopilot, created paused. |
| On demand | The boss asks in chat, or presses **Generate now** (a manual autopilot run) on the brief card. |
| Urgent | An urgent proposal pings the boss by itself; nothing else does. |

If the machine was asleep at 08:00 the run is skipped by the autopilot's existing
policy; the brief card then says "No brief today" with **Generate now**, so a
missed brief is one click, not a mystery.

### Where the boss sees and asks

- **Brief card** in the Office: a header button with an unread dot opens
  `OfficeBriefDialog`: the latest brief rendered, its proposals with Approve and
  Reject, and an **Ask** button on each item that opens the Secretary's chat with
  the item prefilled. Earlier briefs are a list in the same dialog.
- **Chat.** The Secretary is pinned in Chat (`chat_pinned_agent`), and clicking its
  desk opens the existing agent panel on the chat tab. Its instructions say to call
  `multica brief digest` before answering any status question rather than recall.
- **Drill down.** Squads and issues in the brief are links: a squad name opens
  its room and panel, an issue opens its thread. This is the only place the boss
  meets a department's agents unless they go looking.
- **Phone.** A Telegram, Slack or Lark bot bound to the Secretary (existing Chat
  integrations) lets the boss ask from anywhere. Pushing the daily brief into
  that chat needs outbound delivery from an autopilot run, which this spec
  does not assume; verify it before promising it (see Open risks).

### Setup

A first-run flow, "Set up your company", on the empty Office:

1. Check for an online `hermes` runtime. If none, show the install link
   (`install-agent-runtime`) and the daemon step, and wait.
2. Create the Secretary: provider `hermes`, that runtime, private, concurrency 1,
   from a new `secretary` agent instruction template in
   `packages/core/agents/instruction-templates.ts`.
3. Create the `Executive` floor and the `Executive Office` squad, the Secretary
   leading, the boss as a member; set `secretary_agent_id`; pin the Secretary in
   Chat.
4. Create the daily autopilot and the paused weekly one.
5. Offer the `market_research` squad as the next step.

The `secretary` template states: the role and the pull model; the digest as the
only source of company facts; the brief structure; decision-first and concise;
write in the language of the boss's last message, falling back to the language in
the autopilot's description; the inquiry cap and the authority table; what goes
in memory and what never does.

## The knowledge base

### Decision

Adopt **WeKnora** (Tencent, MIT, Go, v0.9.0 at the time of writing) as the
knowledge engine, deployed as a sidecar, with Multica owning the boundary. Do not
write our own RAG, and do not fork WeKnora.

The rule that keeps the two systems from overlapping: **WeKnora stores and
retrieves; Multica's agents think and act.** WeKnora ships its own agents, skills,
sandbox, automations, IM channels, user logins and per-user memory. None of those
are used. The company has one agent system (Multica's, with Hermes for the
Secretary) and one identity system (Multica's).

### What WeKnora covers

| The company needs to... | WeKnora capability (v0.9.0 docs and API) | Multica glue |
|---|---|---|
| Store anything | Parse PDF, Word, Excel, images, web pages, Markdown, whole folder trees; FAQ libraries; manual Markdown with `draft` / `publish` status | Upload, URL import and "Save to knowledge" |
| Preserve it | Chunk edit with revision history and rollback, Wiki page revisions, per-KB activity feed, audit log | Provenance links back to the issue, brief or proposal it came from |
| Find it | Hybrid retrieval (BM25 and vectors in one ParadeDB Postgres), rerank, tags, parent/child chunks, `knowledge-search` | Search box, MCP read tools, automatic context |
| Reason over it | Wiki pages generated from documents with source citations, optional knowledge graph (Neo4j) | Agents read Wiki pages through MCP |
| Keep it current | Scheduled data-source sync: Notion, Confluence, GitLab, Feishu, Yuque, DingTalk, Seafile, Nextcloud, RSS | Configured in WeKnora; surfaced in the Knowledge page |
| Share it safely | Workspaces (tenants) with roles; API keys scoped by capability and KB list; MCP endpoints with token, KB list, tool list and rate limit | One tenant per Multica workspace; endpoints per access level |

### Architecture

```
Multica server --- knowledge client (Go) ---> WeKnora: app, docreader, postgres, redis   (internal network)
   |  provisions a tenant per workspace; owns the Knowledge API and its schemas
   |  stores the KB registry and provenance links in its own database
Agents (any provider, Hermes included) --- MCP, read-only ---> WeKnora /mcp/<endpoint>
Agents --- `multica knowledge ...` CLI ---> Multica API ---> WeKnora (writes land as drafts)
Browser --- Multica API only ---> never talks to WeKnora directly
```

Principles:

1. **Multica owns the contract.** The Knowledge API the app and CLI use has Multica's
   own shapes and zod schemas; WeKnora's responses are adapted behind a `knowledge`
   interface in Go. WeKnora moved from 0.8.3 to 0.9.0 in weeks, so the version is
   pinned (never `latest`) and the adapter has contract tests against recorded
   WeKnora responses. Swapping the engine later means a new adapter, not new screens.
2. **One WeKnora tenant per Multica workspace.** Isolation between companies is
   WeKnora's tenant boundary. A workspace without knowledge enabled works exactly as
   today.
3. **Optional and degradable.** If WeKnora is down, runs proceed without it, the
   Knowledge page says "offline", and nothing in the core product fails.
4. **No secret reaches a browser.** Keys and endpoint tokens live server-side.

### Data model (Multica database)

Same migration rules as above (no foreign keys, concurrent indexes in their own files).

```sql
-- one row per workspace that enabled knowledge
CREATE TABLE workspace_knowledge (
    workspace_id UUID NOT NULL,
    weknora_tenant_id TEXT NOT NULL,
    api_key_ciphertext BYTEA NOT NULL,      -- the workspace's full-access key
    state TEXT NOT NULL DEFAULT 'ready' CHECK (state IN ('provisioning', 'ready', 'failed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Multica's view of each knowledge base
CREATE TABLE knowledge_base (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    weknora_kb_id TEXT NOT NULL,
    key TEXT NOT NULL,                       -- 'company', 'market', ...
    name TEXT NOT NULL,
    visibility TEXT NOT NULL DEFAULT 'workspace' CHECK (visibility IN ('workspace', 'restricted')),
    position INTEGER NOT NULL DEFAULT 0,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- where a document came from, and whether it is live
CREATE TABLE knowledge_link (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    knowledge_base_id UUID NOT NULL,
    weknora_knowledge_id TEXT NOT NULL,
    source_type TEXT NOT NULL CHECK (source_type IN
        ('issue', 'comment', 'proposal', 'brief', 'project', 'upload', 'url', 'sync')),
    source_id UUID,
    idempotency_key TEXT NOT NULL,           -- e.g. 'proposal:<id>:decision'
    state TEXT NOT NULL CHECK (state IN ('draft', 'published', 'failed')),
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('agent', 'member', 'system')),
    created_by_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);
-- unique (workspace_id, idempotency_key); (knowledge_base_id, state); (source_type, source_id)
```

`knowledge_link` is what makes writes idempotent (a retried decision log does not
duplicate), lets the app show "from issue MUL-123" on a document, and lists drafts
without asking WeKnora.

### Default knowledge bases

Created when the owner enables knowledge, from a template, editable afterwards:

| Key | Holds | Written by |
|---|---|---|
| `company` | Charter, policies, brand voice, org and roles, glossary, standing rules. Wiki on. | The boss, by hand |
| `market` | Market research, competitor profiles, customer voice, reading lists | Research floor, as drafts |
| `product` | PRDs, specs, roadmap, analyses, user research | Product floor, as drafts |
| `engineering` | Architecture decisions, runbooks, post-mortems; GitLab sync optional | Build floor, as drafts |
| `decisions` | Every proposal decision with the note, and the archive of briefs | Multica itself, published |

The embedding model is chosen once, before any KB exists, because WeKnora fixes a
KB's vector store at creation. Default: **`bge-m3`** through a local Ollama
(multilingual, so Vietnamese, English and Chinese work together), or any
OpenAI-compatible embedding endpoint. An LLM is needed only for document
summaries and Wiki generation; Wiki generation is off by default and switched on
per KB (`company`, `decisions`) at the `focused` setting to bound cost.

### Writing: drafts, then publish

A knowledge base is shared and long-lived, so one wrong "fact" is retrieved by
every agent for as long as it stays. The main risk is pollution, not scale.

- **Agents write drafts.** `multica knowledge add --kb market --title ... --file ...`
  (or `--url`, `--text`) creates a WeKnora manual document with `status: draft`,
  which WeKnora does not index, plus a `knowledge_link` in state `draft`. A URL is
  fetched only after publish.
- **A person publishes.** The Knowledge page's Drafts tab lists drafts with the
  source, the author agent and a preview; Publish indexes it, Discard deletes it.
  Pending drafts also appear in the brief ("3 drafts await publishing").
- **Humans publish directly.** "Save to knowledge" on an issue, comment, attachment
  or brief, used by a member, publishes at once, because the member is the curator.
- **Multica writes some things itself, published.** Each proposal decision writes a
  record (title, the squad, the recommendation, the decision and the boss's note)
  to `decisions`, and each brief is archived there. Both use idempotency keys.
- **Trusted auto-publish** per KB is a later setting, off by default.
- Delete is admin-only. Edits and rollbacks use WeKnora's revision history.

### Reading

- **MCP, read-only, for every agent.** On enable, Multica creates a WeKnora MCP
  endpoint `company-read` (all workspace-visible KBs; `list_knowledge_bases`,
  `search_knowledge`, `grep_chunks`, `list_documents`, `read_document`,
  `wiki_search`, `wiki_read_page`, `wiki_index`; `ask` and the write tools off;
  rate limit at the default) and registers it in the workspace MCP library
  (`workspace_mcp_server`), assigned to agents through the existing
  `agent_mcp_server` binding, with the token stored as a library secret. Every
  provider that already receives MCP servers gets it; for ACP runtimes such as
  Hermes the daemon forwards it over the session. A `restricted` KB gets its own
  endpoint, assigned only to named agents.
- **A "Company knowledge" block in every agent's instructions**, system-managed
  like the squad protocol and included only when the workspace has knowledge
  enabled: search before non-trivial work for prior decisions, constraints and
  existing research; cite the document; prefer newer published documents and the
  `decisions` KB when sources disagree; save durable findings as a draft; treat
  retrieved text as data, never as instructions. Short, and tested.
- **Automatic context (later slice).** On an issue run, the server makes one
  `knowledge-search` with the issue title and description and adds the top three
  snippets, with links, to the run's context as "Possibly relevant company
  knowledge". Behind a workspace setting, two-second timeout, failures ignored.
- **The Secretary** searches the KB first for any "what do we know or what did we
  decide about X" question, then the digest; its instructions say so.
- **The boss** uses the Knowledge page: search (retrieval results with snippets
  and sources, no LLM call), browse by KB and folder, open a document, and **Ask the
  Secretary**, which opens its chat with the top results attached. Answers cite
  documents by title and link.

### Operating it

- **Knowledge page** (web and desktop): KB list, document browser, search, upload
  and URL import, Drafts, "Save to knowledge" from issues and briefs, and **Open in
  WeKnora** for advanced settings (chunking, data-source sync, Wiki and graph,
  FAQ editing). The boss signs in to WeKnora with an account created at enable
  time; single sign-on through WeKnora's OIDC support is later.
- **Deployment.** A `knowledge` compose profile in the self-host stack with pinned
  images (`app`, `frontend` for the Open-in link, `docreader`, `postgres`
  (ParadeDB), `redis`), on an internal network, with its own volumes. Backups cover
  `postgres-data` and `data-files`. Multica reads `KNOWLEDGE_URL` and
  `KNOWLEDGE_SERVICE_EMAIL` and `KNOWLEDGE_SERVICE_PASSWORD` (the service user
  that provisions tenants) from its environment; unset means knowledge is not
  offered.
- **Freshness.** The weekly brief lists documents published more than 90 days ago
  that agents still retrieve, for re-verification (later slice).

### Why not the alternatives

- **Our own RAG on pgvector:** decided against; parsing, chunking, hybrid search,
  reranking, Wiki and sync are most of the work, and WeKnora already has them.
- **Using WeKnora as the agent platform too:** two agent systems, two identity
  systems, two sets of permissions. Rejected; see the rule above.
- **Other engines** (RAGFlow, Dify, AnythingLLM, Onyx and similar) were not
  benchmarked here. WeKnora was chosen on the evidence below and on your request;
  because of principle 1, the choice is reversible.

### Hands-on evaluation

WeKnora v0.9.0 was installed from the official repository with Docker Compose on
the development machine (core profile only: `app`, `frontend`, `docreader`,
`postgres` (ParadeDB), `redis`) with a local Ollama serving `bge-m3`. Nothing was
tried with an LLM configured, so the LLM-dependent features are not evaluated.

**Verified**

| Area | Result |
|---|---|
| Footprint | Five containers idle at about 650 MB of RAM in total. Images are about 1 GB of the pulled set; `bge-m3` is 1.2 GB. Light enough to run beside Multica. |
| Ingestion | Five files (Vietnamese Markdown, English Markdown, two longer docs) parsed, chunked and embedded in about 20 seconds. |
| Retrieval | `knowledge-search` answered in 64 to 330 ms. A Vietnamese question found the right Vietnamese document; a Vietnamese question about an English competitor profile found the English document first; an English question found a Vietnamese decision record. Multilingual retrieval works with `bge-m3`. |
| Keyword | An exact phrase ("4 giờ") ranked its document first at 0.74; hybrid mode combines BM25 and vectors. |
| Draft and publish | A manual document created with `status: draft` is not searchable; switching it to `publish` made it the top hit (0.805) about 12 seconds later. The draft-then-publish write path is real. |
| MCP | The endpoint speaks MCP over HTTP (`initialize`, `tools/list`, `tools/call`), authenticates with a bearer token (401 on a wrong one), exposes only the tools it was created with (`ask` was refused as "not found" when excluded) and rate-limits at 60 calls a minute by default. |
| Tenancy | A user can create a tenant (workspace) and mint a full-access API key for it; with that key alone, models and knowledge bases are managed. A key from one tenant searching another tenant's knowledge base returned nothing. |
| Scoped keys | A key with only `retrieve` on one KB could search that KB (200), was refused on another KB (403, "scope does not allow one or more knowledge bases"), and refused ingestion and KB creation (403). Default-deny holds. |

**Things the trial changed in this spec**

- **Provisioning uses a service account, not a platform key.** The call that mints
  a workspace API key (`POST /tenants/:id/api-keys`) accepts only an owner's login
  token, not an API key. So Multica holds one WeKnora service user (credentials
  in its environment), logs in as it, creates the tenant, mints the workspace's
  full-access key, and uses that key from then on. In the response, `api_key`
  is an encrypted form and `token` is the usable value. Self-serve tenant creation
  must be allowed for the service user (or a system administrator, bootstrapped
  with `WEKNORA_BOOTSTRAP_SYSTEM_ADMIN_EMAIL`, creates the tenants).
- **Embedding hosts must be allow-listed.** WeKnora refuses model URLs given as a
  bare IP (its SSRF guard). A local Ollama needs `SSRF_WHITELIST_EXTRA` to include
  its address, or a hostname. Setting that variable replaces the compose default
  list, so keep the defaults you need.
- **Retrieval always returns something.** A query for a token that exists nowhere
  still returned unrelated chunks at 0.41 to 0.42. Automatic context must apply a
  minimum score (start at 0.5) and the instruction block must tell agents to judge
  relevance and not treat a hit as an answer.
- **Ollama must be reachable from the containers.** It was bound to the Docker
  bridge address only, not to all interfaces, so it is not exposed to the network.
- **Ports.** WeKnora defaults to 80 and 8080; 8080 is Multica's. The trial ran the
  UI on 8085 and the API on 8090.

**Not verified** (needs an LLM key, real files or more time)

- Document summaries, Wiki generation, auto-tagging, the `ask` tool and reranking.
- PDF, Word, Excel and image parsing; URL import; data-source sync.
- Behaviour on a large corpus and the quality of long-document chunking.
- The WeKnora web UI beyond confirming it is served.

These are checks for step K2.2 and K4 of the plan, not reasons to doubt the choice.

## Implementation deviations

Where the code on `main` differs from phase 1 text above, the code is right and the
text is kept for the reasoning. Phases 2 to 4 below are written against these facts.

- The leader's proposal briefing lives in `server/internal/handler/squad_briefing.go`,
  not in `execenv`. The Company-knowledge block is added at claim time in the daemon
  handler so older daemons receive it too.
- A proposal's kickoff issue is created after the decision transaction commits (the
  issue service owns its transaction); a failure reverts the proposal to pending.
- `POST /api/office/setup` takes the Secretary's instructions from the client
  (`secretary_instructions`); the server holds no copy. Timezone order: request, the
  caller's saved timezone, UTC. Refusals are typed (`runtime_required`,
  `secretary_name_taken`, `floor_full`, `floor_limit`).
- A template that names a floor (Market Research) is seated by the client: it creates
  the floor if missing, then moves the new squad with `PUT /api/office/seats`.
  `floor_name` on squad create applies only when every floor is full.
- `brief.created_by_agent_id` is nullable beside `created_by_member_id`, so an admin
  can publish by hand. `secretary_agent_id` is a workspace column, not a settings key.
- The digest's knowledge section reads Multica's `knowledge_link` registry; documents
  added directly in WeKnora are not counted.
- Automatic knowledge context has no setting; it is on whenever knowledge is ready.
- The Knowledge page has no file upload. Documents enter through the CLI, issue and
  brief saves, URL drafts and the WeKnora UI.
- The Brief button sits in the floor tabs; the setup card stays for owners until the
  Market Research squad exists.
- `e2e/plugin-surface-document.spec.ts` was removed: it tested the pre-MUL-6485 srcdoc
  surface, and its absence was aborting the whole Playwright run.
- Migrations 595 to 599 contain shipped duplicate prefixes; the lint test exempts those
  exact names. New duplicates still fail.
- Knowledge needs `MULTICA_KNOWLEDGE_SECRET_KEY` and `KNOWLEDGE_PUBLIC_URL` beside the
  documented variables, and WeKnora's tenant cap per user is lifted by the profile.
- A worker figure is 12 layout units tall; the engine acceptance is about 37 px at
  1280 x 720.

## Phase 2: proactive operations

Phase 1 gave squads a proposal channel and the boss one Secretary. It did not give each
squad a standing job. Today a squad that nobody triggers does nothing, and nothing
notices. Phase 2 makes proactivity a property of the squad: every squad has a charter,
the charter's routines run, the platform measures whether they delivered, and the
attention the squad is allowed to spend on the boss adapts to how useful it has been.

Migrations for this phase use numbers 700 to 729. All migration rules above apply (no
foreign keys, concurrent indexes in their own files, primary keys by the 327-334
pattern). New JSON read by the UI goes through zod and `parseWithFallback`.

### 2.1 The squad charter

A charter is the squad's standing job description: why it exists, what it owes the
company and how often, what it may not do, and when to escalate. It replaces the
informal use of `squad.description` and `squad.instructions` for this purpose; the
description stays a one-line label and the instructions stay the leader's routing
rules.

```sql
CREATE TABLE squad_charter (
    squad_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    mission TEXT NOT NULL DEFAULT '' CHECK (char_length(mission) <= 2000),
    responsibilities JSONB NOT NULL DEFAULT '[]',  -- [{key, text}]
    outputs JSONB NOT NULL DEFAULT '[]',           -- [{key, kind, kb_key, cadence_hint}]
    boundaries TEXT NOT NULL DEFAULT '' CHECK (char_length(boundaries) <= 2000),
    escalation TEXT NOT NULL DEFAULT '' CHECK (char_length(escalation) <= 2000),
    intake TEXT NOT NULL DEFAULT '' CHECK (char_length(intake) <= 2000),  -- what other squads may ask of this one
    response_hours INTEGER NOT NULL DEFAULT 48 CHECK (response_hours BETWEEN 1 AND 720),
    revision BIGINT NOT NULL DEFAULT 1,
    updated_by_type TEXT NOT NULL DEFAULT 'member',
    updated_by_id UUID,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);   -- primary key on squad_id via the concurrent-index pattern

CREATE TABLE squad_routine (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    squad_id UUID NOT NULL,
    autopilot_id UUID NOT NULL,                    -- the schedule; unique per routine
    purpose TEXT NOT NULL CHECK (char_length(purpose) BETWEEN 1 AND 200),
    expected_output TEXT NOT NULL DEFAULT 'none'
        CHECK (expected_output IN ('none', 'issue', 'proposal', 'kb_document', 'report')),
    required BOOLEAN NOT NULL DEFAULT true,
    grace_hours INTEGER NOT NULL DEFAULT 24 CHECK (grace_hours BETWEEN 1 AND 336),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);   -- unique (autopilot_id); index (squad_id)
```

A routine is an ordinary autopilot (schedule, description, run history) plus this
link. No second scheduler is added.

API (flat registrations, as elsewhere):

| Endpoint | Purpose |
|---|---|
| `GET /api/squads/{id}/charter`, `PUT /api/squads/{id}/charter` | Read, replace. Admin or the squad's creator. `revision` increments; `If-Match` style conflict on a stale revision returns 409. |
| `GET /api/squads/{id}/routines` | Routines with state, last run, next run, last output (see 2.2). |
| `POST /api/squads/{id}/routines` | Link an existing autopilot, or create one from a cadence preset (`daily`, `weekdays`, `weekly`, `biweekly`, `monthly`, or a cron string) with a description. |
| `DELETE /api/squads/{id}/routines/{routine_id}` | Unlink; optionally pause the autopilot. |

**Briefing.** A squad that has a charter gets a system-managed block, "Your charter",
in the leader's instructions, rendered from the charter (mission, responsibilities,
outputs with cadence, boundaries, escalation) followed by the standing rules: search the
company knowledge before starting, file proposals instead of acting on money,
outside-world effects or org changes, record `no action` with a reason instead of padding,
and respect the attention limits. The block is tested like its neighbours.

**UI.** The squad panel gains a Charter tab (read for members, edit for admin and the
creator) and a Routines list with state chips, Run now, Pause and Add routine.

### 2.2 Routine compliance

The platform, not the squad, decides whether a routine delivered. For each routine the
server derives a state from `autopilot_run`, the squad's records and the output kind:

| State | Meaning |
|---|---|
| `on_time` | A run completed in the last expected window and produced the expected output, or recorded a `no action` with a reason. |
| `no_output` | A run completed, nothing was produced and no `no action` was recorded. |
| `late` | The next run is overdue by more than `grace_hours`. |
| `failed` | The latest run failed or was skipped for a reason other than a policy hold. |
| `paused` | The autopilot is paused (a hold from phase 4 shows as `held`). |
| `never_ran` | Created, and the first scheduled time has not come. |

Expected output is read from existing records: `proposal` (a proposal by the squad
since the run started), `kb_document` (a `knowledge_link` by one of its agents),
`issue` (an issue created by the autopilot run), `report` (a comment or document the
run linked). The digest gains, per squad with a charter, a `charter` object with the
routine states, and the workspace section counts the non-`on_time` required routines.
The brief's "Blockers and risks" lists them with a proposed fix (the Secretary may ask
the leader, see 2.5). A squad with a charter and no activity for more than seven days is
reported as `silent`.

### 2.3 The operating review

Charters make squads accountable for what they were told to do. The operating review is
how they find what nobody told them. Saving a charter creates (once) a weekly routine,
"Operating review", assigned to the squad, with a system-built description:

1. Compare last week's outputs with the charter; list what was late or missing.
2. Search the company knowledge for prior art, decisions and stale documents in the
   squad's area.
3. Name at most one thing worth doing that no one asked for, with evidence.
4. File it as a proposal (the attention limits apply) or record `no action` with the
   reason. Padding is a failure; an honest `no action` is a success.
5. Draft updates for stale knowledge-base documents (drafts only).

The review is required by default and its compliance is measured like any routine. The
boss can turn it off per squad.

### 2.4 Completing the proposal lifecycle

States become `pending`, `approved`, `rejected`, `revising`, `expired`, plus a snooze
flag. Added columns: `revision` (starts at 1), `expires_at`, `snoozed_until`.

- **Ask for changes.** `decision: "revise"` with a note moves the proposal to
  `revising`, reassigns the backing issue to the proposing squad (waking its leader) and
  posts the note. The leader changes the body with `multica proposal revise <id>`, which
  increments `revision`, returns the state to `pending`, and does not count against the
  weekly creation cap. Only one open revision at a time.
- **Decide later.** `decision: "snooze"` with `until` keeps the proposal pending and
  hides it from "Needs your decision" until the date. It still counts towards the
  pending caps, so a squad cannot bury the queue by being snoozed.
- **Expiry.** A pending proposal with no decision for 14 days becomes `expired` with a
  system note and one wake-up to the leader; it frees its cap and counts as a
  non-acceptance in 2.6. A daily scheduler job does this.
- **Prior art.** Creating a proposal returns `related` entries when a proposal from the
  last 90 days has a similar title, or the `decisions` knowledge base has a document
  scoring 0.75 or more. It never blocks; the leader's instructions say to cite it, and
  the brief shows "related prior decision".

The Decisions dialog and the brief card gain Ask for changes and Decide later.

### 2.5 Accounted inquiries

The Secretary's inquiries to leaders become records so the cap is enforced rather than
requested.

```sql
CREATE TABLE brief_inquiry (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    asked_by_agent_id UUID NOT NULL,
    squad_id UUID NOT NULL,
    issue_id UUID,
    comment_id UUID,
    question TEXT NOT NULL CHECK (char_length(question) <= 1000),
    asked_on DATE NOT NULL,
    answered_at TIMESTAMPTZ,
    answer_excerpt TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

`multica brief ask --squad <id> [--issue <id>] "question"` posts the comment mentioning
the leader (the existing wake path) and writes the row. At most three per workspace per
UTC day (`inquiry_limit`, 429). When the leader replies, the row is marked answered and
the next digest carries `inquiries: [{question, answered, answer_excerpt}]`, so the
Secretary reads the answer without searching.

### 2.6 Adaptive attention

The fixed caps (3 pending, 3 per week per squad, 10 per workspace) become defaults that a
squad earns or loses. A nightly job recomputes, for each squad with at least five decided
proposals in the last 60 days, the acceptance rate (approved over approved, rejected and
expired):

| Acceptance | Per-squad pending / weekly cap |
|---|---|
| below 20% | 1 / 1 |
| 20% to 60% | 3 / 3 (default) |
| above 60% | 4 / 5 |

The workspace cap of 10 never moves. Table `squad_attention` stores the caps, the rate
and the sample size; the squad panel shows them, and an admin can pin a squad's caps.
Urgent proposals keep their separate one-per-week rule.

### 2.7 Reaching the boss in a chat app

Today the brief lives in the Office and the inbox. The boss asked to talk to the
Secretary mostly through one channel, so the brief must also arrive where they are.

The chat integrations (Telegram, Slack, Lark, DingTalk, WeCom) only reply to inbound
messages; there is no proactive send. Phase 2 adds one, narrowly:

- `channel.Notifier` in `server/internal/integrations/channel`: `Send(ctx, binding,
  Message)` where `Message` is text with optional action buttons. Each platform adapter
  implements it with its own send API, using the installation credentials and the
  existing member-to-chat binding. Telegram ships first; the others are added behind the
  same interface and are listed as separate steps.
- Preferences per member: `brief_push` (off by default), `binding_id`, `quiet_hours`.
  Table `brief_delivery` (brief_id, member id, binding_id, state, attempts, last_error,
  delivered_at) with retry and backoff from the scheduler. Delivery failure never fails
  the brief.
- The pushed message is the headline, the number of decisions, the top three decisions
  with links, and the brief link. It is short on purpose.
- Replying in the chat reaches the Secretary through the existing chat integration.
  `/brief` returns the latest brief and `/decisions` the pending proposals.
- **Deciding from chat** is allowed only through signed buttons: each carries the
  proposal id, the action and an expiry (24 hours) signed with the server key, and on
  press the server resolves the external user to a Multica member through the binding and
  applies the same rules as the app (owner or admin member only). An agent never presses
  a button. If the binding does not resolve to an owner or admin, the button replies with
  a link to the app instead.

### 2.8 The brief feedback loop

The Secretary is the boss's interface, so its quality is measured by the boss. The brief
dialog gets Useful / Not useful and tags (`too_long`, `missing_something`, `wrong`,
`too_vague`, `not_for_me`) with an optional note. Table `brief_feedback` (brief_id, member,
rating, tags, note). `multica brief feedback --recent 14d --json` returns the recent
entries, and the Secretary's template requires reading them at the start of every run and
recording standing preferences in its memory ("always lead with decisions", "skip floor
X"). A rolling `usefulness` figure (share of useful ratings over 14 days) appears in the
Secretary's own scorecard in phase 4.

### 2.9 Catching up a missed brief

The boss's machine may be asleep at 08:00. A scheduler job runs hourly: for each
workspace with a Secretary and an enabled daily brief, if the local time is past the
catch-up hour (default 10:00), no daily brief exists for the day, the Secretary's runtime
is online and no Secretary run is queued or running, it triggers the daily autopilot
once with `source = catchup`. A workspace setting turns it off. The brief card still
offers Generate now.

### 2.10 Phase 2 acceptance

- Every squad created from a template has a charter and its routines; the digest shows
  routine states; a deliberately paused required routine appears in the next brief with a
  fix.
- A squad whose proposals the boss rejects five times in a row is capped to one pending
  and one per week by the next nightly run; the squad panel says why.
- Revise and snooze work from the app and from a chat button; an expired proposal frees
  the squad's cap and wakes its leader once.
- A brief published at 08:00 reaches Telegram within a minute, with working Open and
  Decide buttons, and a brief the machine missed is produced by 10:00 once it is back.
- The Secretary's inquiries stop at three a day and their answers appear in the next
  digest; feedback given on one brief changes the next brief's emphasis (checked by a
  scripted feedback entry and the Secretary's memory file).

## Phase 3: the company operating system

Phase 1 and 2 give the company a body (floors and squads), a mouth (the Secretary) and a
memory (the knowledge base). They do not yet give it a way of working that carries an
idea from "someone noticed something" to "a product is live and learning". Phase 3 adds
that: the pipeline and its initiatives, the knowledge system that feeds and records it,
the starter company that installs all of it in one step, and a directory so squads can
ask each other for work.

Migrations for this phase use numbers 730 to 779.

### 3.1 Initiatives and the pipeline

An **initiative** is one venture moving through the company: a product, a feature line, a
market experiment. It is the unit the boss thinks in ("how is the invoicing tool doing?"),
as distinct from a project (a body of work) or an issue (a task).

```sql
CREATE TABLE pipeline (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    workflow_id UUID NOT NULL,        -- issue_workflow whose steps are the stages
    project_id UUID NOT NULL,         -- the board project initiatives live in
    stages JSONB NOT NULL,            -- see below
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE initiative (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    pipeline_id UUID NOT NULL,
    seq INTEGER NOT NULL,                       -- INI-1, INI-2 ... per workspace
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    thesis TEXT NOT NULL DEFAULT '',            -- why this is worth doing
    stage_key TEXT NOT NULL,                    -- current stage = workflow status key
    owner_squad_id UUID,                        -- who owns the current stage
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'shelved', 'operating', 'closed')),
    issue_id UUID NOT NULL,                     -- backing issue, moved through the workflow
    build_project_id UUID,                      -- created when the Build stage starts
    source_proposal_id UUID,
    sponsor_user_id UUID,                       -- the boss who approved it
    stage_entered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at TIMESTAMPTZ,
    closed_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);   -- unique (workspace_id, seq); indexes (workspace_id, status), (issue_id)

CREATE TABLE initiative_stage_event (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    initiative_id UUID NOT NULL,
    from_stage TEXT,
    to_stage TEXT,
    kind TEXT NOT NULL CHECK (kind IN ('created', 'advanced', 'sent_back', 'shelved',
        'resumed', 'closed', 'forced', 'attested', 'artifact_waived')),
    actor_type TEXT NOT NULL,
    actor_id UUID,
    gate_proposal_id UUID,
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE initiative_artifact (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    initiative_id UUID NOT NULL,
    stage_key TEXT NOT NULL,
    kind TEXT NOT NULL,                         -- a knowledge_kind key
    required BOOLEAN NOT NULL DEFAULT true,
    knowledge_link_id UUID,                     -- set when a published document satisfies it
    waived_by UUID,
    waived_note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);   -- unique (initiative_id, stage_key, kind)
```

`stages` is an ordered array. Each stage:

```json
{ "key": "validate", "name": "Validate", "owner": {"squad_key": "product_discovery"},
  "objective": "Decide with evidence whether the opportunity is worth a product.",
  "instructions": "text the owning leader receives on entry",
  "artifacts": [{"kind": "validation_report", "required": true},
                {"kind": "competitor_profile", "required": true},
                {"kind": "customer_insight", "required": false}],
  "exit": {"mode": "gate", "criteria": "A go / no-go recommendation with evidence."},
  "sla_days": 14 }
```

`owner` resolves a template key to a squad at install and a squad id afterwards. Exit
mode is `auto` (advance when the required artifacts are published), `attest` (the owning
leader must attest the stage is complete, then advance) or `gate` (a boss decision).

**The default pipeline** has seven stages. The front door, Opportunity, is a proposal
rather than a stage: opportunities are cheap, so they are filtered by the attention
budget and the boss's approval before they become initiatives.

| Stage | Owner (starter company) | Required artifacts | Exit |
|---|---|---|---|
| Validate | Product Discovery | `validation_report`, `competitor_profile`, `customer_insight` | gate: commit to a product? |
| Define | Product Analysis | `product_brief`, `analysis` (economics and feasibility), `risk_register` | attest |
| Plan | Planning | `prd`, `roadmap`, `launch_plan` (outline), `budget_request` | gate: approve plan and budget |
| Build | the build squad (SDLC) | `adr`, `test_report`, `release_candidate` | attest, QA verdict recorded |
| Launch | Release & Ops with Growth & GTM | `launch_checklist`, `release_notes`, `gtm_plan` | gate: go live (external effects) |
| Operate | Support, Growth, Analytics | `metrics_report` (weekly, repeating) | gate at 30 and 90 days: continue, scale, sunset |
| Learn | the squad that owned Build | `retrospective` | auto, then the initiative closes |

**How an initiative moves.**

- **Creation.** An approved `opportunity` proposal (see 3.2) creates the initiative at
  Validate: the backing issue is created in the pipeline's project with the stage's
  status, the workflow's handoff assigns it to the owning squad (workflows already hand
  to squads) and wakes its leader with the stage instructions and the thesis, and the
  stage's artifact rows are created. The boss can also create one directly, and the
  Secretary can on request.
- **Artifacts.** A document satisfies a required artifact when it is published, has the
  matching `kind` and carries this `initiative_id` (see 3.3). Optional artifacts never
  block. An owner or admin can waive a required artifact with a note, which is recorded.
- **Advance.** `POST /api/initiatives/{id}/advance`. Preconditions: every required
  artifact of the stage is present or waived, and the exit mode is satisfied. `auto`
  stages advance on their own when the last artifact is published. `attest` stages need
  `multica initiative attest` from a member of the owning squad, then advance. `gate`
  stages create a `stage_gate` proposal (the boss sees it in the brief like any other) and
  advance when it is approved; a rejection with a note sends the initiative back to the
  stage with the leader woken, exactly like a rejected proposal.
- **Moving.** The advance changes the backing issue's status through the issue service,
  so the workflow's existing handoff assigns the next squad and runs it; no routing code
  is added. The build stage also creates the build project with the SDLC workflow and a
  kickoff issue for the build squad, recorded as `build_project_id`.
- **Other transitions.** `send-back` (to the previous stage, note required, admin or
  owning leader), `shelve` and `resume` (owner or admin; pauses stage SLAs and hides the
  card from the active board), `close` (owner or admin, with a reason; closes the backing
  issue and triggers a retrospective, see 4.8), and `force` (owner or admin moves to any
  stage, recorded as `forced`). Agents cannot shelve, close or force; they propose.
- **SLA.** A stage past `sla_days` is `stalled` in the digest and the brief; it is a flag,
  not an automatic move.

Permissions: any member can read; an admin or an approved proposal creates; the rules
above restrict the rest. Every write publishes an event and appends an
`initiative_stage_event`.

### 3.2 Proposal kinds

Proposals gain a `kind` and a `payload`, so the one decision queue serves every question
the boss is asked.

```sql
ALTER TABLE proposal ADD COLUMN kind TEXT NOT NULL DEFAULT 'decision';
    -- 'decision' | 'opportunity' | 'stage_gate' | 'org_change' | 'budget' | 'standing_approval'
ALTER TABLE proposal ADD COLUMN payload JSONB NOT NULL DEFAULT '{}';
```

Each kind has a handler with `Validate(payload)` at filing and `Apply(decision)` on
approval, registered in one place so a new kind is one file:

| Kind | Filed by | Payload | On approval |
|---|---|---|---|
| `decision` | any squad | optional `handoff_squad_id` | Phase 1 behaviour: project and kickoff issue. |
| `opportunity` | Research and Product squads | `title`, `thesis`, `market`, `evidence_doc_ids`, `target_stage` (default `validate`) | Creates the initiative (3.1). |
| `stage_gate` | the system, when a gate stage is ready | `initiative_id`, `from`, `to`, `artifact_ids` | Advances the initiative. |
| `org_change`, `budget`, `standing_approval` | Phase 4 | see 4.4 to 4.7 | see there |

`multica proposal create --kind opportunity ...` takes kind-specific flags and prints the
same prior-art notes as 2.4. The Decisions dialog and the brief render each kind with its
own summary (an opportunity shows its thesis and evidence links; a gate shows the stage's
artifact checklist and the leader's attestation).

### 3.3 Knowledge as the company's record

Phase 1 stores documents; phase 3 makes the store carry meaning.

**Taxonomy.** `knowledge_kind` (per workspace, seeded, editable by admins): `key`, name,
default knowledge base, template (Markdown skeleton with required headings), `review_days`
(freshness), `required_sections`. Seeded kinds: `market_report`, `competitor_profile`,
`customer_insight`, `opportunity_brief`, `validation_report`, `product_brief`,
`analysis`, `risk_register`, `prd`, `roadmap`, `launch_plan`, `budget_request`, `adr`,
`runbook`, `test_report`, `release_candidate`, `release_notes`, `launch_checklist`,
`gtm_plan`, `metrics_report`, `retrospective`, `postmortem`, `decision_record`,
`brief_archive`, `policy`, `playbook`, `reference`.

`knowledge_link` (phase 1) gains `kind`, `initiative_id`, `stage_key`, `squad_id`,
`confidence` (`low`, `medium`, `high`), `valid_until` (date), `supersedes_link_id` and
`tags text[]`. These are also written to WeKnora as tags so MCP search can filter by kind.
`multica knowledge add` takes `--kind`, `--initiative`, `--confidence`, `--valid-until`;
the server checks the kind's required headings and rejects a draft that lacks them with
the missing list. `multica knowledge template <kind>` prints the skeleton. Publishing a
document that supersedes another marks the old one `superseded` and retrieval prefers the
new one.

**Knowledge bases.** The five defaults gain `ventures` (every initiative's documents,
one folder per initiative, named `INI-<seq>-<slug>`). The `company` knowledge base holds
policies and playbooks.

**The Librarian.** A second member of the Executive Office squad. It owns the knowledge
base's health and may not publish except where a publish policy allows it. Its routines:
a daily draft triage (summarise new drafts for the brief, flag duplicates, check required
sections), a weekly freshness review (documents past `valid_until` or the kind's
`review_days` become review requests to the owning squad, or draft updates), a monthly
taxonomy audit (missing kind, orphaned links, `knowledge_link` rows whose document has
gone from WeKnora) and a weekly retrieval check (below).

**Publish policy.** `knowledge_publish_policy` (workspace, scope = knowledge base key or
kind, mode `manual` | `auto_after_review` | `auto`). The default is `manual`. System
writers (decision records, brief archives) use `auto`. `auto_after_review` publishes
when a different squad's critic attests `multica knowledge review <id> --verdict ok`,
and only for squads at autonomy level L2 or above (4.4). Every automatic publish writes a
governance event and appears in the next brief's FYI.

**Retrieval health.** `knowledge_eval_case` (question, expected kind or document id,
owner). The Librarian runs the cases weekly through the search endpoint and records the
hit rate; the digest reports it, and a drop is a risk line in the brief. The boss or any
squad can add cases from a search that returned a bad answer ("this should have found the
pricing decision").

**WeKnora capabilities that need an LLM.** Provisioning gains a models step that registers
a chat/summary model, a reranker and optionally a vision model from the deployment's
OpenAI-compatible endpoint (`KNOWLEDGE_LLM_BASE_URL`, `KNOWLEDGE_LLM_API_KEY`,
`KNOWLEDGE_LLM_MODEL`, `KNOWLEDGE_RERANK_MODEL`; on this deployment they point at the
9router endpoint, and its host must be in the SSRF allow-list). The step tests the
connection and reports exactly which model failed. With a chat model present, Wiki is
enabled on `company`, `ventures` and `decisions` at the `focused` setting, and document
summaries are on; any of this can be turned off per knowledge base. LLM spend inside
WeKnora is not metered by Multica; the cost is bounded by `focused` and by limiting Wiki
to three bases, and is listed as a risk.

**Upload and sources.** The Knowledge page gains drag-and-drop upload
(`POST /api/knowledge/documents/upload`, multipart, up to WeKnora's limit, kind required,
member uploads publish at once as the curator rule says) and URL import. A "Sources" panel
lists WeKnora data sources (Notion, Confluence, GitLab, Feishu, Yuque, RSS and others)
with last sync and failures, read through the adapter; creating one is a link to the
WeKnora UI. A Multica-native routine for the Build floor can sync a repository's `docs/`
directory into the engineering base for teams on GitHub, which WeKnora has no connector
for.

**Access.** `knowledge_acl` (knowledge base, subject type `squad` | `floor` | `agent`,
subject id, access `read` | `write`) turns `restricted` into a real list. A restricted base
is reachable only through MCP endpoints created for its subjects; agents of other squads
cannot see it in `list_knowledge_bases`.

**Lifecycle.** Deleting a workspace deprovisions its WeKnora tenant (a scheduler job with
retry; the tenant's data is removed only after a confirmed workspace deletion and a
seven-day hold, recoverable in between). An owner who wants to use WeKnora's own UI gets a
per-owner account provisioned at enable time; the password is shown once on request and
stored encrypted. Single sign-on remains out of scope.

### 3.4 The starter company

A new company should not mean forty manual setup steps. The **starter company** template
installs a coherent organisation, idempotently and resumably, from the Office's first-run
card ("Build your company").

| Floor | Squad | Mission | Routines (cadence) | Owns |
|---|---|---|---|---|
| Executive | Executive Office | Single point of contact; consolidates, routes, curates | Daily brief; weekly review; Librarian routines | the brief, the knowledge base |
| Research | Market Research | Know the market | Weekly market scan | `market_report` |
| Research | Opportunity Lab | Generate and size opportunities | Weekly opportunity generation | opportunity proposals |
| Research | Competitor Intelligence | Watch competitors | Weekly watch, monthly deep profile | `competitor_profile` |
| Research | Customer Voice | Hear customers | Monthly synthesis; support-signal scan | `customer_insight` |
| Product | Product Discovery | Validate opportunities | Operating review | stage Validate |
| Product | Product Analysis | Economics, feasibility, risk | Operating review | stage Define |
| Product | Planning | PRD, roadmap, plan, budget request | Operating review | stage Plan |
| Build | Build (SDLC greenfield and maintenance) | Build and verify | existing SDLC workflows | stage Build |
| Launch | Release & Ops | Ship safely | Release checklist audit | stage Launch (release) |
| Launch | Growth & GTM | Reach the market | Weekly growth review | stage Launch (go to market), Operate |
| Launch | Support | Keep users | Daily triage; weekly knowledge write-back | stage Operate |
| Governance | Finance & Risk | Money and risk | Weekly budget review (phase 4) | budgets, risk |

Each squad is defined by a **squad card** in `packages/core/squads/company-template.ts`:
name, floor, leader and member roles (each role references an agent instruction template),
charter, routines, default autonomy level, default budget share, and the pipeline stages
it owns. Roles are written once and reused; common blocks (evidence and citation rules,
knowledge-first behaviour, "propose, do not act" for money and outside effects, language)
are shared by all instruction templates so they stay consistent.

Application rules:

- **Idempotent and resumable.** Matched by squad key and name; existing agents, squads,
  floors and routines are reused; a failed step is reported and re-run safely.
- **A runtime chosen once.** The wizard asks for the runtime (and model) for worker
  agents; the Secretary keeps its Hermes runtime. Agents are private to the boss by
  default and given the knowledge MCP server through the existing assignment.
- **No surprise spend.** Routines are created paused except the Secretary's brief and the
  Market Research scan. The wizard shows the estimated runs per week, and an estimated
  cost from the workspace's measured average run cost (or a stated default), and offers
  "Start all routines", "Start per floor" or "Leave paused".
- **Installs the rest.** The default pipeline (with its custom statuses and workflow),
  the knowledge kinds, the `ventures` base, the publish policies, and in phase 4 the
  goals scaffold, budgets and autonomy defaults.
- **Partial templates.** Every squad is optional in the wizard; dependencies are explained
  (a pipeline stage without its squad stays unowned and the wizard says so).

### 3.5 Asking another squad for work

Squads need each other (Build needs Planning's PRD; Growth needs Customer Voice). The
charter's `intake` and `response_hours` (2.1) make this explicit.

- `GET /api/squads/directory` and `multica squad directory` list squads with floor,
  mission, intake, response hours and current open requests. The leader briefing tells
  leaders to consult it before asking the boss.
- A request is an ordinary issue assigned to the other squad, created with
  `multica issue create --assignee-squad <id> --parent <issue>`; it is tagged
  `origin_type = squad_request` so the digest can measure it.
- The digest lists requests older than the receiving squad's `response_hours` as
  `overdue_requests`, and the brief's blockers section names them.

### 3.6 Launch, operate and learn

- **Launch** is the only stage with irreversible outside effects (publishing, announcing,
  charging). Its gate is always the boss's, whatever the autonomy level (4.4). The launch
  checklist is a required artifact whose items are checked by the Release & Ops leader;
  Multica does not publish anything outside the workspace itself.
- **Operate** creates a weekly `metrics_report` routine for the owning squads, registers
  the initiative's key results (4.1), and schedules two review gates, at 30 and 90 days
  after launch (configurable per initiative). Each is a `stage_gate` proposal with three
  options: continue, scale (creates a follow-on initiative at Define), sunset.
- **Learn** runs when an initiative is sunset, scaled or closed (and after a failed
  launch): the retrospective routine in 4.8.

### 3.7 What the Secretary and the Office show

- The digest gains `pipeline`: per active initiative the stage, days in stage, SLA state,
  artifact progress, blockers, the next gate, and `overdue_requests`. The brief gains an
  optional **Pipeline** section (see the brief structure below).
- `/pipeline` is a new route (add `pipeline` to the reserved slugs): a board with one
  column per stage, cards for initiatives (title, owner squad avatar, days in stage,
  artifacts `2/3`, stalled badge), filters by owner squad and status, and a side panel
  for the initiative: overview and exit criteria, artifacts with links into the knowledge
  base and Waive, the backing issue thread, gate proposals with Approve and Reject, the
  stage timeline, and in phase 4 goals and budget. Moving a card by drag is not allowed;
  admins can Force with a reason.
- The Office rooms show a small count of initiatives the squad currently owns; clicking
  opens `/pipeline` filtered to it. The Secretary template learns the `multica initiative`
  commands and the pipeline vocabulary.

**The brief's section order** becomes: Headline, Needs your decision, Progress, Pipeline,
Goals, Budget, Blockers and risks, Watch, People, FYI. The first three, Blockers and
risks, and FYI stay required; Pipeline, Goals, Budget, Watch and People appear when there
is data, and `multica brief publish` accepts them in that order only.

### 3.8 Phase 3 acceptance

- Applying the starter company on an empty workspace with a runtime selected creates the
  floors, squads, agents, charters, routines (paused except the two), pipeline, kinds,
  bases and policies; running it again changes nothing; killing it halfway and re-running
  completes it.
- An approved opportunity proposal creates an initiative at Validate; publishing its
  required documents satisfies the checklist; the gate proposal appears in the next
  brief; approving it moves the backing issue, assigns the next squad and wakes its
  leader; every move is in the stage timeline.
- A document without the kind's required headings is rejected with the missing list; a
  superseding document hides the old one from search results; the weekly retrieval check
  reports a hit rate and a deliberately wrong expectation lowers it.
- A restricted base is invisible to a squad outside its list and visible to one inside it.
- Deleting a workspace removes its tenant after the hold and not before.

## Phase 4: governance

A company that works needs to be steerable. The boss has to say what the company is for
(goals), what it may spend (budgets), how far each squad is trusted (autonomy levels and
standing approvals), stop everything (controls), see whether people and agents are doing
well (reviews) and change the organisation when they are not (org changes), all with a
record nobody can edit. Phase 4 adds those, in that order of dependency, and the scale
features that keep the Secretary readable when the company is large.

Migrations for this phase use numbers 780 to 849. A new route, `governance`, holds the
tabs Goals, Budgets, Autonomy, Reviews, Log and Controls; add it to the reserved slugs.
Where a tab shows data that phase 3 or 2 already has, it links to it rather than copying
it.

### 4.1 Goals, key results and metrics

```sql
CREATE TABLE goal (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('company', 'floor', 'squad', 'initiative')),
    scope_id UUID,
    parent_goal_id UUID,
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    why TEXT NOT NULL DEFAULT '',
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,
    owner_type TEXT NOT NULL CHECK (owner_type IN ('squad', 'member', 'agent')),
    owner_id UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'active', 'achieved', 'missed', 'dropped')),
    confidence TEXT NOT NULL DEFAULT 'unknown'
        CHECK (confidence IN ('on_track', 'at_risk', 'off_track', 'unknown')),
    confidence_note TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL,
    created_by_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (period_start < period_end)
);

CREATE TABLE metric (
    workspace_id UUID NOT NULL,
    key TEXT NOT NULL CHECK (key ~ '^[a-z0-9][a-z0-9_.]{0,63}$'),
    name TEXT NOT NULL,
    unit TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL CHECK (source IN ('builtin', 'manual', 'webhook')),
    direction TEXT NOT NULL DEFAULT 'increase' CHECK (direction IN ('increase', 'decrease')),
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);   -- unique (workspace_id, key)

CREATE TABLE metric_reading (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    metric_key TEXT NOT NULL,
    scope_type TEXT NOT NULL,            -- same values as goal.scope_type
    scope_id UUID,
    value NUMERIC NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    actor_type TEXT NOT NULL,            -- 'system' | 'member' | 'agent' | 'webhook'
    actor_id UUID,
    note TEXT NOT NULL DEFAULT ''
);   -- index (workspace_id, metric_key, scope_type, scope_id, observed_at DESC)

CREATE TABLE key_result (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    goal_id UUID NOT NULL,
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    metric_key TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id UUID,
    start_value NUMERIC NOT NULL,
    target_value NUMERIC NOT NULL,
    weight INTEGER NOT NULL DEFAULT 1 CHECK (weight BETWEEN 1 AND 10),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);   -- index (goal_id)
```

**Metrics.** A metric is a named, unit-bearing series. Three sources:

- **Built-in**, computed nightly by the server into readings, scoped to the company, a
  floor, a squad or an initiative: `delivery.issues_done`, `delivery.cycle_time_days`,
  `delivery.rework_rate` (reopened over done), `runs.count`, `runs.success_rate`,
  `runs.cost_usd`, `proposals.acceptance_rate`, `proposals.decision_hours`,
  `routines.compliance_rate`, `pipeline.initiatives_active`, `pipeline.stalled`,
  `pipeline.stage_days`, `knowledge.docs_published`, `knowledge.fresh_ratio`,
  `knowledge.retrieval_hit_rate`, `brief.usefulness`.
- **Manual**, reported by a member or an agent: `multica metric report --key mrr
  --scope initiative:<id> --value 1200 [--note ...]`, or from the app.
- **Webhook**, for product metrics from outside: `POST /api/metrics/{key}/readings` with
  a workspace token, rate-limited, scope in the body. Webhook sources are how an Operate
  stage's real numbers (signups, revenue, uptime) reach the company.

**Key results and progress.** Progress of a key result is
`clamp((current - start) / (target - start), 0, 1)` using the latest reading in the
goal's period (direction is handled by the sign of the difference). A goal's progress is
the weighted mean of its key results. Confidence is derived against a straight line from
the period's start to its end: `on_track` if progress is at least the expected share minus
ten points, `at_risk` down to thirty points, otherwise `off_track`; with no reading yet it
is `unknown`. The owner may override the confidence with a note, and the override is
shown as such.

**Who sets goals.** The boss and admins create company and floor goals. Squads' goals come
from a **planning cycle**: at the start of each period (quarter by default, setting) a
scheduler job opens a planning issue for every squad with a charter, containing the
company goals and the squad's charter; the leader answers with `proposal` kind `goal`
(payload: a goal and its key results). Approval makes the goal active. The leader
briefing includes the squad's active key results as "Your goals this period", and the
operating review (2.3) is told to report progress against them.

**UI.** Governance → Goals: a tree (company, floors, squads, initiatives) with progress
bars, confidence chips and a sparkline per key result; goal detail with readings and the
proposals and initiatives it links; add a reading. The brief's **Goals** section lists
goals that changed state since the last brief and every at-risk or off-track goal with
its cause (the metric that moved) and a recommendation.

### 4.2 Budgets

```sql
CREATE TABLE budget (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('company', 'floor', 'squad', 'initiative')),
    scope_id UUID,
    period TEXT NOT NULL CHECK (period IN ('week', 'month', 'quarter')),
    amount_micros BIGINT NOT NULL CHECK (amount_micros >= 0),   -- millionths of a US dollar
    soft_percent INTEGER NOT NULL DEFAULT 80 CHECK (soft_percent BETWEEN 1 AND 100),
    mode TEXT NOT NULL DEFAULT 'hold' CHECK (mode IN ('warn', 'hold')),
    classes TEXT[] NOT NULL DEFAULT '{routine,delegated}',      -- which runs it can hold
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused')),
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);   -- unique (workspace_id, scope_type, scope_id, period) where status = 'active'

CREATE TABLE budget_period (
    budget_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    spent_micros BIGINT NOT NULL DEFAULT 0,
    runs INTEGER NOT NULL DEFAULT 0,
    extended_micros BIGINT NOT NULL DEFAULT 0,     -- extensions granted by the boss
    state TEXT NOT NULL DEFAULT 'ok' CHECK (state IN ('ok', 'soft', 'hard')),
    computed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);   -- primary key (budget_id, period_start)

CREATE TABLE budget_hold (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    budget_id UUID NOT NULL,
    task_id UUID NOT NULL,
    agent_id UUID,
    squad_id UUID,
    run_class TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    released_at TIMESTAMPTZ,
    release_reason TEXT                -- 'extended' | 'period_rolled' | 'budget_changed' | 'cancelled' | 'overridden'
);
```

**Spend.** Spend is the sum of `task_usage.cost_usd_ticks` (the provider's own price, in
1e-10 USD) for the scope's tasks; where a provider reports none, the same estimate the
Analytics page uses (token counts times the price table) is used, so the two figures
agree. A task belongs to a squad through `agent_task_queue.squad_id` when set and
otherwise through its agent's home squad; to a floor through its squad; to an initiative
through its backing issue or any issue of its build project. A job every minute reads
`task_usage` rows changed since a stored cursor and updates `budget_period`; a task
finishing also updates the affected periods at once, so the figures lag by seconds.

**Enforcement.** One gate, at the one place every run passes: the task claim in
`TaskService` (`claimTask` and the batch claim). Before a queued task is handed to a
daemon the gate asks `BudgetGate.Admit(task)`. It admits unless a `hold`-mode budget whose
`classes` include the task's run class is in state `hard`, where `hard` means
`spent + reserved >= amount + extended`, and `reserved` is the number of in-flight tasks of
the scope times the scope's trailing 90th-percentile run cost (so concurrent runs cannot
overshoot far). A refusal leaves the task queued (it is not failed), records one
`budget_hold` row per task and publishes an event; the claim simply returns no task. The
gate reads cached counters, so admission is constant time and never queries
`task_usage`. Autopilot dispatch checks the same gate first and records a skipped run
with reason code `budget_hold`.

Run classes: `human` (a person started it by commenting, chatting or assigning; its
originator is recorded on the task) is **never** held, so the boss can always talk to the
company; `routine` (autopilot, wakeup, operating review); `delegated` (agent-to-agent
delegation, squad leader dispatch). The Secretary's brief routine and the catch-up are
exempt. A budget holds `routine` and `delegated` by default.

**States and the boss's levers.** `soft` (at `soft_percent`) only warns: it appears in
the brief and the squad panel. `hard` holds. The boss resolves it by extending the
period (an amount, recorded in `extended_micros`), raising the budget, releasing the
holds once, or letting the period roll; a squad leader can request more with a `budget`
proposal (payload: budget, amount, reason, expected value), which the boss approves or
rejects like any decision. Extensions and releases are governance events (4.5).

**Defaults.** The starter company asks for a company monthly budget and allocates shares
to floors and squads from the squad cards; with no amount given it creates budgets in
`warn` mode at a stated default so nothing holds silently. Initiatives get a budget at the
Plan gate from the `budget_request` artifact; approving the gate approves the budget.

**UI.** Governance → Budgets: scope tree, spent against limit with the elapsed-time
marker and the projected overrun date, holds in force with Release and Extend, history by
period. The brief's **Budget** section lists scopes above the soft line and holds in
force.

### 4.3 Autonomy levels

Each squad has a level, `squad.autonomy_level` (0 to 3, default 1), changed only by the
boss or an admin (and recommended by reviews through proposals).

| Level | Name | What the platform allows |
|---|---|---|
| L0 | Suggest | The squad's routines are held (`autonomy_l0`); it runs only when someone assigns or mentions it. It can still file proposals. |
| L1 | Work | Routines run; the squad researches, analyses, writes knowledge drafts and files proposals. Every decision is the boss's. |
| L2 | Trusted | As L1, plus its knowledge drafts may publish through `auto_after_review` (3.3), and standing approvals (4.4) can approve its proposals. |
| L3 | Autonomous | As L2, plus standing approvals may create initiatives from its own opportunities at the first stage, within caps and budget. |

Two things never depend on level: **launching** (the Launch gate, any step with effects
outside the workspace) and **organisation changes** (4.6) are always the boss's decision.
Money is bounded by budgets at every level.

The honest limit: Multica cannot inspect what a runtime does inside a run, so a level
cannot stop an agent from, say, calling an external API its credentials allow. What the
platform controls is everything that passes through it: which routines run, what is
published into the shared record, which proposals are decided without the boss, which
initiatives are created, how much is spent, and the boundaries written into the charter
and the briefing. Anything with outside effects stays behind a gate the platform owns
(the Launch gate), behind the agent's own permission settings, or behind the daemon's
isolation advice in `security-model.mdx`. The spec states this in the Governance tab so
the boss does not read L3 as "unsupervised".

### 4.4 Standing approvals

Deciding every routine thing is the opposite of what the Secretary is for. A **standing
approval** pre-authorises a narrow class of proposals so the boss sees them afterwards
instead of beforehand.

```sql
CREATE TABLE approval_policy (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    kind TEXT NOT NULL,                      -- a proposal kind; never org_change or a launch gate
    match JSONB NOT NULL,                    -- {squad_ids, floor_ids, max_cost_micros, payload_equals, tags}
    max_per_week INTEGER NOT NULL DEFAULT 3 CHECK (max_per_week BETWEEN 1 AND 50),
    valid_until DATE,
    active BOOLEAN NOT NULL DEFAULT true,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

When a proposal is filed, the policy engine checks active policies. A policy applies only
if the filing squad is at L2 or above (L3 for creating initiatives), the proposal matches,
the weekly cap is not used, the proposal is not urgent and not of a forbidden class, and
the estimated cost fits `max_cost_micros` and the squad's budget. A match approves the
proposal as the system actor `policy:<id>`, applies it exactly as a human approval would,
records a governance event and puts the decision in the next brief's FYI as
"Auto-approved by policy". The boss can revoke a policy at any time, and a policy that
auto-approves something later rejected on review is flagged in the weekly review.

Forbidden for standing approval, in code, not configuration: `org_change`,
launch gates, anything that spends above the squad's remaining budget, any proposal with
`urgent`, and any change to a policy or to autonomy. Only the boss or an admin creates a
policy; an agent may propose one (kind `standing_approval`, payload the policy) and the
boss approves it like any proposal.

### 4.5 Company controls and the governance log

**Controls.** `workspace.company_state` is `running`, `paused` or `quiet`. Paused: queued
`routine` and `delegated` runs are held (reason `company_paused`), autopilots skip with
that reason, wakeups do not fire, automatic knowledge publishing and pipeline
auto-advance stop, and standing approvals do not apply. `human` runs, the Secretary's
brief and chat keep working, so the boss can still see and talk to the company. "Pause
and stop running work" also cancels running `routine` and `delegated` tasks through the
existing cancel path. `quiet` is a daily window in the workspace timezone (default
22:00 to 07:00) during which routine runs are deferred, not dropped, except exempt
ones. The Office header shows the state and a Pause button; Governance → Controls holds
the settings; `multica company pause|resume|status` exists for the CLI.

**The governance log.** An append-only table records every action that changes how the
company is governed:

```sql
CREATE TABLE governance_event (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    kind TEXT NOT NULL,        -- policy_auto_approve, policy_created, policy_revoked,
                               -- budget_set, budget_extended, budget_hold, budget_release,
                               -- autonomy_changed, org_change_applied, org_change_reverted,
                               -- auto_publish, initiative_forced, company_paused,
                               -- company_resumed, goal_activated, review_published
    actor_type TEXT NOT NULL,  -- 'member' | 'agent' | 'system' | 'policy'
    actor_id UUID,
    subject_type TEXT NOT NULL,
    subject_id UUID,
    summary TEXT NOT NULL CHECK (char_length(summary) <= 500),
    details JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);   -- index (workspace_id, created_at DESC); no update or delete path exists in code
```

Governance → Log filters by kind, actor and subject and exports CSV. Retention is
indefinite; the table has no `UPDATE` or `DELETE` query, and a database test asserts it.

### 4.6 Reviews and organisation changes

**Scorecards.** A nightly job computes, per agent and per squad and for the company, a
scorecard for the last week and month and stores it (`scorecard`: subject type and id,
period, `metrics` JSONB, `sample_size`, `computed_at`). Metrics: runs, success rate
(failures split by cause so a runtime being offline is not charged to the agent),
issues done, first-pass rate (done without being reopened), cycle time, cost per done
issue, human intervention rate (human comments or edits after an agent marked work done),
proposal acceptance rate, routine compliance, knowledge contribution (published
documents, review verdicts), and for the Secretary the brief usefulness (2.8). A
scorecard with too few runs for its period is marked `insufficient` and is never reviewed.

**Reviews.** The `People & Performance` squad on the Governance floor holds a reviewer
agent. Monthly it reviews every squad that has enough data, and agents flagged `below`
by their scorecard or sampled, producing a `review` record:

```sql
CREATE TABLE review (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('agent', 'squad', 'company')),
    subject_id UUID,
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,
    scorecard_id UUID NOT NULL,
    rating TEXT NOT NULL CHECK (rating IN ('exceeds', 'meets', 'below', 'at_risk')),
    narrative TEXT NOT NULL CHECK (char_length(narrative) <= 8000),
    recommendations JSONB NOT NULL DEFAULT '[]',   -- [{type, rationale, proposal_id}]
    reviewer_agent_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

A review must cite the scorecard figures it relies on and the evidence (issues, runs,
documents). Each recommendation of a kind that changes something becomes an `org_change`
proposal, so the boss decides with the evidence beside it. The brief's **People** section
lists new reviews and their recommendations. A review of a person, an agent or a squad
is a working tool, not a ranking: it is visible to owners and admins and to the subject
squad's leader, and its metrics say what they measure.

**Organisation changes.** Proposal kind `org_change`, with `payload.type`:

| Type | Payload | Applied by |
|---|---|---|
| `instruction_change` | agent, new instructions, diff summary, rationale | Updating the agent; a revision row is written first. |
| `skill_change` | agent, skills to add and remove | The existing skill assignment. |
| `charter_change` | squad, new charter | The charter update (2.1), revisioned. |
| `autonomy_change` | squad, from, to | Updating the level. |
| `hire` | squad, instruction template or role, name, runtime | Creating the agent from the template, adding it to the squad with it as home squad. |
| `retire` | agent or squad, rationale | Archiving; queued tasks are cancelled, running ones finish, the knowledge stays. |
| `move` | agent, to squad | Membership and home squad. |
| `merge_squads` | from, into | Members and routines move; the source is archived; the charter text is merged by the boss in the dialog. |

`agent_instruction_revision` (agent, revision, instructions, source `human` | `proposal` |
`rollback`, proposal, actor, created_at) is written by **every** instruction update, from
any path, through one hook in the agent update service, so history exists even for
changes made by hand, and any revision can be restored. The Decisions dialog renders an
instruction or charter change as a diff. Applied changes enter a **probation window** of
14 days: at its end the reviewer compares the scorecard before and after and records
`improved`, `neutral` or `worse`; `worse` raises a rollback proposal. Org-change
proposals count towards the attention limits, are filed only by the reviewer, the
Secretary, the subject squad's leader (for their own squad's charter and members) and
members, and never auto-approve.

### 4.7 Retrospectives and the learning loop

Learning has to leave the heads of the agents that had the experience and enter the
record every agent reads.

| Trigger | Subject |
|---|---|
| An initiative is closed, sunset or scaled; a Launch gate is rejected twice | The initiative and its owning squads |
| An agent has three failed runs of the same failure category in a week | The agent |
| A squad's proposals are rejected three times in a row | The squad |
| A budget hold occurred | The scope |
| A stage SLA is breached twice | The initiative stage |

A scheduler job fires triggers (one per subject per 14 days; table `retro_trigger` keeps
them) and creates a retrospective issue for the right leader from a template: what
happened, the evidence, the root cause, what to change. The deliverable is a published
`postmortem` or `retrospective` document in the knowledge base and up to three
improvement proposals (an `instruction_change`, a `charter_change`, a runbook). The
instruction block tells agents to search `postmortem` documents before starting similar
work, so a lesson that has been paid for once is not paid for again.

### 4.8 Scale: floor chiefs, scoped briefs and the pulse

One Secretary reading every squad stops being readable at some size. Past the point where
the digest would otherwise omit squads, two additions keep it honest:

- **Floor chiefs.** `floor_chief` (floor, agent). A chief is an agent (template
  `floor_chief`) that publishes a daily floor brief for its floor from a floor-scoped
  digest (`GET /api/office/digest?floor_id=`). `brief` gains `scope_type`
  (`company` | `floor`) and `scope_id`; a floor brief needs a subset of the headings.
  A workspace setting `digest_mode` (`auto` | `squads` | `floors`) controls the
  Secretary's source: `auto` switches to floor briefs when there are more than 30 active
  squads. The Secretary then reads the latest floor briefs plus the company-level
  sections (pipeline, goals, budget, decisions) and drills into a floor only on a flag.
- **The pulse.** `GET /api/office/pulse` returns, cached for a minute: company state,
  decisions waiting and the oldest age, goals at risk, budgets above the soft line and
  holds, stalled initiatives, routines late, the knowledge retrieval hit rate, and
  runtimes offline. The Office header shows it as a compact strip that opens
  Governance on the relevant tab.

### 4.9 Operating the system

- **Company as code.** `GET /api/office/export` and `multica company export` produce a
  JSON description of the organisation (floors, seats, squads with charters and
  routines, agents' roles and template references, pipeline, kinds, policies, goals,
  budgets, autonomy levels). `multica company import` recreates it idempotently in
  another workspace. It excludes secrets, runtimes, documents and history. This is both a
  backup of the structure and how a second company is started.
- **System health.** Governance → Controls shows the last success and lag of each
  scheduled job (attention recompute, catch-up, budget cursor, scorecards, retros,
  delivery retries, knowledge freshness, eval), delivery failures, WeKnora health and the
  budget gate's cache age. A stuck job is a risk line in the brief.
- **Several owners.** Any owner or admin is "the boss". A decision applies once; the
  second concurrent decision gets 409 with the decider's name. The log shows who decided.
- **Retention.** `metric_reading` older than 90 days is downsampled to daily values;
  everything else is kept. Exports include a retention note.
- **Security review** before release: signed button tokens and expiry, the policy engine
  cannot be edited by any agent actor (tested), instruction text and imported documents
  are rendered as inert Markdown, retrieved text is data in every briefing block, new
  endpoints are rate-limited and size-capped, the service account's scope is minimal.
- **Backups.** Document the Postgres tables and the WeKnora volumes (`postgres-data`,
  `data-files`), the restore order, and a restore drill in the plan.

### 4.10 Phase 4 acceptance

- A company goal with three key results shows progress from a built-in metric, a manual
  reading and a webhook reading; confidence moves from `on_track` to `at_risk` when the
  readings fall behind the line; the brief names the cause.
- A squad whose budget reaches its limit has its next routine run held with a visible
  reason while a boss chat still works; Extend releases the hold and the task runs;
  the log has both events.
- A standing approval auto-approves a matching proposal from an L2 squad, refuses the
  same proposal from an L1 squad, is revoked in one click, and never touches a Launch
  gate or an `org_change` (tests try each).
- Pause stops routine runs within a minute and not chat; resume restores them; quiet
  hours defer rather than drop.
- A monthly review of a squad with enough data cites its scorecard, produces an
  `instruction_change` proposal with a diff; approving it writes a revision; rolling it
  back restores the earlier text; the probation check records an outcome.
- A failed initiative produces a retrospective issue, a published postmortem document and
  a proposal; a later agent's search for the topic returns the postmortem.
- With more than 30 squads, `digest_mode = auto` produces floor briefs and a company
  brief that reads them and stays within the length target.

## Out of scope

Excluded on purpose, even after phase 4:

- Mobile. The office, pipeline and governance pages are web and desktop only; the inbox
  already notifies mobile.
- Agents drawn in squads other than their home squad, humans on the floor, and new rooms
  such as a boss office, meeting room or library (each needs an engine patch with new art).
- WeKnora single sign-on, and WeKnora's own agents, automations, channels and per-user
  memory. The company has one agent system and one identity system.
- The Secretary deciding in the boss's name. Pre-authorisation exists only as standing
  approvals (4.4), which are narrow, capped, revocable and logged.
- Inspecting or sandboxing what a runtime does inside a run. Governance controls what
  passes through Multica (4.3 says exactly what).
- Business systems beyond metric webhooks: payments, invoicing, CRM, ad platforms and
  hosting are reached only by agents using their own tools under their own credentials.
- A company-spanning view across several workspaces, and one Secretary per member.
- Training or fine-tuning models.

## Testing

Per the repo layers; one canonical layer per behaviour.

- **Go (DB-backed, `testutil` fixtures):** seat uniqueness, swap in one
  transaction, auto-seat on squad create including new-floor overflow, home
  squad cleared on membership removal and squad archive, floor delete refused
  when occupied, proposal create membership check, both attention limits,
  approve with and without handoff (project and kickoff issue created, handoff
  leader enqueued), reject posts the mention comment, decision refused for a
  non-owner/admin or agent actor, workspace isolation on every new query.
- **Core:** `layoutFloor` matrix (leader first, overflow past ten, unassigned,
  human members skipped, archived squads, slots 0 to 5), zod malformed-response tests for each new
  endpoint.
- **Views:** `OfficeScene` seats by floor, tag priority, floor tabs, squad panel,
  Decisions dialog approve/reject, unseated tray, room badge. Mock stores with
  the callable shape plus `getState`, API at `@multica/core/api`.
- **Go (secretary):** digest caps and ordering, quiet-squad collapse, stale and
  blocked calculations, workspace isolation; brief publish allowed for the
  Secretary and admins only, refused for other agents; required headings and size
  limit; `brief_ready` inbox item per owner; decision endpoint refuses an agent
  actor; unassigned proposals raise no inbox item and no run, urgent ones do;
  setup creates the agent, floor, squad, setting and both autopilots.
- **Views (secretary):** brief dialog renders the body and the proposals with
  Approve and Reject, unread dot, empty state with Generate now, Ask prefill.
- **Go (knowledge):** the adapter against recorded WeKnora responses (login,
  tenant, key, KB, document, publish, search, MCP endpoint); provisioning is
  idempotent and resumable after a failure at each step; a draft is not published
  until a person publishes it; idempotency keys prevent duplicate decision records;
  a restricted KB's endpoint is assigned only to named agents; the instruction
  block appears only for workspaces with knowledge enabled; everything degrades
  when the sidecar is unreachable.
- **Views (knowledge):** KB list, search results with sources, upload and URL
  import, Drafts publish and discard, "Save to knowledge", offline state.
- **CLI:** `proposal create` flag validation and limit message; `brief digest`
  flags; `brief publish` heading validation.
- **E2E (`TestApiClient`):** the first loop from a created research squad to an
  approved proposal that starts a project, with the autopilot triggered
  manually rather than waiting for the cron.
- **Phase 2:** charter CRUD and revision conflicts; the briefing block text; routine
  state derivation for each state and output kind; the `silent` flag; revise, snooze and
  expiry transitions and cap effects; related-prior-art notes; inquiry cap and answer
  capture; adaptive caps at each band and the pin; notifier adapters against recorded
  platform responses, retry and quiet hours; signed button verification (expired, wrong
  action, non-owner binding, agent actor); feedback storage and CLI; catch-up fires once.
- **Phase 3:** pipeline install creates statuses and workflow idempotently; initiative
  creation from an approved opportunity; artifact satisfaction, waiver and the three
  exit modes; gate proposal approve and reject; send-back, shelve, close, force
  permissions; SLA flags; proposal-kind registry (unknown kind is refused); kind headings
  validation and supersession; publish policies including `auto_after_review` with and
  without an L2 squad; Librarian jobs; retrieval check; restricted-base ACL; workspace
  deletion deprovisioning with the hold; starter company idempotency and resume at each
  step; directory and overdue requests.
- **Phase 4:** metric reading ingestion and the webhook (auth, rate limit); progress and
  confidence derivation; budget attribution to squad, floor and initiative; the gate for
  each run class (human never held), reservation arithmetic, period roll and release;
  policy engine approves and refuses each forbidden class; level behaviours; pause,
  resume and quiet; the governance log has no update or delete path; scorecards with
  insufficient data; reviews cite scorecards; every org-change type applies and reverts;
  instruction revision on every update path; probation outcome; retro triggers dedupe;
  floor-scoped digest and `digest_mode = auto`; export and import round trip.
- **Views (phases 2 to 4):** charter and routines tabs, Decisions with revise and
  snooze, pipeline board and initiative panel, Knowledge upload and sources, Governance
  tabs, pulse strip, pause control, diff rendering of instruction and charter changes.
- **E2E:** the six "company day" scenarios of the roadmap, scripted over the API with
  fake leaders plus the UI checks that do not need a model; a Hermes smoke test of the
  brief (`agentintegration`, only on authorisation).
- Default tests never resolve or run real agent CLIs. A Hermes smoke test of the
  daily brief is gated behind `agentintegration` and `MULTICA_RUN_REAL_AGENT_SMOKE=1`,
  and runs only when the boss authorizes it.

Run `make sqlc` after the SQL changes, then `pnpm typecheck`, `pnpm lint`,
`pnpm test` and `make test`.

## Delivery order

Atomic conventional commits, in this order, each leaving the app working:

0. Engine: 6 rooms of 10 desks, rebuilt wasm, patch and README. It touches only
   `lib/pixtuoid`, so it can be built in parallel with 1.
1. Floors, seats, home squad: migrations, handlers, core, `layoutFloor`; remove
   `office_room`.
2. Office rebuilt around floors: tabs, squad rooms, tags, unseated tray. Needs 0
   and 1.
3. Squad panel and agent panel changes.
4. Proposals: migration, service, handlers, CLI, leader briefing, attention
   limits.
5. Decisions dialog, room badge, approve handoff.
6. `market_research` template and weekly routine.
7. Digest: queries, endpoint, `multica brief digest`.
8. Briefs: migration, endpoints, `brief_ready` inbox type, `brief publish`, the
   brief dialog and Generate now.
9. Secretary: instruction template, `secretary_agent_id`, the Executive template
   and the first-run setup flow, daily and weekly autopilots.
10. Knowledge base: sidecar and provisioning, MCP read path, governed write path,
    Knowledge page, automatic context. The detailed steps are in
    [the plan](digital-company-office-plan.md), workstream 7; they depend only on
    the repo's existing agent and MCP machinery, so they can start in parallel with
    1 and 4.
11. Docs under `apps/docs` (Office, Squads, Autopilots, a Secretary page, the
    knowledge base) in the supported locales.

Steps 7 to 9 need 1 and 4. The Secretary is useful before 6 (it reports on any
squad), so 6 and 7 to 9 can ship in either order.

Steps 0 to 11 are built. The remainder is organised in the plan as workstreams:

12. **Phase 2 completion** (plan WS9): charters and routines, compliance, operating
    review, proposal lifecycle, inquiries, adaptive attention, chat-app delivery,
    feedback, catch-up.
13. **Phase 3** (plan WS10): proposal kinds, pipeline and initiatives, knowledge taxonomy,
    Librarian and policies, starter company, directory, Operate and Learn mechanics.
14. **Phase 4** (plan WS11): metrics and goals, budgets and the gate, autonomy levels and
    standing approvals, controls and the governance log, scorecards, reviews and org
    changes, retrospectives, scale.
15. **Release** (plan WS12): documentation, the six company-day acceptance scenarios,
    security review, backup drill, upgrade notes.

Within the new phases the order is forced by dependencies: proposal kinds (3.2) before
gates and goals; charters (2.1) before the starter company; the budget gate (4.2) before
standing approvals that mention cost; the revision hook (4.6) before any org change.

## Decisions

Decisions 1 to 5 settled the open questions of the first draft. Decisions 6 to 10 were
taken while building phase 1. Decisions 11 to 22 are the design choices of phases 2 to 4.

1. **Drop `agent.office_room`: yes.** The commits that added it are on this fork's
   `main` (merged through PR #22) but in no release tag, and the values were room
   labels unrelated to squads. Keeping a dead column and an enum mirror would be
   the compatibility shim the repo rules ask to avoid.
2. **The boss is the workspace owner.** Any owner or admin can decide. No
   per-squad "reports to" setting. Proposals are no longer addressed to the boss
   at all (see 6): they queue and reach the boss through the Secretary, except an
   urgent one, which is assigned to the oldest owner.
3. **Floor names.** Floors made by hand are `Floor N` and are renamed in place with
   a double-click; floors made by a template carry the template's name. Existing
   squads are not auto-seated by a migration. An owner or admin gets a one-click
   "Seat all" banner, which keeps the first view of the new office a decision by
   the boss rather than a silent rewrite.
4. **Attention limits.** 3 pending and 3 per week per squad, 10 pending per
   workspace. The first draft's 5 per week was too loose for six squads a floor,
   and per-squad limits alone do not bound the boss's queue. Constants for now.
5. **Room size.** A floor is 6 rooms, as asked. The evidence on squad size says
   the bigger problem is desks, not rooms: the built-in SDLC squads have 9 and 10
   agents, so 6 rooms of 4 desks would still hide more than half of each. Rooms
   therefore get 10 desks (see [Engine](#engine)), with `+N` for the rare larger
   squad.

6. **One Secretary, pull model.** Departments never report to the boss; the
   Secretary reads a server-computed digest and writes one brief. A per-squad
   report would multiply the very flood the boss wants removed and spend runs on
   status nobody asked for. Proposals stop pinging the boss for the same reason.
7. **Secretary is in a squad.** As leader of `Executive Office`, it needs no
   special seat, and the boss can route unowned requests to it by assigning or
   mentioning the squad. The brief autopilot targets the agent, not the squad.
8. **Hermes for the Secretary, Multica for everything else.** Use Hermes' memory
   and skills; use Multica's autopilot, brief, inbox and chat for scheduling and
   delivery. Hermes' own cron and gateway are not used: the daemon retires the
   run's gateway, so they would not persist.
9. **The boss decides.** The endpoint refuses agent actors; chat answers with
   links, not approvals.

10. **WeKnora as the knowledge engine, behind our boundary.** Verified on this
    machine (see the hands-on evaluation): multilingual retrieval, draft and
    publish, MCP with tool and KB scoping, tenant isolation and scoped keys all
    behave as documented, at about 650 MB idle. It is adopted as a pinned sidecar;
    Multica's own API is the contract, so the engine can be replaced. Agents read
    through MCP and write drafts through Multica, and WeKnora's own agents and
    logins are not used.

11. **A charter is data, not prose in `description`.** A separate table lets the
    platform inject it into the briefing, measure routines against it and revision it.
    Routines are ordinary autopilots linked to the squad; no second scheduler.
12. **The pipeline is a workflow plus an initiative.** Workflows already hand an issue
    to a squad when it enters a status, so stages are statuses and routing needs no new
    code. The initiative adds what an issue cannot hold (thesis, artifacts, gates,
    history). Opportunity is a proposal, not a stage, so the attention budget filters
    ideas before they cost anything.
13. **One decision queue, many kinds.** Opportunity, stage gate, organisation change,
    budget and standing approval are proposal kinds with a handler each. The boss learns
    one interface and the Secretary has one thing to summarise.
14. **Budgets are enforced at the claim, not at enqueue.** Every run passes the claim, and
    enqueue paths are many. Human-started runs are never held so the boss can always talk
    to the company; a trailing 90th-percentile reservation keeps concurrent runs from
    overshooting. Budgets are cost-in-dollars first, because that is what the boss
    reasons about.
15. **Autonomy is bounded by what Multica can observe.** Levels govern routines,
    publishing, standing approvals and initiative creation. Launch and organisation
    changes are always the boss's. The limit (no inspection of a runtime's internal
    actions) is stated in the product rather than hidden behind the word "autonomous".
16. **Standing approvals are narrow, capped and revocable, and the forbidden classes
    are in code.** They exist so the boss sees routine decisions afterwards; they cannot
    approve a launch, an organisation change, an urgent item or a change to themselves.
17. **The governance log is append-only by construction** (no update or delete query
    exists) so the boss can trust it when they disagree with the system or an agent.
18. **Reviews produce proposals, not actions.** A review cites its scorecard; any change
    it recommends is an `org_change` the boss decides with a diff in front of them, with
    revisions and a probation check so a bad change can be undone.
19. **Scale is a mode, not a rewrite.** Floor briefs and the pulse switch on when the
    company outgrows one Secretary's reading; below that the Secretary reads squads.
20. **Proactive chat delivery is a narrow notifier plus signed buttons.** Telegram first;
    the same interface for the others. Deciding from chat resolves the external user to a
    member through the binding and never lets an agent press a button.
21. **The starter company creates routines paused, except two.** A new company that
    quietly starts forty schedules is a surprise bill. The wizard shows the estimate and
    the boss chooses.
22. **Company as code.** Exporting and importing the organisation's structure makes
    backup, cloning and review of a company definition possible without copying data.

## Open risks

- **Engine rebuild.** The grid and the desk count are constants, but the walkable
  mask, door placement and the lounge column were tuned for 2 x 2. If the 3 x 2
  grid with 10-desk pods does not meet the acceptance size, use the 8-desk
  fallback before changing the grid.
- **Hermes in practice.** Hermes v0.22.5 is installed and answers through the 9router
  endpoint (`hermes -z`), but a run through Multica's daemon and the daily brief on the
  boss's machine are not verified. Plan step 9.14 is the acceptance run.
- **Machine uptime.** The brief runs on the boss's machine. If it sleeps, the 08:00
  run is skipped; the Generate now card is the mitigation, not a guarantee.
- **Read-everything agent.** The Secretary sees every issue and comment. It is
  private to the boss, and there is no per-issue privacy to bypass today; revisit
  if issue-level privacy is added.
- **Push to chat apps.** Whether an autopilot run can deliver into a Telegram, Slack
  or Lark chat is unverified; the channel code is built around replying to
  inbound messages. Until proven, the brief lives in the Office and the inbox.
- **Digest accuracy.** The brief is only as good as the digest. The stale and
  blocked rules are first guesses and should be tuned on real use.
- **WeKnora API churn.** It changed between 0.8.3 and 0.9.0 within weeks. The
  version is pinned and the adapter is contract-tested; a bump is its own step.
- **Service account.** Multica holds one WeKnora service user's credentials. Keep
  them in the deployment environment, never in the repo or a log, and scope that
  user to creating tenants.
- **Knowledge quality.** A shared KB is only as good as what is published. The
  draft gate and the decisions KB are the defence; the first weeks of real use
  decide whether auto-publish is ever safe.
- **LLM-dependent WeKnora features** (Wiki, summaries, `ask`, rerank) are untested
  and add model spend; they stay off until an LLM is configured and measured.
- **Budget attribution lag.** Cost is known after usage is reported, so a hold can arrive
  after the money is spent. The reservation bounds the overshoot to roughly one run per
  concurrent slot; the brief says "projected", not "exact".
- **Providers without cost.** Where a provider reports no cost, the estimate from tokens
  is used. If a model is missing from the price table the figure is zero; the budget page
  lists such models so the boss can add a price.
- **The claim path is hot.** The gate must stay constant time. It reads cached counters
  refreshed by the job and by task completion; a stale cache fails open for `warn` mode
  and fails closed only for a budget already in `hard`.
- **Agents reporting their own metrics.** An agent can post a favourable reading. Agent
  readings are labelled, goals meant for judgement should use built-in or webhook
  sources, and the review compares reported against measurable figures.
- **Policy misuse.** A broad standing approval is a hole. Policies have caps, an expiry,
  forbidden classes in code and a log; the weekly review flags policy approvals that were
  later reversed.
- **Review fairness.** Scorecards are only as good as their causes. Failures are split by
  cause, small samples are excluded, and recommendations are proposals the boss judges.
- **Cost of the starter company.** Forty agents means real spend even when each is idle
  between routines. Routines start paused, budgets start in `warn` mode, and the wizard
  shows the estimate before anything runs.
- **Chat delivery per platform.** Each platform's send permission, rate limit and
  identity binding differ. Delivery failure never fails a brief; Telegram is proven
  first.
- **Unmetered WeKnora LLM spend.** Wiki and summaries call a model inside WeKnora, which
  Multica does not meter. They are limited to three bases at `focused`, and can be turned
  off per base.
- **Template quality.** Forty instruction templates are forty prompts. They share common
  blocks, are reviewed by the Librarian's checks and the boss's feedback, and the
  weekly reviews are the long-run correction.
- **Migration volume.** Phases 2 to 4 add about thirty tables. Each ships with its
  feature, behind the workspace opt-in that already gates the Secretary and knowledge, so
  an unused phase costs nothing at runtime.
- **Prompt compliance.** The attention limits are enforced by the server, but
  proposal quality (evidence, a real recommendation) depends on the leader's
  briefing and the critic role. The first loop is also the test of that.
