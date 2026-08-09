# VERIFY — firtal-cerebro

**This file is the single source of truth for how work in this repository is
verified.** CLAUDE.md points here and does not restate the commands, so the two
cannot drift.

Never mark work complete, open a PR, or claim a fix works without green checks.
Evidence before assertions.

## 0. Which check command to run

`check`, `check-main` and `check-worktree` all run the same `scripts/check.sh`
(typecheck, TS unit tests, Go tests, E2E). They differ only in which env file
they load, and loading the wrong one runs the pipeline against another
checkout's database and ports.

| Where you are | Command | Env file |
|---|---|---|
| A git worktree | `make check-worktree` | `.env.worktree` |
| The main checkout | `make check` | `.env` |
| Forcing the main checkout from inside a worktree | `make check-main` | `.env` |

`.env.worktree` exists in a worktree and not in a main checkout — that is how
you tell which you are in. `multica repo checkout` and `git worktree add` both
produce worktrees.

Workflow: write code, run the command for your checkout, read any failure
output, fix, re-run until green. Only then is the task complete.

**Quick iteration.** While iterating, run only the layer you touched, then
finish with the full command above before claiming done:

```sh
pnpm typecheck              # TypeScript type errors only
pnpm test                   # TS unit tests only (Vitest, all packages)
make test                   # Go tests only
pnpm exec playwright test   # E2E only (requires backend + frontend running)
```

Single tests:

```sh
pnpm --filter @multica/views exec vitest run auth/login-page.test.tsx
cd server && go test ./internal/handler/ -run TestName
pnpm exec playwright test e2e/tests/specific-test.spec.ts
```

## Functional proof (browser contracts)

The functional proof runs against `localhost` with an isolated worktree
database. Do not use staging, production, Cloudflare Access, or shared
credentials for this contract.

## 1. Boot

Prepare the worktree and start the backend and web app:

```sh
make setup-worktree
make start-worktree
```

`make start-worktree` prints the worktree-specific localhost ports from
`.env.worktree`.

## 2. Seed test user

No persistent account or secret is required. `loginAsDefault` in
`e2e/helpers.ts` creates the localhost-only `e2e@multica.ai` session through
the real send-code and verify-code flow, ensures the isolated E2E workspace
exists, and navigates through `/login?next=…`.

The Playwright specs seed their own feature flags, agents, policies, issues,
and service tokens. Every spec restores or removes that data in `finally`.
The service-token contract proves the complete product lifecycle: the three
read-only scopes, mandatory 30/90/365-day expiry choices, one-time secret
reveal, scoped reading, write rejection, durable request audit, immediate
feature-flag rejection and UI removal, re-enable, revoke, and rejection after
revoke. It attaches final revoked-state screenshots at desktop and mobile
widths without exposing the secret.

## 3. Verify

Run the two permission-surface browser contracts:

```sh
pnpm generate:feature-catalog
git diff --exit-code -- server/cmd/multica/cerebro_feature_catalog.json
pnpm exec playwright test e2e/fir-3388-permissions-authoring.spec.ts e2e/fir-3388-capabilities.spec.ts e2e/fir-3755-service-tokens.spec.ts --project=chromium
```

Then run the complete repository contract for your checkout — see the table in
section 0.

Required CI checks:

- `CI / Verify cerebro feature catalog is up to date`
- `Cerebro E2E (agent configuration) / e2e`

For the production release proof, run:

```sh
multica agent-browser internal-verify --app multica
```

The sanitized result includes `version_commit`, the current UI markers, final
URL, browser errors, and the path to the screenshot. Compare `version_commit`
with the release merge commit before calling the production rollout complete.

Add `--page <path>` to end the run on another route of the same app — for
example `--app finance --page /controls`. The app's own login and marker checks
run first and still decide the verdict; the page is opened afterwards and is
what the screenshot shows. Only a path inside that app is accepted: a full URL
or a `//host/path` form is rejected, never followed.

## 4. Teardown

Stop only the processes owned by this worktree:

```sh
make stop-worktree
```

Do not run `make db-down` from a worktree because PostgreSQL is shared with
other checkouts. The browser specs remove their own test records, including
revoked service-token rows and audit rows.

## 5. Agent-context changes

Changes to the runtime brief, to always-on skill propagation, or to the
agent-context API are covered by:

```sh
cd server && go test ./internal/daemon/execenv/ -run "AlwaysOn|Brief"
cd server && go test ./internal/handler/ -run AlwaysOn
```

After changing anything an agent reads, also run
`multica agent context lint <agent-id>` and confirm every remaining finding is
documented and justified.
