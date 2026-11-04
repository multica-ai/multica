# Phase 3 runbook: the boss's machine

These steps cannot run in CI. They need Hermes, the WeKnora sidecar with a model
endpoint, and the machine the boss works on. Spec 3.8 and plan 10.20 are the
reference. Each step lists its expected result. No step has been run yet, so every
row starts as **not verified**. The boss signs off each one by writing the date and
the observed result in the Sign-off column, or by recording why it could not be done.

The API part of 3.8 is automated in `e2e/phase3-company.spec.ts` and
`e2e/phase3-pipeline-ui.spec.ts`. The steps below cover what those tests skip.

## Prerequisites

- Phase 2 runbook section 1 is done: Hermes is online through the daemon.
- For sections 3 to 6: the WeKnora sidecar runs, and the deployment's
  `KNOWLEDGE_LLM_BASE_URL`, `KNOWLEDGE_LLM_API_KEY`, `KNOWLEDGE_LLM_MODEL` and
  `KNOWLEDGE_RERANK_MODEL` point at the 9router endpoint. Its host is in the SSRF
  allow-list. Only the boss can supply the endpoint and the key.

## 1. Starter company on a clean workspace

Use a new workspace with no floors or squads, where the boss is an owner.

| # | Step | Expected result | Sign-off |
|---|---|---|---|
| 1.1 | Create a new workspace and open the Office. | The Office is empty, and "Build your company" is offered. | not verified |
| 1.2 | Open "Set up your company" and let it finish. | The checks show Hermes runtime online, Secretary, Executive floor, Daily brief as done, and Weekly review paused. | not verified |
| 1.3 | Choose "Build your company". Select all squads, choose the worker runtime and an optional model, choose "Leave paused", read the estimate, and press "Build company". | The steps show Created. The floors are Executive, Research, Product, Build, Launch and Governance. Fourteen squads sit on their floors. | not verified |
| 1.4 | Open each squad's Charter tab. | Every squad has a mission. | not verified |
| 1.5 | Open the routines of each squad. | Only the Market Research "Market scan" and the Secretary's "Daily brief" run. Every other routine shows paused. | not verified |
| 1.6 | Open /pipeline. | Seven columns: Validate, Define, Plan, Build, Launch, Operate and Learn. No stage is reported as unowned. | not verified |
| 1.7 | Open Knowledge and its kinds. | The 27 seeded kinds are listed, such as validation_report and retrospective. The ventures base is added when WeKnora is ready. | not verified |
| 1.8 | Press "Build company" again. | Every step shows "Already there", apart from the ventures step while WeKnora is not ready. Floors, squads, routines and the pipeline are unchanged. | not verified |
| 1.9 | If a step ever fails ("Finished with problems"), press "Run again". | The failed step completes, and nothing that existed is changed. | not verified |

## 2. The Secretary's daily brief

| # | Step | Expected result | Sign-off |
|---|---|---|---|
| 2.1 | In the Office, check the daily brief autopilot. | It is set for 08:00 local time and is active. | not verified |
| 2.2 | Press Generate now on the brief card. | The brief has its sections in spec order: Headline, Needs your decision, Progress, Pipeline (only when initiatives exist), Blockers and risks, Watch, People (when there is data), and FYI. Goals and Budget do not appear before phase 4. | not verified |
| 2.3 | After section 6 has an open gate, press Generate now again. | The Pipeline section lists that initiative with its stage, days in stage, artifacts such as 1/2, and the pending gate. | not verified |

## 3. WeKnora with the LLM (live check of plan 10.11)

| # | Step | Expected result | Sign-off |
|---|---|---|---|
| 3.1 | As an owner or admin, press Enable knowledge on the Knowledge page. | Knowledge is enabled and reports ready. | not verified |
| 3.2 | Open Models and press Test models. | Each configured chat and rerank model shows Working, and any vision model too. Nothing shows Failed. | not verified |
| 3.3 | Set `KNOWLEDGE_LLM_MODEL` to a name the endpoint does not serve, restart, and test again. Then restore it. | The test marks the chat model Failed and gives the reason. Only that model fails. | not verified |
| 3.4 | Open the bases and check Wiki and Summaries. | Wiki is on for company, ventures and decisions, and no other base has Wiki. Summaries are on. | not verified |
| 3.5 | Upload a short market note (a few paragraphs, with the headings the kind requires) with kind market_report into the ventures base. | The upload reads "is in the knowledge base". A Wiki page and a summary appear for the document. The 9router endpoint logs show requests for the chat model. | not verified |
| 3.6 | Search for a phrase from the note. | The document is returned with its kind and source, and its Wiki page is linked. | not verified |
| 3.7 | Add a Git repository with a `docs` folder over https (not private) under Knowledge, Sources, Repository docs, turn the sync on, and press Sync now. | Each Markdown file with a Summary heading appears in the engineering base as a reference document. A file without the heading is listed under the form as failed. Changing a file and syncing again replaces it. | not verified |
| 3.8 | Remove one of the synced files from the repository, push, and press Sync now. | The document leaves the engineering base and its sync record reads archived. | not verified |

