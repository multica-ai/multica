# VERIFY — firtal-cerebro

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

Run the complete repository contract before delivery:

```sh
make check-worktree
```

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

## 4. Teardown

Stop only the processes owned by this worktree:

```sh
make stop-worktree
```

Do not run `make db-down` from a worktree because PostgreSQL is shared with
other checkouts. The browser specs remove their own test records, including
revoked service-token rows and audit rows.
