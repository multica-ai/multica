# CI gates

The `CI` workflow keeps `frontend` and `backend` as its scope aggregates.
`scripts/ci-scope.mjs` validates the path decision and every dependency result:
selected jobs must succeed and only unselected jobs may be skipped. Daily and
manual runs select all scopes. After this workflow has merged into `main`,
branch protection must require **`CI required`**:
it always runs and requires both aggregates to succeed, rejecting failed,
cancelled, skipped or missing results. The release workflow also has a `backend`
job, so its skipped packaging result must not be mistaken for the CI gate.

## Go formatting

`backend-format` runs on the backend scope and feeds `backend`. It executes
`bash scripts/check-gofmt.sh` from the repository root. It recursively checks
Go files throughout `server/`, including tests, and fails on unformatted files
or formatter errors. Fork-owned files and fork edits to upstream files must
be formatted; unmodified upstream files must not acquire fork-only whitespace
changes just to satisfy this gate.

`scripts/gofmt-upstream-exceptions.txt` records the paths and Git blob IDs of
unformatted upstream originals, with the full upstream source commit in a
comment. The gate skips a listed path only when `git hash-object --no-filters`
matches that exact content. Editing or renaming the file automatically removes
its exemption; a stale entry cannot hide an unformatted fork edit. The gate
never rewrites files. Missing or malformed manifests fail closed, and manifest
changes select the backend CI scope.

This content-pinned list works with shallow checkouts and without network
access or an upstream ref in CI. It requires explicit reconciliation during
each [upstream sync](upstream-sync-checklist.md#go-formatting-scope): retain only
byte-identical, unformatted upstream originals, and take hashes from the
reviewed upstream commit, never from fork changes. The initial ten exceptions
come from `2ea01ae4ef55de4310b99af192d2dbd367832883`.

`bash scripts/check-gofmt.test.sh` proves that untouched upstream snapshots
pass without rewriting, while new fork files, edits to an exempted path and
syntax errors fail. Fixtures cover the repository's Go root, `server/cmd/`,
nested tests and paths with spaces. The regression uses the same no-argument
command as CI from a disposable root with no Git history. Run both commands
locally with Git and the CI Go 1.26 toolchain on `PATH`.

## Retained product E2E

`retained-e2e-tests` feeds `frontend`, including when only backend/migrations or
E2E files changed. The scope covers Web/Desktop/shared code, backend, fixtures,
the dedicated configuration/runner and dependency inputs. Documentation-only
changes do not allocate the browser runner.

The job installs locked dependencies and Chromium, then runs:

```bash
bash scripts/test-retained-e2e.sh
```

The script builds the actual Go server and production Web app, applies all
migrations to the job's disposable PostgreSQL 17 service, and runs exactly
`onboarding-smoke.spec.ts`, `dag-task-lines.spec.ts` and
`issue-dependencies.spec.ts` and `comments.spec.ts`. Before any build or
migration, it verifies that all required spec files exist; a rename or deletion
fails the gate instead of silently running fewer files. It uses local test
credentials, API `127.0.0.1:18080`, Web `127.0.0.1:13000` and database
`labrastro_e2e` on `127.0.0.1:15432`. It overwrites inherited application/database
URLs and disables email delivery, self-host/Next.js telemetry and PostHog
analytics (`DO_NOT_TRACK=1`, `NEXT_TELEMETRY_DISABLED=1`,
`ANALYTICS_DISABLED=1`), including when the shell supplies a PostHog key.
No production secret is needed.
No daemon/agent process, production account or production database participates.

Use a disposable checkout without root `.env`/`.env.worktree` or Web dotenv
overrides. The runner rejects those files before building or migrating.
Playwright owns both service processes, refuses to reuse an existing server,
waits for readiness and stops the processes on completion. Tests run with one
worker, no retries and `forbidOnly`; existing product assertions remain intact.
The artifact `retained-e2e-<run_id>-<run_attempt>` retains setup/build logs, the
HTML report, screenshots and failure traces for seven days. Server output also
appears in the job log.

For local reproduction, first provision a **new disposable** PostgreSQL service
with the same database, user/password `labrastro_e2e` and loopback port 15432.
For example, on a host with Docker, from that disposable checkout:

```bash
docker run --rm --detach --name labrastro-retained-e2e-db \
  -e POSTGRES_DB=labrastro_e2e -e POSTGRES_USER=labrastro_e2e \
  -e POSTGRES_PASSWORD=labrastro_e2e -p 127.0.0.1:15432:5432 \
  pgvector/pgvector:pg17
trap 'docker stop labrastro-retained-e2e-db' EXIT
pnpm install --frozen-lockfile
pnpm exec playwright install --with-deps chromium
bash scripts/test-retained-e2e.sh
```

Do not point this recipe at a shared database or tunnel production to the test
port. Existing developer checkouts should continue using their managed
`make up`/`make down` environment and ordinary `playwright.config.ts`.

If the hosted job cannot run, record its actual failed step and logs. The
temporary release gate is this same full command on a disposable supported
Linux host against the exact reviewed candidate SHA, with every retained test
passing and the report attached to the release handoff. Missing browser,
database, build or test evidence blocks release; a skip, unit-only result or
an existing-account smoke test does not satisfy this gate.

## Main branch protection

A repository administrator must configure the following in this order:

1. **Require a pull request: on. Required approvals: 0** (leave **Require
   approvals** unchecked). Authors and reviewing agents share the GitHub
   identity `AstralSolipsism`, which cannot approve its own PR. Requiring even
   one GitHub approval with administrator enforcement would prevent every
   agent-authored PR from merging. Dismissing stale GitHub approvals is therefore
   not applicable. The workspace still requires two sequential expert reviews
   for high-risk changes, each recording approval of the final head SHA in the
   issue; a new head needs fresh review.
2. **After this PR has merged into `main`**, require the successful
   **`CI required`** check from GitHub Actions and require branches to be up to
   date before merge. Before that merge, other PRs' merge refs do not contain
   this job, so requiring it would block them on a check they cannot produce.
   Update other open PR branches from the new `main` so their new runs include
   the check.

Enforce the rules for administrators and bypass-capable roles as well; leave
no direct-push/bypass allowance, force pushes or deletions. This change adds no
requirement to resolve GitHub review conversations; any such separate policy
is Master's decision.

After saving the rule, verify in GitHub that an ordinary direct update to main
is disallowed and a PR with a failed/missing required aggregate cannot merge;
use the rule UI/API, not a destructive test push. Token access to repository
content does not establish access to branch administration. When the current
token is denied, put these exact settings in the PR and ask Master to configure
them without changing identities.

See [GitHub's branch protection reference](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches)
and [Playwright's service lifecycle](https://playwright.dev/docs/test-webserver).
