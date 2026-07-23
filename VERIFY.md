# VERIFY — firtal-cerebro

All commands use the isolated worktree database and localhost. The Playwright
fixture creates its own test login, runtime, agent, allow/deny policies, and
removes them in `finally`.

## 1. Boot

```bash
make setup-worktree
make start-worktree
```

## 2. Seed test user

No manual credential is required. `loginAsDefault` creates the localhost-only
`e2e@multica.ai` login through the local API and reads its one-time code from
the isolated local database.

## 3. Verify

```bash
pnpm exec playwright test e2e/fir-3388-capabilities.spec.ts --project chromium
```

The test drives the real `Capabilities` tab, proves an allowed and denied
policy from the database fixture, reloads, and proves both results persist.
The required CI check is `e2e` in `Cerebro E2E (agent configuration)`.

## 4. Teardown

The test removes its policy, agent, runtime, feature flag, and login fixture.
Stop the local app after the suite:

```bash
make stop
```

Never point these commands at production or a production database copy.
