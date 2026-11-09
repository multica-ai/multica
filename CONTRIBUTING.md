# Contributing Guide

This guide documents issue and pull request requirements and the local development workflow for contributors working on the Multica codebase.

It covers:

- issue templates, pull request requirements, and review evidence
- first-time setup
- environments: starting, inspecting, stopping and deleting them
- day-to-day development in the main checkout
- isolated worktree development
- the shared PostgreSQL model
- testing and verification
- full-stack isolated testing (backend + frontend + daemon from source)
- troubleshooting and destructive reset options

## Contribution Terms

By submitting a contribution to Multica — a pull request, a patch, or any
other work — you agree to condition 2 of the [Multica License](LICENSE):

- your contribution is submitted under the Multica License as a whole (the
  additional conditions in Part I together with the incorporated Apache
  License 2.0 text in Part II), not under the Apache License 2.0 alone;
- your contributed code may be used for commercial purposes, including the
  producer's cloud business operations;
- the producer can adjust the Multica License to be more strict or relaxed
  as deemed necessary.

See the [LICENSE](LICENSE) file for the full terms.

## Opening an Issue

Choose the form that fits your report:

- **[Feature request](.github/ISSUE_TEMPLATE/feature_request.yml):** Explain the
  **Problem** and **Goal**. A high-level **Proposal**, **Alternatives**, and
  **Screenshots** are optional; you can raise a problem without designing its
  solution.
- **[Bug report](.github/ISSUE_TEMPLATE/bug_report.yml):** Describe the
  **Problem**, **Expected behavior**, and **Steps to reproduce**. For intermittent
  bugs, explain what you tried and how often it happens. Screenshots, logs, and
  environment details are optional supporting information.

Select the affected area so we can route the report. Bug reports also ask for
the deployment type. Screenshots are optional for issues; the mandatory
screenshot rule below applies to PRs that change UI or interactions.

## Pull Request Requirements

Use the [PR template](.github/PULL_REQUEST_TEMPLATE.md) with these five sections:

1. **Problem:** Describe the concrete problem, who is affected, and why it matters.
2. **Goal:** State the intended outcome of the change.
3. **Proposal:** Explain the high-level design and why it achieves the goal.
   Make it understandable to a reader without much project background. Keep
   implementation details to a minimum.
4. **Alternatives (optional):** When useful, briefly describe other approaches
   and why you did not choose them.
5. **Screenshots:** For UI or interaction changes, attach screenshots or a
   screen recording from the running app showing the affected screens and
   states. Before/after comparisons are strongly preferred; for a new screen,
   show the after state. If there is no UI or interaction impact, write "N/A"
   and explain why.

End the description with `Closes #<issue>` when the PR resolves an issue.

**UI or interaction PRs without screenshots or a recording are rejected by
default.** Reviewers should request changes or close the PR until one is
provided. PRs
missing the problem, goal, or high-level proposal are also not ready for review.

### Screenshots and Recordings

Store PR evidence as [GitHub attachments](https://docs.github.com/en/github-cli/github-cli/attaching-files-with-github-cli).
Their URLs do not depend on a temporary Git branch and have no preset expiry;
GitHub does not promise permanent archival. Keep visual-test reference images
with their tests, and use CI artifacts for temporary debugging reports. Actions
artifacts expire, so they are unsuitable as the only copy of PR evidence.

From the repository root, upload files to an existing PR with Node.js 22+ and
[GitHub CLI 2.103.2+](https://github.com/cli/cli/releases/tag/v2.103.2):

```bash
scripts/pr-shots --repo multica-ai/multica 123 ./before.png ./after.png ./demo.mp4
```

By default, the script appends attachments at the end of the PR description
(after any closing issue reference), preserves its existing content, and prints
the resulting attachment Markdown to stdout. A PR URL can replace the number;
`--repo` is optional when `gh` can identify the repository. Diagnostics
go to stderr. Files are identified by SHA-256 in hidden comments next to the
attachments in the PR body, so retries and renamed files reuse uploaded content
across checkouts. Keep those comments if you want that retry behavior. Changed
file content produces a new attachment.

The attachments are already in the PR description; do not paste another copy
of the stdout Markdown into it. To place an attachment under Screenshots, move
its entire block from the end of the description: the hidden hash comment,
blank lines, and image Markdown or video URL together.

Uploads run one file at a time and stop at the first failure. Already confirmed
files remain saved; stderr identifies the failed and unattempted files. The
script reads back the PR even when `gh` exits nonzero, because a write may have
succeeded. Re-run the same command to reuse confirmed attachments and upload
the missing ones. Run one uploader per PR at a time and avoid editing the
description during an upload; GitHub body edits are not conditional writes.

To migrate an existing screenshot while keeping its caption and position, use
`--replace` with exactly one image file. The old URL must occur in a standalone
Markdown image, such as `![Original caption](https://example.com/old.png)`:

```bash
scripts/pr-shots --repo multica-ai/multica \
  --replace https://example.com/old.png 123 ./after.png
```

Verify the new attachments render and remain accessible to the PR's reviewers
before deleting old storage. Check other PRs and repository references before
removing a shared evidence branch. Preserve old links for any failed migration.

You can also use the native CLI directly, including during PR creation:

```bash
gh pr create --body-file ./pr.md --attach ./before.png --attach ./after.png
gh pr edit 123 --attach ./demo.mp4
```

In `pr.md`, write `![Before](./before.png)` and `![After](./after.png)` where
the images should appear. For video, put `![](./demo.mp4)` in its own paragraph.
The CLI replaces these local references with uploaded URLs; unreferenced
attachments are appended. Native `gh --attach` does not deduplicate retries.

CLI uploads require **push access to the target repository** and a credential
accepted by GitHub's upload endpoint. Version 2.103.2 lets that endpoint decide
which GitHub App tokens can upload; it does not guarantee support for every App
or an Actions `GITHUB_TOKEN`. Verify the actual agent/CI credential. Without
push access, upload through the PR editor in GitHub's web interface. Contributors
who must stay in the CLI should ask a maintainer in the PR to upload their files;
opening a fork PR alone does not grant upload permission on the upstream repo.
This helper targets GitHub.com, not GitHub Enterprise Server.

Supported files are PNG, JPEG, GIF, WebP, SVG, MP4, MOV, and WebM. Images are
limited to 10 MB; video limits are 10 MB on Free plans and 100 MB on paid plans.
Use H.264 MP4 for broad playback support. Public-repository attachments are
publicly accessible: use test data and inspect captures before uploading.

Capture the affected screens from the running application and record the
commit, viewport, theme, and reproduction steps with the evidence. Use the
local environment workflow below; for Desktop recording, see
[Scripting Desktop over CDP](#scripting-desktop-over-cdp). For stable screenshots:

```js
await page.screenshot({ path: "after.png", animations: "disabled" });
```

This completes finite CSS/Web Animations and resets infinite ones to their
initial frame during capture. To compare a particular animation frame, pause
`document.getAnimations()` and set each animation's `currentTime` to the same
timestamp on both versions instead. Hold a loading state with a controlled
local API response, rather than relying on a fixed sleep. Include a short
recording when the motion or sequence itself is the change under review.

## Development Model

Local development uses one shared PostgreSQL container and one database per checkout.

- the main checkout usually uses `.env` and `POSTGRES_DB=multica`
- each Git worktree uses its own `.env.worktree`
- every checkout connects to the same PostgreSQL host: `localhost:5432`
- isolation happens at the database level, not by starting a separate Docker Compose project
- backend and frontend ports are still unique per worktree

This keeps Docker simple while still isolating schema and data.

## Prerequisites

- Node.js `22`
- `pnpm` `10.28.2`
- Go `1.26.9`
- Docker

Outside mobile, `pnpm typecheck` uses the native TypeScript 7 compiler (`tsc`,
provided by the `@typescript/native` npm alias). The `typescript` dependency
aliases `@typescript/typescript6` so ESLint, Next.js builds, and repository
scripts retain the compatible TypeScript 6 API. Use `tsc6` to check with that
compiler explicitly. Keep both dependencies when updating the toolchain;
replacing the API package with TypeScript 7 breaks API consumers.
Mobile keeps its own TypeScript dependency.
When removing TS6 compatibility, update the `typescript` peer range in
`packages/eslint-config/package.json` together with TypeScript API support in
`typescript-eslint`.

### Editor TypeScript SDK

The compatibility package provides the compiler API but does not include
`node_modules/typescript/lib/tsserver.js`. An existing workspace SDK selection
or `typescript.tsdk` setting pointing to `node_modules/typescript/lib` can
therefore become invalid or fall back to the editor's bundled TypeScript.

To keep TS6 in VS Code with the repository root open:

1. After `pnpm install`, set the SDK directory to
   `node_modules/@typescript/old/lib`, the real TS6 installation in this
   repository's pnpm layout. Use `typescript.tsdk`, or `js/ts.tsdk.path` in
   versions using that setting name. The selected directory must contain
   `tsserver.js`.
2. Open a TypeScript file, run **TypeScript: Select TypeScript Version**, and
   select the workspace version. Confirm TS6 is selected in the language
   status item. See [VS Code's SDK selection instructions](https://code.visualstudio.com/docs/typescript/typescript-transpiling#_using-newer-typescript-versions).

To use the TS7 language server instead, remove the TS6 SDK override, install
the [official TypeScript 7 extension](https://marketplace.visualstudio.com/items?itemName=TypeScriptTeam.native-preview),
and run **TypeScript: Enable TypeScript 7**. The editor language server is
selected separately from the CLI compiler used by `pnpm typecheck`.

## Important Rules

- The main checkout should use `.env`.
- A worktree should use `.env.worktree`.
- Do not copy `.env` into a worktree directory.

Why:

- the current command flow prefers `.env` over `.env.worktree`
- if a worktree contains `.env`, it can accidentally point back to the main database

## Environment Files

### Main Checkout

Create `.env` once:

```bash
cp .env.example .env
```

By default, `.env` points to:

```bash
POSTGRES_DB=multica
POSTGRES_PORT=5432
DATABASE_URL=postgres://multica:multica@localhost:5432/multica?sslmode=disable
PORT=8080
FRONTEND_PORT=3000
```

### Worktree

Generate `.env.worktree` from inside the worktree:

```bash
make worktree-env
```

That generates values like:

```bash
POSTGRES_DB=multica_my_feature_702
POSTGRES_PORT=5432
PORT=18782
FRONTEND_PORT=13702
DATABASE_URL=postgres://multica:multica@localhost:5432/multica_my_feature_702?sslmode=disable
```

Notes:

- `POSTGRES_DB` is unique per worktree
- `POSTGRES_PORT` stays fixed at `5432`
- backend and frontend ports are derived from the worktree path hash
- `make worktree-env` refuses to overwrite an existing `.env.worktree`

To regenerate a worktree env file:

```bash
FORCE=1 make worktree-env
```

## Environments

An environment is the database, ports, CLI profile and processes that belong to
one checkout. It is a named object: it can be listed, inspected and deleted.

```bash
make up                      # start this checkout's environment (api + web)
make up C=api,web,daemon     # choose the components
make status                  # what is running, and whether it is yours
make list                    # every environment on this machine
make down                    # stop the processes, keep the data
make destroy                 # stop, then drop the database and free the slot
make gc                      # collect expired environments or ones whose checkout is gone
```

Components are `api` (Go backend), `web` (Next.js), `daemon` (agent daemon) and
`desktop` (Electron). Selecting any of them implies `api`. `make up` is
idempotent: re-running it against a live environment reuses the database, the
profile and any component already healthy.

Three properties are worth knowing because the old flow lacked them:

- **API, Web and Desktop renderer ports, database names and profiles are allocated, not recomputed.** The
  allocator starts from this directory's path hash, so a checkout keeps the
  numbers it has always had, and moves only when the registry or a live
  listener says the slot is taken. The registry lives in `~/.multica/dev/`;
  deleting it and re-running `make up` is a supported recovery.
- **Nothing reports success for something it has not reached.** The database is
  created and verified through `DATABASE_URL` — the same string the backend
  uses — and `GET /health` reports `pid`, `commit` and `started_at` so `make up`
  can prove the process answering is the one it just started rather than a
  leftover on the same port.
- **`down` and `destroy` differ deliberately.** `down` stops processes and
  keeps the database, profile and slot, so the next `make up` is seconds.
  `destroy` consumes the database, profile, daemon task workspaces, Desktop
  userData and slot. If any deletion fails, it keeps the manifest and exits
  non-zero so cleanup can be retried instead of losing the deletion recipe.
- **Temporary environments have a best-effort fallback.** `make up
  ARGS=--ephemeral` records a 24-hour TTL. The next `make up` automatically
  collects expired and directory-less environments; `make gc` runs the same
  collection explicitly.

Run any command inside an environment's variables without repeating them:

```bash
make env-exec ARGS="-- pnpm exec playwright test"
```

`make dev` (below) still runs backend and frontend in the foreground of your
terminal, which is the right thing when you want Ctrl-C to stop everything.

## First-Time Setup

### Quick Start (recommended)

From any checkout (main or worktree):

```bash
make dev
```

This single command:

- auto-detects whether you're in a main checkout or a worktree
- creates the appropriate env file (`.env` or `.env.worktree`) if it doesn't exist
- checks that prerequisites (Node.js, pnpm, Go, Docker) are installed
- installs JavaScript dependencies
- ensures the shared PostgreSQL container is running
- creates the application database if it does not exist
- runs all migrations
- starts both backend and frontend

### Explicit Setup (advanced)

If you prefer separate control over setup and startup:

#### Main Checkout

```bash
cp .env.example .env
make setup-main
make start-main
```

Stop:

```bash
make stop-main
```

#### Worktree

```bash
make worktree-env
make setup-worktree
make start-worktree
```

Stop:

```bash
make stop-worktree
```

## Recommended Daily Workflow

### Main Checkout

Use the main checkout when you want a stable local environment for `main`.

```bash
make start-main
make stop-main
make check-main
```

### Feature Worktree

Use a worktree when you want isolated data and separate app ports.

```bash
git worktree add ../multica-feature -b feat/my-change main
cd ../multica-feature
make dev
```

After that, day-to-day commands are:

```bash
make dev              # start (re-runs setup if needed, idempotent)
make stop-worktree    # stop
make check-worktree   # verify
```

### Git Identity in Managed Task Checkouts

`multica repo checkout` uses the user's system/global Git identity, including
conditional includes evaluated in the checkout. Agent names determine branch
names, not commit authors. The optional `Co-authored-by` trailer is independent
of author and committer identity.

Linked worktrees mask the shared cache's `user`, `author`, and `committer`
name/email settings with private defaults in `multica-identity.config`, included
first by their `config.worktree`. Repeating checkout refreshes these defaults;
explicit worktree settings and Git command/environment overrides take precedence.
Conditional identities are snapshots: switching branches or changing global
config takes effect in these defaults on the next checkout call, not at commit time.
Use `git config --worktree user.name "Your Name"` and
`git config --worktree user.email "you@example.com"` for an intentional checkout
override. Plain `git config` and `--local` write to the shared cache in linked
worktrees. Isolated clones already have private config and retain it unchanged.
If no user identity is configured, commits fail instead of using a stale cache
identity; configure an explicit worktree identity before committing.

Existing linked checkouts receive the protection on their next checkout call,
including calls that keep local work. Residual shared identity is masked, not
deleted: changing it would change other running tasks. Existing tasks that never
repeat checkout remain unprotected until they do. Enabling `worktreeConfig`
moves `core.bare`/`core.worktree` to the bare repository's own `config.worktree`
without changing other worktrees' effective settings. The files are standard Git
config; older daemons leave them in place, but do not refresh identity defaults
or protect newly created worktrees. No pushed commit history is rewritten.

### Removing a Worktree

Git does not provide a `pre-worktree-remove` hook. Use the repository wrapper
from another checkout so database cleanup happens before Git removes the
worktree directory:

```bash
make remove-worktree WORKTREE=../multica-feature
```

The command refuses to remove the primary checkout, the current checkout, a
locked worktree, or a worktree with uncommitted changes. If the target contains
`.env.worktree`, it shows the database name and asks for `y/N` confirmation,
drops that database, and only then runs `git worktree remove`. A worktree that
was never set up has no `.env.worktree`, so database cleanup is skipped.

Running `git worktree remove` directly bypasses this cleanup and can leave an
orphaned local database.

## Running Main and Worktree at the Same Time

This is a first-class workflow.

Example:

- main checkout
  - database: `multica`
  - backend: `8080`
  - frontend: `3000`
- worktree checkout
  - database: `multica_my_feature_702`
  - backend: generated worktree port such as `18782`
  - frontend: generated worktree port such as `13702`

Both checkouts use:

- the same PostgreSQL container
- the same PostgreSQL port: `5432`

But they do not share application data, because each uses a different database.

## Command Reference

### Shared Infrastructure

Start the shared PostgreSQL container:

```bash
make db-up
```

Stop the shared PostgreSQL container:

```bash
make db-down
```

Important:

- `make db-down` stops the container but keeps the Docker volume
- your local databases are preserved

### App Lifecycle

Main checkout:

```bash
make setup-main
make start-main
make stop-main
make check-main
```

Worktree:

```bash
make worktree-env
make setup-worktree
make start-worktree
make stop-worktree
make check-worktree
```

Generic targets for the current checkout:

```bash
make setup
make start
make stop
make check
make dev
make test
make migrate-up
make migrate-down
```

These generic targets require a valid env file in the current directory.

## How Database Creation Works

Database creation is automatic.

The following commands all ensure the target database exists before they continue:

- `make setup`
- `make start`
- `make dev`
- `make test`
- `make migrate-up`
- `make migrate-down`
- `make check`

That logic lives in `scripts/ensure-postgres.sh`.

## Testing

Run all local checks:

```bash
make check-main
```

Or from a worktree:

```bash
make check-worktree
```

This runs:

1. TypeScript typecheck
2. TypeScript unit tests
3. Go tests
4. Playwright E2E tests

Notes:

- Go tests create their own fixture data
- E2E tests create their own workspace and issue fixtures
- the check flow starts backend/frontend only if they are not already running

## Local Codex Daemon

Run the local daemon:

```bash
make daemon
```

The daemon authenticates using the CLI's stored token (`multica login`).
It registers runtimes for all watched workspaces from the CLI config.

## Full-Stack Isolated Testing

Running the complete stack — backend, frontend and daemon — from source, with
its own database and CLI profile, is one command:

```bash
make up C=api,web,daemon
```

It creates the environment if needed, sets the fixed local verification code
before the first launch, logs in as `dev@localhost`, mints a personal access
token, creates a workspace, writes the CLI profile, builds `server/bin/multica`
and starts the daemon from that binary. It then prints the URL, the login, the
commit, and the stop command.

Two constraints are enforced rather than documented:

- **The daemon runs from a built binary, never `go run`.** The daemon records
  its own executable path at startup and re-execs it as the
  execution-environment helper for every task; `go run` deletes that binary when
  the launcher exits, so the daemon would register, heartbeat, and then fail
  every task with `fork/exec …/go-build…/exe/multica: no such file or directory`.
- **`daemon start` is refused under a daemon-managed task.** A checkout below a
  `.multica/daemon_task_context.json` marker cannot start a second daemon
  competing for its own work, so `make up C=daemon` stops with that explanation
  before spending a login on it. Use `C=api,web` there.

### Desktop

```bash
make up C=desktop
```

This writes a marked `apps/desktop/.env.development.local` pointing at this
environment's backend, starts Electron with the renderer port and app name from
the environment registry, and waits until that renderer is actually serving.
Several checkouts can therefore run Desktop side by side without maintaining a
second path-derived identity. `make destroy` removes the marked env file and
this environment's Electron userData. Direct `pnpm dev:desktop` still uses its
path-derived fallback when it is run outside `make up`.

Log in with `dev@localhost` and `888888`.

#### Scripting Desktop over CDP

Some bugs only reproduce in Electron, so evidence for them has to come from the
running app driven with real input. Start Desktop with a Chrome DevTools
Protocol endpoint, then attach Playwright to it:

```bash
make up C=desktop ARGS="--ephemeral --cdp"
make env-exec ARGS="-- node scripts/desktop-cdp.mjs --screenshot desktop.png"
make env-exec ARGS="-- node scripts/desktop-cdp.mjs --record steps.mp4 ./steps.mjs"
make destroy ARGS="--yes"
```

`--cdp` allocates a port for the environment, `--remote-debugging-port N`
picks one, and `--no-cdp` drops it; later `make up` runs keep it. `make status`
prints the endpoint, and `make status ARGS="--json"` reports `desktop_cdp_port`.

`scripts/desktop-cdp.mjs` attaches to the main window of the already signed-in
Desktop. A script argument is an ES module whose default export receives
`{ browser, context, page, env, login, screencast, args }`. Drive it with
`page.mouse` or locator clicks, which go through Chromium's input pipeline,
rather than `element.click()` inside `page.evaluate`. `--login`, or `login()`
from a script, signs `dev@localhost` back in after a step signed out.
`--record` needs `ffmpeg` to encode; without it the frames and an ffconcat file
stay in `<file>.frames/`. The helper only disconnects, so the app stays up for
the next attach.

### Isolation Guarantee

Nothing in this flow touches the system-installed `multica` or the default
`~/.multica/config.json`:

| Resource | System / Production | Local Dev (per environment) |
|---|---|---|
| Config | `~/.multica/config.json` | `~/.multica/profiles/dev-<slug>-<offset>/config.json` |
| Daemon PID | `~/.multica/daemon.pid` | `~/.multica/profiles/dev-<slug>-<offset>/daemon.pid` |
| Workspaces dir | `~/multica_workspaces/` | `~/multica_workspaces_dev-<slug>-<offset>/` |
| Database | remote / production | local: `multica_<slug>_<offset>` |
| Registry | — | `~/.multica/dev/envs/<name>/` |
| Desktop profile | `desktop-api.multica.ai` | `desktop-localhost-<port>` |

Multiple environments run simultaneously without conflict; `make list` shows
all of them.

## Troubleshooting

### Missing Env File

If you see:

```text
Missing env file: .env
```

or:

```text
Missing env file: .env.worktree
```

then create the expected env file first.

Main checkout:

```bash
cp .env.example .env
```

Worktree:

```bash
make worktree-env
```

### Check Which Database a Checkout Uses

Inspect the env file:

```bash
cat .env
cat .env.worktree
```

Look for:

- `POSTGRES_DB`
- `DATABASE_URL`
- `PORT`
- `FRONTEND_PORT`

### List All Local Databases in Shared PostgreSQL

```bash
docker compose exec -T postgres psql -U multica -d postgres -At -c "select datname from pg_database order by datname;"
```

### Worktree Is Accidentally Using the Main Database

Check whether the worktree contains `.env`.

It should not.

The safe worktree setup is:

```bash
make worktree-env
make setup-worktree
make start-worktree
```

### App Stops but PostgreSQL Keeps Running

That is expected.

- `make stop`
- `make stop-main`
- `make stop-worktree`

only stop backend/frontend processes.

To stop the shared PostgreSQL container:

```bash
make db-down
```

## Destructive Reset

If you want to stop PostgreSQL and keep your local databases:

```bash
make db-down
```

If you want a fresh database for the current checkout only (drops the
database named in `POSTGRES_DB`, recreates it, and runs all migrations):

```bash
make stop        # stop backend/frontend first
make db-reset
make start
```

- only affects the current env's database; other worktree databases are untouched
- refuses to run if `DATABASE_URL` points at a remote host
- pass `ENV_FILE=.env.worktree` to target a specific worktree

To permanently drop the current worktree database without recreating it:

```bash
make db-drop ENV_FILE=.env.worktree
```

The command prints the selected database and environment file, then requires a
`y/N` confirmation. It only operates on the local Docker PostgreSQL service,
protects PostgreSQL system databases, and refuses to drop the default main
database `multica` unless `ALLOW_MAIN_DB_DROP=1` is explicitly supplied.
Declining the confirmation is a successful no-op; when called by
`make remove-worktree`, it also leaves the worktree in place.

If you want to wipe all local PostgreSQL data for this repo:

```bash
docker compose down -v
```

Warning:

- this deletes the shared Docker volume
- this deletes the main database and every worktree database in that volume
- after that you must run `make setup-main` or `make setup-worktree` again

## Typical Flows

### Stable Main Environment

```bash
make dev
```

### Feature Worktree

```bash
git worktree add ../multica-feature -b feat/my-change main
cd ../multica-feature
make dev
```

### Return to a Previously Configured Worktree

```bash
cd ../multica-feature
make start-worktree
```

### Validate Before Pushing

Main checkout:

```bash
make check-main
```

Worktree:

```bash
make check-worktree
```
