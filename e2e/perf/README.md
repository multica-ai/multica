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
its report format and comparisons are unchanged. Session data is still fetched
in full: these checks measure rendering, not API pagination or network savings.

## Agent recent work

`agent-recent-work.spec.ts` opens the real agent detail page, reveals 1,000
synthetic terminal runs using the existing Show more action and 200-run API
pages, then verifies a bounded mounted window, the last run's transcript,
Escape dismissal, and dialog survival when its opener is recycled. It covers
wide and compact layouts with the page's existing scroll owner. HTTP and
WebSocket traffic are intercepted; unexpected requests and page errors fail
the scenario. No server, daemon or real agent runs are involved.

Build with a same-origin API URL so all requests reach the synthetic boundary:

```bash
NEXT_PUBLIC_API_URL='' pnpm --filter @multica/web build
NEXT_PUBLIC_API_URL='' pnpm --filter @multica/web exec next start --port 3000
PLAYWRIGHT_BASE_URL=http://localhost:3000 pnpm exec playwright test --config=playwright.perf.config.ts agent-recent-work.spec.ts
```

For a comparison, run the same test against an unmodified main production
build with `AGENT_WORK_BASELINE=1`. Both sides keep API pagination; the baseline
asserts that all 1,000 revealed rows stay mounted while the new renderer keeps
fewer than 50. JSON attachments report the observed row counts and cursors;
these are rendering checks, not backend or network-throughput benchmarks.

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

The image-heavy thread scenario (MUL-8182) types a comment while a long thread
of large screenshots loads in the viewport. Image responses are held until the
first keystroke, so fetching, decoding and laying out the images all land in
the measured window, which runs with the CPU throttled 4× — a fast workstation
absorbs this page without dropping a frame. It reports keydown-to-next-frame
p95/max, keystrokes over 16.7 ms, long tasks, layout shift, mounted images,
and fetched bytes. This frame measurement is not INP. It shares the report path with the live-runs scenario, so it only
runs when selected:

```bash
PERF_SCENARIO=image-thread pnpm exec playwright test --config=playwright.perf.config.ts image-heavy-thread.spec.ts
node scripts/perf-compare.mjs --base origin/main --head HEAD --out perf-report --scenario image-thread
```

For a review comparison, set `PERF_SCREENSHOT_PATH` to a real 2880×1800 PNG
(kept locally, outside Git; `PERF_SCREENSHOT_ALT_PATH` optionally alternates a
second screenshot), `PERF_IMAGE_MODE=known|missing|none`, and
`PERF_IMAGE_POSITION=start|loaded`. All modes keep the same 37 comments.
The `loaded` position scrolls through every image before typing at the thread
bottom. The `missing` mode omits dimensions to represent existing attachments;
its 16:10 placeholder can shift when the true aspect ratio becomes known.
`none` is the no-image control. Defaults use generated pixels for CI.

Use `PERF_REPEATS=3` with `perf-compare.mjs` to retain every sample in
`base.json` / `head.json` and report the median of each numeric metric.
Use a separate output directory per mode/position.

The floating-chat lifetime regression runs against the same production build:

```bash
pnpm exec playwright test --config=playwright.perf.config.ts floating-chat-lifetime.spec.ts
```

It checks that closing removes the message list, keeps the real composer and
an in-flight upload alive, shows messages received while closed on reopen, and
survives an interrupted exit. It also checks that the message list's ancestor
is never scaled, and that a reopened window never paints before its message
list does. HTTP and WebSocket traffic is synthetic; no backend is needed.

## Issue history with a real local API

`issue-history.spec.ts` measures direct issue entry and Inbox selection on the
production UI against an isolated, migrated local API/database. It seeds and
cleans up its own issues through `TestApiClient`; completed execution rows are
inserted locally without dispatching agents. It never mocks HTTP or WebSocket
responses. Run it explicitly with `PERF_SCENARIO=issue-history`.

Start the API with `make up C=api ARGS="--ephemeral"`. Build each compared ref
with `NEXT_PUBLIC_API_URL='' NEXT_PUBLIC_WS_URL='' pnpm --filter @multica/web build`,
then run its production server with `REMOTE_API_URL` pointing at that API.
Run the **same spec from the head checkout** against each server:

```bash
PERF_SCENARIO=issue-history PERF_REPEATS=5 \
  PLAYWRIGHT_BASE_URL=http://localhost:<production-web-port> \
  PERF_REPORT_PATH=<absolute-report-path> \
  make env-exec ARGS="-- pnpm exec playwright test --config=playwright.perf.config.ts issue-history.spec.ts"
```

Each scenario gets one warmup and five fresh-browser-context samples. The
default datasets have 120 or 716 Markdown replies and 20 completed runs with
64 KiB outputs. An optional `PERF_ISSUE_SOURCE` JSON array of `{content}` adds
a local discussion-text workload; keep private content outside Git and do not
publish its screenshots. Reports contain only counts, byte sizes and a content
digest. Compare digests before comparing samples.

Reports include entry-to-visible-target time, main-thread task duration, long
tasks, live heap after explicit GC, mounted comment bodies, DOM nodes and actual
task-response body bytes (decompressed). CPU is unthrottled. Direct entry
includes application boot; Inbox entry starts with the shell already loaded.
Long tasks are snapshotted before the instrumentation's forced GC. Set
`PERF_DATASET` to dataset names separated by commas or `PERF_ENTRY=direct|inbox` to
repeat a focused case in alternating product order.
These are observed local measurements, not latency thresholds. Stop other
run-owned builds/tests before measuring; the report also records system load.
Destroy the API environment after the comparison with `make destroy ARGS="--yes"`.

## When to run the typing scenarios

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
