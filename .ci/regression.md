# Regression and Acceptance Convention

> **Default: Human acceptance is required after tester regression unless the task explicitly waives it.** Tester agents must preserve the tested preview environment intact after regression so the human reviewer can perform final verification.

This file defines project-level regression rules for Multica PR acceptance, upstream merge verification, and release verification. It complements `.ci/deploy.md`.

## Principles

1. **Project docs own project-specific testing workflow.** Tester agents provide generic QA ability, but Multica-specific environment, fixtures, testcase layout, and reporting rules live in this repository.
2. **Browser behavior requires browser evidence.** Typecheck, build, unit tests, and code review are not substitutes for UI acceptance when the changed behavior is user-visible.
3. **Correct branch only.** PR acceptance must validate the PR branch/worktree or target release branch. An unrelated `main` dev server is only baseline evidence.
4. **BLOCKED is not PASS.** Blocked cases must be listed and either resolved, explicitly accepted by the user, or kept as release risk.

## Required reading for tester agents

Before executing Multica browser regression, read:

- `AGENTS.md` / `CLAUDE.md`
- `testcase/browser-regression-guide.md`
- Relevant `testcase/case/tc-*.md`
- `testcase/fixtures/README.md` when a case needs test data setup

## Acceptance gates

For PR / upstream merge / release acceptance:

1. Perform testcase coverage gap analysis against Issue, PR, diff, and recent Fork-specific changes.
2. Add or update testcase docs when a user-visible or Fork-specific change lacks coverage.
3. Run selected impacted browser cases against the correct build.
4. Upload report and evidence to the Multica Issue when the task is executed through Multica.
5. Report PASS / PARTIAL PASS / BLOCKED / FAIL with exact blockers and failed-case handback.
6. Preserve the tested preview/worktree after tester-run regression so Guodage can perform final human acceptance. Do not tear down Docker Compose preview runtimes, delete worktrees, or delete branches as part of the tester run unless explicitly instructed.

## Preview lifecycle for tester-run regression

When a browser regression requires a local self-host build, prefer:

```bash
make selfhost-build-preview ISSUE=OPE-123
```

The `ISSUE` parameter is the primary user-facing key. Internally the preview derives a profile and Docker Compose project from it (for example `OPE-123` → `multica_preview_ope_123`). `PROFILE=<name>` is only an advanced override for non-Issue experiments or duplicate previews.

Tester reports must include enough information for later human acceptance and cleanup:

- Issue key,
- preview profile,
- Docker Compose project,
- frontend URL,
- backend URL,
- worktree path,
- branch,
- commit SHA and/or PR number.

If Guodage needs to verify from a phone or another machine, the tester may create a temporary tunnel/public link and include that URL in the report.

Recommended quick path for local human acceptance is to expose the preview frontend port only, for example:

```bash
ngrok http <frontend-port>
```

Notes:

- The tunnel URL should be treated as a temporary human-acceptance link, not a production/staging URL.
- The report must include both the tunnel URL and the original local frontend URL.
- API requests normally work through the frontend's same-origin proxy/rewrite. WebSocket/live-update support is best effort unless the task specifically requires it; if WebSocket behavior is in scope, call it out explicitly and validate the tunnel/proxy path for `/ws`.
- If ngrok refuses to start because local HTTP/HTTPS proxy env vars are set, run it with proxy env vars unset (for example `env -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY ngrok http <frontend-port>`).

Cleanup is a separate lifecycle step. `make selfhost-preview-clean ISSUE=OPE-123` may stop the Docker Compose preview runtime, but it must not delete Postgres containers, volumes, databases, or long-lived local test data. Git worktree/branch cleanup is handled by the explicit unified worktree cleanup flow, not by a single tester run.

## Agent-run-dependent cases

Cases that depend on task runs, execution logs, retry, plan mode, failed tasks, or notifications must follow `testcase/fixtures/README.md` before they can be marked BLOCKED.

At minimum, the tester must attempt to:

- check or restart the local profile daemon for local/self-host testing,
- ensure a suitable test agent/runtime exists,
- create a task run via markdown agent mention or `multica issue rerun`,
- report the exact setup step that failed if still blocked.

## Reporting

Reports must include:

- tested URL,
- preview profile and Docker Compose project when a self-host preview runtime was used,
- worktree path,
- branch,
- commit SHA and/or PR number,
- selected cases and selection mode,
- build/typecheck/test results when applicable,
- browser regression result,
- screenshots/report attachments,
- failed or blocked cases with exact reasons.
