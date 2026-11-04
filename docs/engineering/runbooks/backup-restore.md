# Backup and restore (plan 12.3)

This runbook says what to back up for the digital company office, in what order to
restore it, how to export and import the company's structure, and records the drill
run on 2026-11-03.

## What holds the company

Two stores hold everything that is not code or an image.

| Store | Location | What it holds |
|---|---|---|
| Multica Postgres | Compose service `postgres` (container `multica-postgres-1`, volume `multica_pgdata`), database `POSTGRES_DB` from the env file (`multica` for the main checkout, `multica_<...>` for a worktree) | Every workspace row, the office, the pipeline, governance, budgets, briefs, knowledge metadata |
| WeKnora Postgres | Volume `weknora_postgres-data` (the self-host compose calls it `knowledge-postgres-data`) | Knowledge base metadata and chunks in the WeKnora database |
| WeKnora files | Volume `weknora_data-files` (self-host name `knowledge-data-files`, mounted at `/data/files`) | Uploaded and parsed documents the knowledge base points at |

`weknora_docreader-tmp` is scratch space and is not backed up.

Redis and the WeKnora frontend hold no state that must survive a restore.

## Postgres tables that matter

The list comes from the migrations and from the workspace deletion manifest
(`server/internal/handler/workspace_delete_manifest_test.go`, which lists each table a
workspace owns; the phase 3 and 4 plan rule says every new table is added there).

Core workspace and people: `workspace` (including `company_state`,
`company_state_updated_at`, `secretary_agent_id`, `digest_mode`), `member`, `user`,
`agent`, `agent_runtime`, `agent_skill`, `agent_mcp_server`, `agent_instruction_revision`.

Work: `issue`, `comment`, `project`, `issue_workflow`, `issue_status`, `agent_task_queue`,
`issue_wakeup`, `inbox_item`, `activity_log`.

Office: `company_floor`, `squad`, `squad_member`, `squad_routine`, `squad_charter`,
`autopilot`, `autopilot_trigger`, `autopilot_run`, `floor_chief`.

Phase 2: `brief`, `brief_delivery`, `brief_feedback`, `brief_inquiry`,
`brief_push_preference`, `channel_installation`, `channel_user_binding`,
`channel_outbound_message`, `chat_session`.

Phase 3: `proposal` (kinds and payloads), `pipeline`, `initiative`,
`initiative_artifact`, `initiative_stage_event`, `knowledge_base`, `knowledge_link`,
`knowledge_publish_policy`, `knowledge_kind`, `knowledge_eval_case`, `knowledge_eval_run`.

Phase 4: `goal`, `key_result`, `metric`, `metric_reading`, `budget`, `budget_period`,
`budget_hold`, `approval_policy`, `scorecard`, `org_change_application`,
`retro_trigger`, `governance_event` (append-only, no delete path, removed only by
workspace teardown), `maintenance_job`.

Migration bookkeeping: `schema_migrations`. Restoring it tells you which migrations the
dump has; compare the head with the code (`ls server/migrations | tail`).

Do not add foreign keys or cascades when you add a table (repository rule); a restore
therefore relies on the application's own dependency checks.

## Restore order

Restore in this order. Stop writers before step 1.

1. Stop the app and the daemon: `make down` in the checkout (keeps the data). Stop
   WeKnora. This keeps the copies consistent.
2. Restore the multica database from a dump taken with the server's own `pg_dump`
   (`docker compose exec -T postgres pg_dump -U <user> -Fc <db>`). The host `pg_dump`
   may be an older major version and refuse the server (it did in the drill, see below).
3. Restore the WeKnora Postgres volume, then `weknora_data-files`. Start WeKnora and
   check it answers (`knowledge-search` on a known document).
4. Check the migration head: `make migrate-up ENV_FILE=<env file>` applies only the
   migrations the dump does not have. Confirm `schema_migrations` head equals the
   code's head.
5. Start the app: `make up`. Check `make status`.
6. Verify: open the Office, check the pulse strip, `multica brief digest --since 24h
   --json` returns the squads, and the knowledge search answers.

## Company export and import (structure only)

The export carries the company's structure: floors and floor chiefs, squads and seats,
agents and their instructions, charters, routines, pipelines and their stages, goals,
budgets, approval policies and knowledge kinds. It does not carry initiative records,
progress, spend or secrets (spec 4.10 deviations). Goals and budgets scoped to a
squad or an initiative do not round-trip (same deviations list).

```sh
multica company export --file company.json          # owners and admins
multica company import --file company.json --runtime <runtime-id> --dry-run
multica company import --file company.json --runtime <runtime-id>
```

Imported agents run on the `--runtime` given; without it they are reported as blocked.
Imported routines are created paused. Running the import twice changes nothing.

Use this to rebuild the structure in a clean workspace when a database restore is not
possible. It is not a replacement for step 2.

## Drill record (2026-11-03)

The drill ran against the worktree database `multica_multica_51` on this host. It
created a copy database, restored into it, compared it with the source, and dropped the
copy. The main checkout's `.env` database was not touched.

| Step | Command | Time | Result |
|---|---|---|---|
| Create copy | `docker compose exec -T postgres psql -U multica -d postgres -c 'CREATE DATABASE "multica_drill_copy"'` | 0.7 s | Created |
| Dump | `docker compose exec -T postgres pg_dump -U multica -Fc -f /tmp/drill.dump multica_multica_51` | 1.5 s | Exit 0 |
| Restore | `docker compose exec -T postgres pg_restore -U multica -d multica_drill_copy --no-owner --no-privileges -j 4 /tmp/drill.dump` | 4.2 s | Exit 0, no stderr |
| Compare | row counts of 34 tables; `schema_migrations` head | about 1 min | Head 792 in both; 31 tables equal |
| Drop copy | `scripts/drop-database.sh <scratch env file>` with `POSTGRES_DB=multica_drill_copy` | 3.2 s | Dropped |

Wall time for the restore path (dump, restore, drop), not counting the comparison, was
about 9 seconds for a database of this size.

Findings from the drill:

- The host `pg_dump` (16) refused the server (17): "server version mismatch". Use the
  server's binaries in the container, as the drill did.
- The source was live. Three tables differed because rows were written after the dump:
  `governance_event` (656 source, 649 copy), `approval_policy` (97 vs 92) and `proposal`
  (276 vs 268). The writers were the other agents' test suites and the company-day run.
  The drill therefore did not quiesce the source. For an exact comparison run `make down`
  before the dump, then compare.
- The drill copy's head was `882_proposal_tags`. Migration 900 (the claim gate index,
  see `performance.md`) was applied to the source after the dump, so a restore of this
  dump needs `make migrate-up` (step 4) to pick it up.
- WeKnora volumes were not restored in this drill. Restoring them is covered by steps 3
  and the checks above, and was not run here.

Open for a full drill: stop the app, repeat the dump, restore into a copy, run the
counts with zero mismatches, and restore the WeKnora volumes into a scratch
compose project.
