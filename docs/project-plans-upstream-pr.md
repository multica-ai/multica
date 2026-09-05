# Project plans behind a feature flag

Project plans let a project decompose its work into **ordered phases and parts**,
with each part linking the implementation issues that cover it. They add three
view modes to project-scoped issue surfaces — **Document** (the authored plan:
phases, parts, and the rollup of linked issues), **Pipeline** (phases as
dependency-ordered stages), and **Coverage** (which parts have linked issues,
which don't) — alongside the existing board/list/table/gantt/swimlane modes.

Plans are versioned: creating one snapshots the source issue's title,
description, and revision; updating or superseding a plan keeps the old version
as `superseded` so retained versions render immutable number/title snapshots.
Deleting an issue nulls the plan's live pointer to it without deleting the plan
data.

## Flag-gated, off by default, fails closed

Every surface — route handlers, `/api/config` publication, CLI commands, and UI
view modes — sits behind a single `project_plans` flag:

- The default is **OFF**, declared once in the `flagDefaults` map in
  `server/internal/featureflags/keys.go`. The backend route gate
  (`ProjectPlansEnabled`) and the frontend publication
  (`EvaluateFrontendPublicFlags`) both read their default from that map, so a
  route can never serve a surface that `/api/config` reports as off, or vice
  versa.
- Operators enable it per deployment with `FF_PROJECT_PLANS=true` or a
  `project_plans` rule in `MULTICA_FEATURE_FLAGS_FILE`. No override → off.
- Fails closed everywhere: with the flag off (or no flag provider at all), read
  and write routes answer **404** — not 401/403/500 — the same 404 as "no
  active plan". Frontend call sites pass `false` as the `useFeatureEnabled`
  fallback, so no plan affordance can flash before config loads.
- The pattern matches `plugins_v1` and `composio_mcp_apps`: upstream ships the
  flag and its gates; when to roll it out is a maintainer/operator decision,
  not a code default.

## Migrations: 467–489

The schema is 23 up/down migration pairs numbered **467–489**:
`project_plan_kind`, `project_plan`, `project_plan_phase`,
`project_plan_part`, `project_plan_part_issue`,
`project_plan_dependency`, plus their keys and indexes. It is one schema —
the read API and the authoring flow are two halves of the same tables, so
they land together.

The block was originally authored starting at 446 in the tree this feature was
developed on. This PR targets `main`, whose migrations now run through 466, so
the block was renumbered en bloc to start after upstream's highest number
(466), preserving relative order. The concurrent-index-cleanup registry in
`server/cmd/migrate/main.go` was updated to match (22 keys; 467 creates tables
and has no index cleanup). The migrations lint test
(`TestMigrationFilesHaveMatchingDirections`) passes on the final numbering.

## How to try it

1. From `server/`: `go run ./cmd/migrate up`
2. Run the API with `FF_PROJECT_PLANS=true` (or a `project_plans` rule in
   `MULTICA_FEATURE_FLAGS_FILE`).
3. UI: open a project's issue surface — Document / Pipeline / Coverage appear
   in the view-mode switcher and can be saved as views. Authoring: create a
   plan (manually or by snapshotting a source issue), then add phases, parts,
   and issue links.
4. CLI: `multica project plan create-from-issue | add-phase | add-part |
   link-issue` (all accept `--output json|table`).

With the flag off, none of it exists: the plan modes are absent from the
switcher, the routes 404, and `multica project plan` refuses to run.

## What's in the diff

- **Server:** `internal/projectplan/` (service, read model, repository, error
  kinds), `handler/project_plan.go`, CLI `cmd_project_plan.go`, `project_plan`
  sqlc queries and generated code, plan tables in the workspace-delete cascade,
  issue-delete clearing the live plan pointer, the `project_plans` flag key.
- **Core package:** `project-plan/` module (queries, hooks, mutations,
  write-routes, error classification), API client read + write methods, the
  zod overview schema, plan types, `PROJECT_PLANS_FLAG`.
- **Views:** the three plan panes plus authoring dialogs, view-store and
  surface-controller wiring, plan entries in the view-mode switcher and
  save-view dialog, the project-detail entry point, locale keys for
  `en`/`ja`/`ko`/`zh-Hans`.
- **Agent skill:** `multica-project-plans`, which teaches agents the CLI
  surface.

## Testing

- Full Go suite against a fresh Postgres with all 489 migrations applied:
  65 packages ok (`bash scripts/test-go.sh --race` from the repo root —
  `go test -race` over every server package). Four `pkg/agent` cursor
  background-process tests fail in our CI host environment and fail
  identically on the unmodified upstream head, so they are unrelated to this
  change.
- Migrations lint: `go test ./internal/migrations/ -run TestMigration -v` →
  `TestMigrationNumericPrefixesAreUnique` and
  `TestMigrationFilesHaveMatchingDirections` PASS.
- Flag behavior: `TestProjectPlansEnvironmentStatesKeepBackendAndFrontendAligned`
  (unset / `false` / `true` → backend gate and frontend publication always
  agree), `TestPublishedFlagsDefaultOff`, `TestGetConfigExposesFrontendFeatureFlags`
  (published and false by default), `TestGetConfigPublishesProjectPlansKillSwitch`,
  plus fail-closed service/handler tests (no flag provider → 404).
- Frontend: `pnpm install --frozen-lockfile`, `pnpm build`, `pnpm typecheck`,
  and `pnpm lint` all pass. Test suites: core 1759/1768, views 5073/5076,
  desktop 656/656, web 266/266, docs 15/15. The 9 failing core tests
  (localStorage-backed stores) and the 3 failing views tests (billing-tab
  subscription seat facts) fail identically on unmodified upstream `main` on
  the same host, so they pre-date this change.
- Plan-specific suites all pass: core `project-plan/` module (22), API client
  (113), views plan panes (45).
