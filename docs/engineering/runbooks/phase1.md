# Phase 1 runbook: the boss's machine

These steps cannot run in CI. They need Hermes, the machine the boss works on, and
(for the knowledge steps) a WeKnora sidecar. Plan 8.3 is the reference. Each step
lists its expected result. No step has been run yet, so every row starts as
**not verified**. The boss signs off each one by writing the date and the observed
result in the Sign-off column, or by recording why it could not be done.

## 1. Hermes online (WS0.2)

Prerequisite: Hermes is installed on the machine that runs the Multica daemon, and a
model provider is configured in Hermes (`hermes model`). Only the boss can supply it.

| # | Step | Expected result | Sign-off |
|---|---|---|---|
| 1.1 | Start the Multica daemon. Run `multica daemon status`. | A `hermes` runtime is listed as online. | not verified |
| 1.2 | Open the Office, press **Set up your company** and create the Secretary on that runtime. | The Secretary exists as a private agent with concurrency 1; the `Executive` floor and `Executive Office` squad exist; the Secretary is pinned in Chat. | not verified |

## 2. Daily brief generated

| # | Step | Expected result | Sign-off |
|---|---|---|---|
| 2.1 | Open the brief dialog from the floor tabs and press **Generate now**. | A run starts on the Secretary; within a few minutes a brief appears with the sections Headline, Needs your decision, Progress, Blockers and risks, Watch, FYI in that order. | not verified |
| 2.2 | Check the inbox. | One `brief_ready` item for the boss, severity attention. | not verified |

## 3. A proposal approved from a brief

Prerequisite: a squad files a proposal with a handoff squad (for example the
`market_research` squad on the Research floor, after its first weekly scan, or a
hand-filed `multica proposal create`).

| # | Step | Expected result | Sign-off |
|---|---|---|---|
| 3.1 | Open the proposal from the brief with **Approve**, choosing the handoff squad. | The proposal is approved; a project and a kickoff issue assigned to the handoff squad exist; the handoff leader is woken. | not verified |
| 3.2 | Reject a second proposal with a note. | The proposal is rejected; the note is a comment that mentions the proposing leader. | not verified |

## 4. A document saved, drafted, published and found

Prerequisite: knowledge is enabled for the workspace and WeKnora is healthy
(`curl localhost:8090/health`).

| # | Step | Expected result | Sign-off |
|---|---|---|---|
| 4.1 | On an issue or a brief, press **Save to knowledge**. | The document is published at once and appears in the Knowledge page with a link to its source. | not verified |
| 4.2 | Run `multica knowledge add --kb market --title "..." --file notes.md` from an agent task. | A draft appears under the Drafts tab of the Knowledge page and is not searchable. | not verified |
| 4.3 | Publish the draft from the Drafts tab. | It becomes searchable within about a minute. | not verified |
| 4.4 | Ask an agent run to find the document with `multica knowledge search "..."`. | The run cites the document by title and link. | not verified |

## Recording the result

Write the date, the observed result and the name of the person who checked it in the
Sign-off column of each row. If a step cannot be run, write the blocker in the
column (for example, "no Hermes model provider yet") and leave the row unsigned.
