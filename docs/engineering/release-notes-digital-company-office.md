# Release and upgrade notes: digital company office (phases 1 to 4)

Status: draft for the release that carries WS12 of the
[plan](digital-company-office-plan.md). The version number is **not** bumped here. The bump
is an outward-facing step and needs a decision; see [Version](#version).

Last tag: `v0.7.2`. Branch: `feat/digital-company-office-phase4`. `main` is at `746702efa`
(phase 3 merged). Phase 4 is uncommitted in the working tree and is included below.

## What is new

- **Phase 2**: squad charters and routines, the operating review, routine compliance,
  proposal revision and snooze, accounted inquiries, adaptive attention, brief push to
  Telegram, Slack and Lark, brief feedback and catch-up.
- **Phase 3**: pipeline and initiatives (seven stages), the proposal kinds `opportunity`
  and `stage_gate`, the knowledge taxonomy with 26 kinds and the Librarian, the starter
  company wizard, the Executive Office setup.
- **Phase 4**: goals, key results and metrics (with the webhook), budgets and the budget
  gate in the claim path, autonomy levels L0 to L3, standing approvals, company controls
  (pause, quiet hours) and the governance log, scorecards, reviews, eight organisation
  change types with probation and rollback, floor chiefs, the pulse strip, company export
  and import.

The user-facing pages are in the docs site (`apps/docs/content/docs`), in five locales.

## New tables

Migrations 730 to 899 on this branch: 101 `up` and 101 `down` files. Every `up` file has a
`down` file. No migration adds a foreign key, a cascade, or a non-concurrent index.
The count by origin:

- 43 are already on `main` (730 to 773, phases 2 and 3).
- 58 are uncommitted (780 to 842, and 880 and 881).

**New tables (31)**, with the migration that creates each:

| Table | Migration | Area |
| --- | --- | --- |
| `brief_feedback` | 730 | Brief ratings (phase 2) |
| `pipeline` | 737 | Pipeline stages |
| `initiative` | 740 | Initiatives |
| `initiative_stage_event` | 745 | Stage timeline |
| `initiative_artifact` | 749 | Stage artifacts |
| `knowledge_kind` | 751 | Knowledge kinds |
| `knowledge_publish_policy` | 751 | Publish policies |
| `knowledge_eval_case` | 751 | Retrieval eval cases |
| `knowledge_eval_run` | 751 | Retrieval eval runs |
| `knowledge_health_run` | 751 | Librarian routine runs |
| `knowledge_acl` | 761 | Per-base access grants |
| `knowledge_tenant_deprovision` | 764 | Seven-day deprovision hold |
| `knowledge_owner_account` | 767 | Per-owner WeKnora login |
| `knowledge_repo_sync` | 771 | Repo docs sync |
| `knowledge_repo_file` | 771 | Repo docs sync files |
| `governance_event` | 780 | Governance log (append only) |
| `metric` | 783 | Metric definitions |
| `metric_reading` | 785 | Metric readings |
| `goal` | 788 | Goals |
| `key_result` | 792 | Key results |
| `planning_cycle` | 796 | Planning cycle per squad and period |
| `budget` | 800 | Budgets |
| `budget_period` | 800 | Budget periods and spend |
| `budget_hold` | 800 | Holds on tasks |
| `approval_policy` | 811 | Standing approval policies |
| `scorecard` | 820 | Scorecards |
| `review` | 823 | Reviews |
| `agent_instruction_revision` | 827 | Instruction history |
| `org_change_application` | 830 | Applied org changes and probation |
| `retro_trigger` | 835 | Retrospective triggers |
| `floor_chief` | 837 | Floor chief per floor |

**Changed existing tables**: `autopilot_run` (735, 809: catch-up source, deferred status),
`proposal` (736, 813: kind and payload, policy decision, estimated cost, reversal),
`issue` (736, 880, 881: kind check, `done_at`, `reopen_count`, `last_reopened_at`, and a
trigger that keeps them current), `knowledge_link` (751, 757, 758, 770: taxonomy
columns, WeKnora and supersession indexes, review issue), `knowledge_base` (760: wiki and
summary flags, LLM defaults), `initiative_stage_event` (769: kinds), `workspace` (782:
company state and quiet hours; 842: digest mode), `agent_task_queue` (800: budget
accounted cost), `squad` (810: autonomy level), `brief` (840: scope type and id).

**Migration list** (up files, in order): 730 to 736, 737 to 750, 751 to 758, 760 to 773,
780 to 798, 800 to 811, 813, 814, 820 to 833, 835 to 842, 880, 881.

Two notes on the list:

- 880 and 881 are not in the plan's number ranges (phase 4 is 780 to 849). They add the
  delivery history that the built-in metrics `delivery.cycle_time_days` and
  `delivery.rework_rate` and the scorecards read. Issues done before 880 have no `done_at`
  and no reopen history; readers fall back to `updated_at`.
- Migration 782 and 842 change `workspace`. Migration 880 adds columns to `issue`, which is
  the largest existing table. Check the lock behaviour of `ALTER TABLE` on your database
  size before you deploy.

## Environment variables

**Since `main`** (the uncommitted phase 4 work): no new environment variable is read by the
server. A diff of every `os.Getenv` and `LookupEnv` call against `HEAD` shows none added.
The working tree removes one read, `COMPOSIO_WEBHOOK_SECRET`. That removal is not part of
the office work; check it before the release.

**Since `v0.7.2`** (the last tag): the company knowledge base adds the variables below.
They are read in `server/cmd/server/knowledge.go`. When `KNOWLEDGE_URL` is empty, nothing
changes and knowledge is not offered.

| Variable | Default | Purpose |
| --- | --- | --- |
| `KNOWLEDGE_URL` | unset | Backend address of WeKnora. Unset turns the feature off. |
| `MULTICA_KNOWLEDGE_SECRET_KEY` | unset | Base64 32-byte key. Encrypts each workspace's WeKnora key. Without it knowledge is not offered. |
| `KNOWLEDGE_SERVICE_EMAIL`, `KNOWLEDGE_SERVICE_PASSWORD` | unset | The WeKnora service account that provisions each workspace |
| `KNOWLEDGE_PUBLIC_URL` | `KNOWLEDGE_URL` | Address the daemons use for the read-only MCP endpoint |
| `KNOWLEDGE_UI_URL` | unset | WeKnora web interface for the **Open in WeKnora** link |
| `KNOWLEDGE_EMBEDDING_PROVIDER` | `ollama` | Embedding provider |
| `KNOWLEDGE_EMBEDDING_MODEL` | `bge-m3` | Embedding model. Fixed per knowledge base at creation. |
| `KNOWLEDGE_EMBEDDING_URL` | `http://host.docker.internal:11434` | Embedding endpoint |
| `KNOWLEDGE_EMBEDDING_API_KEY` | unset | Embedding endpoint key |
| `KNOWLEDGE_EMBEDDING_DIMENSION` | `1024` | Vector size. An invalid value is ignored with a warning. |
| `KNOWLEDGE_LLM_BASE_URL` | unset | Chat endpoint. Unset means no model is registered. |
| `KNOWLEDGE_LLM_API_KEY` | unset | Chat endpoint key |
| `KNOWLEDGE_LLM_MODEL` | unset | Chat model for summaries and Wiki |
| `KNOWLEDGE_RERANK_MODEL` | unset | Reranking model, optional |
| `KNOWLEDGE_VISION_MODEL` | unset | Vision model, optional. Registered but not tested at setup. |
| `KNOWLEDGE_UPLOAD_MAX_MB` | `50` | Upload size cap. Keep it at or below WeKnora's `MAX_FILE_SIZE_MB`. |

The WeKnora containers read their own variables (`KNOWLEDGE_SYSTEM_AES_KEY`,
`KNOWLEDGE_JWT_SECRET`, `KNOWLEDGE_SSRF_WHITELIST_EXTRA`, `KNOWLEDGE_DISABLE_REGISTRATION`,
and the port settings). They are in `SELF_HOSTING.md` and `SELF_HOSTING_ADVANCED.md`.

**Documentation gap**: `apps/docs/content/docs/environment-variables.mdx` lists only
`KNOWLEDGE_URL`, `MULTICA_KNOWLEDGE_SECRET_KEY`, the service account, `KNOWLEDGE_PUBLIC_URL`
and `KNOWLEDGE_UI_URL`. The embedding, LLM, rerank, vision and upload variables above are not
in that page yet. Add them before the release.

## Optional components

- **WeKnora** (knowledge base). Optional sidecar, started with the `knowledge` compose
  profile. Multica runs without it. Its image is pinned to `v0.9.0` through
  `KNOWLEDGE_VERSION`. Back up its two volumes together with `KNOWLEDGE_SYSTEM_AES_KEY` and
  `MULTICA_KNOWLEDGE_SECRET_KEY`.
- **Hermes** (Secretary runtime). Optional. Setup needs an online Hermes runtime. A model
  provider is configured in Hermes itself. Without Hermes there is no Secretary, no brief,
  and no brief push.
- **Telegram**: `MULTICA_TELEGRAM_SECRET_KEY`. Chat app, and brief push with decision buttons.
- **Slack**: `MULTICA_SLACK_SECRET_KEY`. Chat app, and brief push with decision buttons.
- **Lark (Feishu)**: `MULTICA_LARK_SECRET_KEY`. Chat app, and brief push with decision buttons.
- **DingTalk**: `MULTICA_DINGTALK_SECRET_KEY`. Chat replies only. No brief push and no buttons.
- **WeCom**: `MULTICA_WECOM_SECRET_KEY`, and optionally `MULTICA_WECOM_DEFAULT_LOCALE`,
  `MULTICA_WECOM_MEDIA_ALLOW_CIDRS`, `MULTICA_WECOM_TRACE`, `WECOM_APP_URL`. Chat replies
  only. Run one backend replica, or a sharded realtime relay (`REDIS_URL`); the server logs
  this warning at startup. No brief push and no buttons.

Brief push, the decision buttons and the `/brief` and `/decisions` commands work only in
Telegram, Slack and Lark.

## Upgrade steps

1. Back up the database and, if used, the WeKnora volumes and both encryption keys.
2. Deploy the migrations. The runner applies each `CREATE INDEX CONCURRENTLY` in its own file.
   A failed concurrent index can leave an `INVALID` index. Check with
   `SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;` and drop and recreate it.
3. If you build from source, run `make sqlc` after any SQL change. The generated files for this work are in the working tree, not yet committed.
4. Set the knowledge variables only if you use WeKnora. Leave `KNOWLEDGE_URL` unset otherwise.
5. Existing squads start at autonomy level 1, with no charter and no routines. Routines are
   created by saving a charter or by the starter company.
6. Set up the Secretary from the Office setup card, or with `POST /api/office/setup`. It is
   idempotent.
7. Chat apps: enable brief push per member in the Office settings. Telegram, Slack and Lark
   need their own connection first.

Rollback: each `down` file drops the tables or columns its `up` file added. Rolling back
`780` (`governance_event`) removes the governance log with its data, so export the log
(CSV) first. The other new tables, such as `budget_hold` and `org_change_application`, also
lose their rows on rollback.

## Limits that carry into this release

- Company import does not carry squad-scoped or initiative-scoped goals, budgets, revoked
  policies, or archived squads. Imported routines are created paused.
- Hire, retire and merge organisation changes cannot be reverted, so a worse probation
  verdict for them files no rollback proposal.
- Webhook-triggered routine runs skip the company-state check at admission and are held at
  claim. The run list shows `issue_created` during a pause.
- The plan's deviation list (`digital-company-office.md`, "Phase 4 deviations") says
  `delivery.cycle_time_days` and `delivery.rework_rate` are not built. The code now computes
  them, from migrations 880 and 881. That deviation entry is out of date.
- Not verified by this document: the Playwright suite for phase 4 (plan 12.2), the performance
  checks (12.4), the backup drill (12.3), and any live Hermes, Telegram, WeKnora or LLM run.

## Version

The version is not bumped in this document. `.github/RELEASING.md` and `AGENTS.md` set a
patch bump by default (from `v0.7.2`). A minor or major bump is a separate decision for the
owner. Run the release checklist in `RELEASING.md` when that decision is made.
