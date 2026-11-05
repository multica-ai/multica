# Contribution validation

Base: `multica-ai/multica` main, `98846ce56`.
Date: 2026-11-05.

## Executed

- Built the API and CLI with `go build ./cmd/server ./cmd/multica`.
- Regenerated SQL with `make sqlc`.
- Reset an isolated managed development database and applied all migrations,
  including 615–628, from an empty database.
- Passed the migration runner's concurrent-index cleanup contract checks.
- Passed `pnpm typecheck` across all nine configured workspaces.
- Passed `pnpm lint` across all six configured workspaces (existing warnings
  remain); targeted CodeGraph UI lint completed without warnings.
- Passed 14 core tests covering API parsing and assignment changes.
- Passed CodeGraph parsing, path restrictions, document indexing, resource
  limits, and active-time-budget checks.
- Passed five database-backed handler checks: duplicate registration,
  assignment persistence, cross-workspace assignment rejection, missing
  checkout reconciliation, and MCP assignment revocation.
- Passed daemon MCP configuration checks.

The database-backed MCP check uses source-file fixtures to isolate authorization.
It does not execute an AI provider and is not evidence of model behavior.

## Real services

The API, PostgreSQL, browser application and Docker engine ran in an isolated
managed environment. No production service was changed.

Built these public Git repositories through the real Git/engine pipeline:

| Repository | Commit | Result |
| --- | --- | --- |
| `multica-ai/multica-cli` | `f391633862eed012760bb905d8c6d6df17f0e484` | Ready |
| `expressjs/serve-static` | `6e3606f2892df213e8fa738b51a36ba711e86ce8` | Ready; 41 symbols |

Verified HTTP symbol search, callers, callees, impact, document search and source
reads against successful snapshots. Query results remained pinned to one build
and commit. Cancelling a queued rebuild preserved the previous successful
snapshot.

Verified separate authenticated owner, administrator, member and nonmember
identities: members can list/query; only owners and administrators can manage
credentials and verify repository access; nonmembers receive 404. Path escapes,
Git metadata reads and unsupported operations were rejected.

Anonymous queries returned 404 while disabled, 200 after explicit activation,
then 404 again after revocation. Sharing was left disabled.
Skill export handlers and UI controls were removed.
Browser interaction verified the repository details and symbol-search result
view against the real API. No browser console errors occurred on that page.

## Remaining checks before an upstream PR

- Full `make check` and the complete repository test suites have not run.
- A model-backed agent run through the rebuilt CLI has not run. The HTTP/MCP
  authorization and configuration checks do not establish end-to-end provider use.
- Private remote Git token authentication and multi-replica restart/recovery
  have not been validated in this contribution environment.
- Desktop typechecking passed; native desktop interaction was not tested.
- English and Simplified Chinese guides were added. New strings in other
  supported locales include English fallback copy and need language review.