### Live check, 2026-11-04 (covers 10.11, 10.12 and 10.14)

An agent run on the development machine, not a sign-off. No row above was
signed off, and every row stays **not verified**. The test workspace was
deleted afterwards.

- Router, checked directly: `VSF_Planner_Reviewer[1m]` answered a chat request
  and accepted an image input. The router has no rerank model and no `/v1/rerank`
  endpoint (404), so `KNOWLEDGE_RERANK_MODEL` is unset.
- WeKnora `GET /health` on port 8090 answered `ok`. The local embedding service
  (`bge-m3` on 172.17.0.1:11434) returned 1024-dimension vectors.
- 3.1 (enable) failed. `POST /api/knowledge/enable` returned 502 "the knowledge
  service refused to create another workspace". WeKnora answered HTTP 429 "reached
  self-service workspace quota". The service account owns 10 tenants. Nine belong to
  deleted workspaces and are held in `knowledge_tenant_deprovision` until
  2026-11-10, so they still count against the quota.
- 3.2 (models test) returned 409 "knowledge is not ready for this workspace"
  (state `disabled`). Because of this, 3.3 to 3.6 and 4.2 to 4.5 were not run.
  No Wiki page, summary or search result was produced.
- 10.12 (repo-docs sync, 3.7 and 3.8) and 10.14 (deprovisioning and the owner
  account) were not run. Both need an enabled base.
- Not verified: whether WeKnora accepts `10.70.39.196` as a model endpoint. Its
  `SSRF_WHITELIST_EXTRA` lists only `172.17.0.1`, so the registration may be refused.

To unblock: raise `WEKNORA_TENANT_MAX_OWNED_PER_USER` for WeKnora, or remove the
held tenants, then rerun 3.1 onward.

### Live check, 2026-11-04 (rerun after the quota and gateway fixes)

An agent run on the development machine, not a sign-off. No row above was
signed off, and every row stays **not verified**. The test workspace was
deleted afterwards.

Settings changed:

- WeKnora `WEKNORA_TENANT_MAX_OWNED_PER_USER` set to `1000` in the WeKnora
  compose `.env` (no `system_settings` row overrode it). `WeKnora-app` was
  recreated with `docker compose up -d --no-deps app`. Health on 8090 is `ok`.
  The other containers were not touched.
- Multica `KNOWLEDGE_LLM_BASE_URL` set to `http://172.17.0.1:20128/v1`, the
  gateway as seen from the container. `SSRF_WHITELIST_EXTRA` already lists
  `172.17.0.1`. WeKnora refused `10.70.39.196` with "direct IP address access is
  not allowed", so the allow-list was not widened.

Junk tenants removed (owned by deleted workspaces, deprovision rows `held`):
WeKnora tenant ids 10009 to 10017. Their workspace rows are gone from Multica.
WeKnora's `DELETE /tenants/{id}` answered 200, but those tenants were already
soft-deleted (`deleted_at` set on 2026-11-03), so the call changed nothing.
Their owner memberships still counted against the quota. The cap change
removed that block. The Multica `knowledge_tenant_deprovision` rows were left
as they are, so the hourly job will finish them after the hold (2026-11-10).
Tenants 10007 and 10008 belong to the service account, have no deprovision
row, and were not touched.

- 3.1 (enable) passed. Knowledge reported `ready` with six bases.
- 3.2 (models test) returned `chat` ok, `vision` ok. `rerank` was not
  checked because no rerank model is configured. The embedding model
  (`bge-m3`) is not probed by this test. It was registered at enable, and
  the uploaded chunks were embedded and indexed.
- 3.4 (Wiki and summaries) was set on `ventures` only, for this test.
  `company` and `decisions` were not set.
- 3.5 (upload) passed. `POST /api/knowledge/documents/upload` with kind
  `market_report` in `ventures` returned 201 and `published`. A summary and a
  Wiki set (three concept and entity pages plus one summary page) were written
  for the document.
- 3.6 (search) passed. The phrase returned the document with kind
  `market_report`. The Wiki link was not checked.
