# Contribution validation

Initial base: `multica-ai/multica` main, `98846ce56`.
Synchronized base: `61e7e8f96` on 2026-11-06.
Initial verification: 2026-11-05.

## Usage scopes and contribution review, 2026-11-06

### Usage-first interface follow-up

The introduction now uses actual cropped registration, scope and symbol-result
captures, with English/Chinese and light/dark variants (12 PNGs, about 226 KiB).
The source is the public `expressjs/serve-static` review repository; captures do
not include account/navigation/host details or credential values. PNG metadata
contains no text/EXIF chunks. Scope thumbnails rearrange crops for readability;
the accessible image dialog shows the original capture. Browser checks loaded
and enlarged all three slides in both themes, verified Escape focus restoration,
and checked a 420px image dialog without horizontal overflow. Shared package
typechecks and targeted lint passed; native desktop interaction remains untested.

First-import credential discovery was also checked against the real API/browser:
the empty welcome page has credential management, and the registration form has
an inline manager next to the credential selector. Closing the nested manager
preserved the repository URL/branch and restored focus to its trigger. The local
environment's missing encryption key remained explicit and Token saves stayed
disabled. No encryption configuration or actual private Git credential changed.
Typechecks, targeted lint and MDX generation were repeated for this entry fix.

Repository details now lead to Configure access. The three scope cards are
parallel on desktop and stacked on narrow screens; agent/project selection uses
the same select-and-save editor. Query checks and build diagnostics are collapsed
under Advanced. Source reading is named by its purpose, with the 100-line limit
explained beside the control.

Repeated nine workspace typechecks, 31 pure core checks, targeted CodeGraph UI
lint and documentation MDX generation. Real-browser checks against the local API
covered project save, cancel preserving the server selection, project removal,
workspace-sharing enable/disable, Escape/focus restoration and diagnostic symbol
search against the existing engine snapshot. At 420px the dialog measured 388px,
had no horizontal overflow and kept the footer inside the viewport. The desktop
cards shared the same row. Temporary project data was removed afterward.

No backend schema or permission behavior changed in this follow-up. The real
database/MCP checks below are earlier evidence, not a newly repeated backend
suite. Model-backed execution and native desktop interaction remain unverified.

- Applied incremental migrations 629–630 to the isolated managed database,
  preserving existing repositories and direct assignments. Regenerated sqlc.
- Passed seven real database/MCP handler checks, including agent-owner binding,
  union/provenance, workspace sharing, task-derived issue/project-chat access,
  project moves, cross-workspace rejection and revocation during an MCP session.
- Repeated API/CLI compilation, all nine workspace typechecks, concurrent-index
  cleanup checks, 31 pure core checks and targeted CodeGraph UI lint.
- Generated the documentation MDX successfully. Full docs production build was
  not run.
- Real HTTP scope writes returned 204 for workspace owner/admin, 403 for an
  ordinary member and 404 for a nonmember. Missing sharing booleans returned 400;
  unknown projects/tasks returned 404. Anonymous query/sharing routes returned 404.
- Repeated real-engine symbol, callers, callees, impact, document and source
  boundary queries on the successful public repository snapshot.
- Browser checks exercised workspace sharing and project assignment, the project
  resource section, and the empty-collection introduction. At 420px, the usage
  dialog measured 388px with no horizontal overflow and reachable bottom actions.
  The authenticated review pages reported no console errors; development preload
  warnings remain.

The permission checks exercise real PostgreSQL, HTTP and authenticated MCP paths.
They do not execute a model. Run-availability UI has typechecking coverage but has
not been exercised in a model-backed run. Only a personal-fork review is prepared;
the upstream PR remains pending human review and the checks listed below.

## UI alignment and base synchronization

The three later main commits were merged without conflicts or migration changes.
On the synchronized code, API/CLI compilation, all nine workspace typechecks,
targeted UI lint, 17 route-space checks and concurrent-index recovery contracts
passed. The UI reuses Field, Select, Alert and Dialog primitives; registration
uses a native form and DialogTrigger. CodeGraph is under Team → Resources in
both applications' shared shell.

Real browser checks covered dark theme, desktop and 420px layouts, registration
authentication selection and a successful anonymous access check against
`expressjs/serve-static`. The narrow dialog had no horizontal overflow and its
footer stayed inside the viewport. Git credentials retain a visible
missing-encryption state rather than allowing an unsavable new credential.

The initial checks below predate the three-commit synchronization unless
explicitly repeated above. Complete repository and model-backed acceptance
remain outstanding.

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

Public graph sharing was exercised in the initial review, then removed from
the contribution together with its routes, UI, API fields and migration flag.
Skill export handlers and UI controls were also removed. Anonymous Git fetching
for public repositories remains available and is separate from query access.
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
- Several cross-query build/credential flows still live in HTTP handler files.
  Service-layer extraction needs further architecture review.
