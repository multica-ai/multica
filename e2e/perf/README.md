# UI performance scenarios

The comment-typing scenario models a long issue thread while three agent runs
stream progress. It exists because of MUL-7227, where a summary animation
replaced a DOM node per streamed message and typing a comment slowed to a crawl.
Component tests could not see it — they run in jsdom, which has no style
recalculation, no layout and no main-thread contention.

## Chat session lists

`chat-session-lists.spec.ts` exercises the real Chat history, archive and floating
history dropdown against synthetic HTTP/WebSocket boundaries. It checks that
2,000 sessions mount fewer than 40 rows, can scroll to and select the last row,
and also covers compact layout and empty/single-session dropdowns. Each measured
surface emits a JSON record and a test attachment with mounted rows and time to
readiness; timings are illustrative samples, not performance thresholds.

Run against a local production Web build (no API server or account needed):

```bash
PLAYWRIGHT_BASE_URL=http://localhost:3000 pnpm exec playwright test --config=playwright.perf.config.ts chat-session-lists.spec.ts
# Measure the original full-list implementation with the same fixture:
CHAT_LIST_BASELINE=1 PLAYWRIGHT_BASE_URL=http://localhost:3001 pnpm exec playwright test --config=playwright.perf.config.ts chat-session-lists.spec.ts
```

`scripts/perf-compare.mjs` remains scoped to the comment-typing scenario below;
its report format and comparisons are unchanged. The rendering fixture deliberately supplies all
rows in one synthetic page. It measures rendering independently of pagination.

## Chat session pagination

`chat-session-pagination.spec.ts` uses the production UI with deterministic
synthetic HTTP/WS data: 500 active chats, 1,500 archived chats, and an 8 KiB
last-message body per chat. It checks all three list entries, fetching the next
page, archive-on-open, old-session deep links, and an empty active list with an
archive. Build with `NEXT_PUBLIC_API_URL=''` so browser API calls stay on the
fixture origin; unexpected origins are blocked and fail the run. Each scenario attaches the exact chat request paths, row counts,
uncompressed JSON bytes, and an illustrative readiness sample. No latency or
bandwidth delay is injected. These browser timings are not backend benchmarks.

```bash
PLAYWRIGHT_BASE_URL=http://localhost:3000 pnpm exec playwright test --config=playwright.perf.config.ts chat-session-pagination.spec.ts
# Run the same fixture against main before this change:
CHAT_PAGINATION_BASELINE=1 PLAYWRIGHT_BASE_URL=http://localhost:3001 pnpm exec playwright test --config=playwright.perf.config.ts chat-session-pagination.spec.ts
```

The companion Go benchmark exercises the real handlers and PostgreSQL queries
with 2,000 generated chats (500 active / 1,500 archived). It reports response
bytes, allocations, and handler time for the preserved full-list endpoint,
the active first page, the summary, and the archive first page. Run against an
isolated, migrated local database; the fixture cleans up after itself.

```bash
cd server
DATABASE_URL='postgres://multica:multica@localhost:5432/your_isolated_database?sslmode=disable' go test ./internal/handler -run '^$' -bench BenchmarkChatSessionListPayload -benchtime=3x -benchmem -count=3
```

Pages default to 50 rows (maximum 100) and cap preview content at 256 Unicode
characters. The database selects a visible page before reading preview bodies.
The summary returns counts and unread-message counts by session without message
bodies, so badges do not depend on how many pages are loaded. Archive queries
are disabled while that view is closed; a realtime invalidation does not fetch
an unopened archive. Full messages and stored content are unchanged.

The existing full-list API remains available for installed clients and other
consumers. Roll out the backend before the new Web/Desktop renderer; the new
renderer requires `/api/chat/sessions/page` and `/api/chat/sessions/summary`.
There is no migration or daemon/CLI protocol change. Roll back the renderer
before rolling back the backend.

## What it runs against

The real Next.js production build, the real page, the real shell and CSS, the
real ProseMirror composer. Only HTTP and the WebSocket are synthetic, so there
is no API server, database, daemon, agent or account involved. Nothing disables
animations or trims the DOM: the cost being measured lives in that machinery.

It covers the renderer shared by Web and Desktop. Electron's own main-process
behaviour is not exercised here.

## Running it

Against a frontend you already have running:

```bash
pnpm exec playwright test --config=playwright.perf.config.ts
# PLAYWRIGHT_BASE_URL defaults to http://localhost:3000
# PERF_REPORT_PATH=<file> also writes the JSON report
```

Comparing two refs — this builds and measures each one, in sequence:

```bash
node scripts/perf-compare.mjs --base origin/main --head HEAD --out perf-report
```

The comparison needs Node, pnpm and Chromium. It writes `comparison.json` and
`comparison.md`. Each side stops its server, waits for it to exit and removes
its worktree before the next side starts, and the script exits on its own on
success, on failure and on cancellation.

The ordinary e2e suite ignores this directory; it needs a production build and
a machine that is not busy doing anything else.

The floating-chat lifetime regression runs against the same production build:

```bash
pnpm exec playwright test --config=playwright.perf.config.ts floating-chat-lifetime.spec.ts
```

It checks that closing removes the message list, keeps the real composer and
an in-flight upload alive, shows messages received while closed on reopen, and
survives an interrupted exit. It also checks that the message list's ancestor
is never scaled, and that a reopened window never paints before its message
list does. HTTP and WebSocket traffic is synthetic; no backend is needed.

## When to run it

Nothing runs this automatically. The routine guard is the component test from
#8260, which pins the exact failure mode behind MUL-7227 — a streamed message
replacing the summary's DOM node — and runs with the normal suite.

Run the comparison yourself, or through the **UI performance report** workflow
(`workflow_dispatch`, base and head refs as inputs), when a change touches
summary animation, long lists, transcript rendering or global styles — and
attach the report to the PR. The cost of doing it by hand instead of on every
PR is that a regression outside those areas will not be caught automatically.

## What it costs

On a GitHub-hosted `ubuntu-latest` runner the comparison took 340.5 s: 291.5 s
for the two production builds, 26 s for the two measurements, the rest install
and startup — plus checkout, dependency and Chromium setup around it. The job is
capped at 15 minutes. Locally, a repeat comparison whose builds come out of the
turbo cache takes under a minute; the report says when a build was restored.

## Reading the result

`typing_elapsed_ms` is the primary number: how long the user waits from the
first keystroke until the editor holds the whole string, main-thread queueing
included. `recalc_style_ms`, `layout_ms`, `task_ms` and the long-task counters
say where the time went.

Two things are not the same as a fast run, and the scenario reports them as
`invalid` or `timeout` rather than as a duration: messages that never reached
the UI, and a composer that did not end up holding every character. An empty
page types very quickly.

One sample per ref. Read a small difference as noise until a second run says
otherwise. Locally the spread across three runs of one build was around 1%; on
CI, comparing two refs with no product difference between them came out 2%
apart on typing and on style recalculation. A threshold needs more CI samples
than that before it can be trusted.

## Changing the fixture

`fixtures/typing-under-live-runs.ts` generates every byte the scenario feeds the
product, and the report carries the fixture version and a digest of it. Bump
`FIXTURE_VERSION` whenever the generated bytes change — two runs are only
comparable when the digests match, which the comparison report states.

Do not add DOM to the page to keep the pressure up. A future virtualization
change that legitimately renders fewer nodes should show up as fewer nodes.