- 3.3, 3.7, 3.8, 10.14 and section 4 were not run.

Defects found and fixed in Multica. The summary and Wiki calls failed with
"decode response: invalid character 'd'". WeKnora omits `stream` on a
non-streaming call, and the gateway then answers with `text/event-stream`. The
chat and vision models are now registered with
`spec.compat.extra_body.stream = false`
(`server/internal/service/knowledge_provision.go`,
`server/internal/knowledge/client.go`, regression test
`TestEnsureModelSendsExtraBodyInCompatSpec`). A model created before this
change keeps its old parameters, so it needs the same compat `extra_body` set
in WeKnora before its Wiki and summaries will work.

Open, not fixed: WeKnora's keyword retrieval fails for this query shape with
"Unsupported query shape" (paradedb). The vector hit still returns. The cause is
in WeKnora, not in Multica.

Test workspaces (two were created and both deleted through the API). Their
WeKnora tenants 10018 and 10019 are now `held` in Multica until 2026-11-11, which
is the normal seven-day hold.

## 4. Uploads, headings and supersession

| # | Step | Expected result | Sign-off |
|---|---|---|---|
| 4.1 | On the Knowledge page, drop a file in "Add a file" and leave Kind empty. | The upload is refused. A kind is required. | not verified |
| 4.2 | Upload a file with kind validation_report and its three headings (Question, Findings, Recommendation). | The upload is published at once, and the file is searchable with its kind. | not verified |
| 4.3 | Run `multica knowledge add --kind validation_report` with a file that lacks Findings. | The command is refused, and it names Findings as the missing heading. | not verified |
| 4.4 | Write a corrected validation_report with `multica knowledge add --supersedes <id>`, where the id is the document from 4.2, then publish it with `multica knowledge publish <id>`. | The new document is published. Search no longer returns the old one. | not verified |
| 4.5 | Add an eval case that expects a document id that does not exist, then run `multica knowledge eval run`. | The hit rate is lower than the earlier run, and the digest reports it. | not verified |

## 5. A restricted base, seen from inside and outside its list

Use a base that only the squads you name may read, such as ventures. Set its
visibility to Restricted on the Bases page.

| # | Step | Expected result | Sign-off |
|---|---|---|---|
| 5.1 | Under Access, choose Grant access for Market Research (Squad) with Read. | The access list shows Market Research with Read. | not verified |
| 5.2 | Ask the Market Research leader, in an issue, to list the knowledge bases and search the restricted base. | The answer names the base and cites a document from it. | not verified |
| 5.3 | Ask the Growth & GTM leader the same thing. | The answer does not name the base, and no document from it is returned. | not verified |
| 5.4 | Remove Market Research's access. Ask its leader again. | The base is no longer named, as in 5.3. | not verified |

## 6. The pipeline with a real gate

Validate needs two published documents (validation_report and competitor_profile),
so run section 3 first. The gate is filed by the initiative sweep, which runs once an
hour. Check the scheduler job `initiative_sweep` if it has not appeared after an hour.

| # | Step | Expected result | Sign-off |
|---|---|---|---|
| 6.1 | Have Opportunity Lab file an opportunity: `multica proposal create --kind opportunity --thesis "..." --market "..."`, with a knowledge document id from section 3 as `--evidence-doc`. Approve it in Decisions. | An initiative appears on /pipeline in Validate, owned by Product Discovery, with 0/2 artifacts. | not verified |
| 6.2 | Have the Product Discovery leader publish a validation_report and a competitor_profile, each with the initiative attached. | The card shows 2/2 artifacts, and the panel shows both as Published. | not verified |
| 6.3 | Wait for the sweep. Open Decisions. | A stage gate for the move from Validate to Define is waiting. The panel shows "Waiting for a decision". | not verified |
| 6.4 | Press Generate now on the brief. | The Pipeline section lists the gate. | not verified |
| 6.5 | Approve the gate in Decisions. | The panel shows the initiative in Define. The backing issue has moved, Product Analysis's leader has been woken on it, and the timeline shows the advance from Validate to Define. | not verified |
| 6.6 | Reject a later gate with a note. | The initiative stays at its stage, the leader is woken, and the timeline records the return. The gate shows Rejected. | not verified |
| 6.7 | As an admin, open an initiative's panel and try Force move with no reason. Then give a reason and move it to another stage. | No reason is refused. The move happens and the timeline records Forced with the reason. | not verified |
| 6.8 | Try to drag a card on the board. | Cards do not move by drag. The stage changes only through the server. | not verified |

## Sign-off

- Boss: ____________________  Date: ____________
- Notes on any step not run, with the reason:
