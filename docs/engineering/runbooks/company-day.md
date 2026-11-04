# Company day, manual run (plan 12.2)

The scripted run, `server/internal/handler/companyday_test.go` (`TestCompanyDay`),
covers the six scenarios over the handler API with a fake clock for the schedules.
It cannot cover the parts that need a real Hermes agent, Telegram, WeKnora with a
model, or the boss's own clock. This runbook covers those parts and is run once on the
boss's machine, on a clean workspace, with the starter company.

Do not run it on the main checkout's database. Use a new workspace.

The control names below follow the spec. If the build labels a control differently,
use the matching control and write the label you saw in the run notes.

## Before you start

1. Environment. In the checkout: `make up`, then `make status` shows the API, web and
   daemon as running. Note the web address (default `http://localhost:30000`).
2. Clean workspace. In the web app, sign in and create a new workspace named
   `Company Day <date>`. Do not import anything into it.
3. Hermes runtime. Install the Hermes agent runtime with the link `install-agent-runtime`
   shows, and start the daemon (`make daemon`). In the app, the runtime must show
   **online** under Settings > Runtimes.
4. Telegram. Create a bot with BotFather and copy its token. In Settings > Chat, add a
   Telegram integration and bind your Telegram account to the Secretary. Send the bot
   `/start`; the binding is confirmed in the app.
5. WeKnora. Start the knowledge sidecar from `docker-compose.selfhost.yml`. In WeKnora,
   configure the LLM and the embedding model (`bge-m3` in the spec's measurements).
   Check that the knowledge search answers a test question before you start the run.
   The keys in `.env` are not printed or copied anywhere.

## The run

Record the time, the result (pass or fail) and any note for each step.

### 1. Morning: the 08:00 brief

1. Confirm the Secretary's daily autopilot shows **08:00** in your timezone under the
   Secretary's routines. Expected: the next run is 08:00 tomorrow.
2. Leave the machine on overnight, or press **Generate now** on the brief card.
3. Expected at 08:00: a brief appears on the brief card, with unread dot. Telegram
   receives the same brief within a few seconds of publish.
4. Read the brief. Expected: under two minutes; the headline is at most three lines;
   it names what was delivered, what is blocked and why, what is recommended, the
   goals, budget burn and the decisions waiting.

Result: ____  Time read: ____ min

### 2. Discovery: the Research floor

1. Wait for the Research routines to run (or start them from each squad's routines
   tab). Expected: reports are written into the knowledge base only after review.
   Check the Knowledge page: a new report is a draft until you publish it.
2. Expected: at most three opportunity proposals waiting from the Research squads. The
   attention limit refuses a fourth.
3. Approve one opportunity from the brief (Approve on the brief card).

Result: ____  Proposals waiting: ____

### 3. Venture: the initiative

1. Expected: the approved opportunity appears in the Pipeline at **validate**, owned by
   the Product squad.
2. Expected: the validate gate is a proposal you can see in Decisions after the
   required documents are published (validation report and competitor profile). Approve
   it. The initiative moves to **define** under the Analysis squad.
3. Expected: the Analysis leader attests define when its documents are published. The
   initiative moves to **plan**. Plan's exit is a gate; leave it pending.

Result: ____

### 4. Ask: what do we know and what did we decide

1. In the Secretary's chat, ask: "What do we know about <the market> and what did we
   decide?"
2. Expected: the answer cites knowledge-base documents by title, and the decisions
   from step 2 and step 3 by their identifier. It calls `multica brief digest` before
   answering (the instructions say so).
3. Expected: nothing is decided by the Secretary. Ask it to approve the plan gate; it
   refers you to the Decisions dialog.

Result: ____  Cites documents: yes / no

### 5. Control: budget and pause

1. In Office > Budget, create a company budget: monthly, $1.00, hold mode, soft line 80%.
2. Let a routine run cost past the soft line (the Analysis squad's routine is enough).
   Expected: the next morning's brief names the budget under Budget. Expected at the
   limit: the next routine run of a squad is held with the reason "budget", and it
   appears in the governance log.
3. Press **Extend** on the budget. Expected: the held run starts; the log has the hold
   and the release.
4. Press **Pause company** in the Office header. Expected within a minute: no routine
   run starts. Chat with the Secretary still works. Resume. Routine runs start again.

Result: ____

### 6. Improvement: the weekly review and a retrospective

1. Friday 17:00, or press **Run review** for the week in the Governance tab. Expected:
   each squad has a scorecard, and the reviews name the squads that need attention.
2. Close one failed initiative (Close on the initiative panel, with a reason). Expected:
   a retrospective issue appears for the Learn stage, and a published postmortem
   document after you publish it.
3. Open an instruction-change proposal for a squad's agent (the review's suggestion, or
   edit an agent's instructions). Expected: the proposal shows a diff. Approve it; the
   revision is written. Roll back in the agent's revision history; the earlier text
   returns.

Result: ____

## After the run

1. Export the company structure: `multica company export --file company-day.json`.
   Keep the file with the run notes.
2. Stop the machine: `make down` (keeps the data).
3. Keep the workspace until the sign-off below is complete.

Sign-off

Run by: ______________________   Date: ______________

Boss: ______________________   Date: ______________
