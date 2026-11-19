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

## Core paths: big issues and Inbox switching

`core-paths.spec.ts` is the benchmark the performance work (MUL-9539) is
judged by. It measures the two paths people report as slow, on the production
UI against a real local API, with a seeded "large workspace":

- **issue**: open a big issue by URL, scroll its history to the end at a
  steady pace while the images load, then, from the middle of the history,
  toggle the left nav (⌘B) and the right sidebar ten times each. It runs on
  two issues: `large` (300 comments, 80 images, 70 runs, 40 sub-issues) and
  `p99` (54 / 54 / 36 / 38, the non-zero p99 of a production sample).
- **inbox**: click through twenty rows of the Work panel's Inbox, then go back
  and forth between the first five. `PERF_INBOX_ROWS=20` (default) uses the
  largest section limit; `PERF_INBOX_ROWS=all` first presses "Show N more".
  A switch ends when the notified comment is in view; `PERF_INBOX_UNTIL=title`
  ends it at the issue title instead, for refs older than #10872 that do not
  bring the comment into view.

Each step reports **time until the content is on screen** and **stalls**:
frames longer than 50 ms (Long Animation Frames), the longest one, and the
scripts behind the worst. Interactions start at the click event's own
timestamp and end in the frame where the target is laid out in the viewport,
so Playwright's IPC is not in the number. A step whose content never appears
fails rather than reporting a fast time.

The dataset (`lib/core-dataset.ts`) takes its sizes from production
aggregates: ~1,000 issues, ~1,800 notifications of which ~950 unread, and the
issues above. Text, structure and image bytes are deterministic. The report
carries a digest of the generator's source and the image bytes, so any edit to
`lib/core-dataset.ts` gives a new digest, and both comparison scripts refuse
to compare reports from different datasets. The first run seeds a `perf-core`
workspace (about a minute); later runs reuse it until the digest changes.

```bash
make up C=api ARGS="--ephemeral"
NEXT_PUBLIC_API_URL='' NEXT_PUBLIC_WS_URL='' pnpm --filter @multica/web build
(cd apps/web && REMOTE_API_URL=http://localhost:<api-port> pnpm exec next start --port 3000) &
PERF_SCENARIO=core-paths PERF_REPORT_PATH=$PWD/perf/main.json \
  make env-exec ARGS="-- pnpm exec playwright test --config=playwright.perf.config.ts core-paths.spec.ts"
node scripts/perf-core-report.mjs main=perf/main.json [other=perf/other.json ...]
```

`PERF_SCENARIO=core-issue` or `core-inbox` runs one scenario;
`PERF_ISSUES=large` narrows the issue scenario; `PERF_REPEATS` (default 5)
and `PERF_WARMUP` (default 1) set the rounds, each in a fresh browser context.
`PERF_CPU_THROTTLE=4` slows the CPU for the whole sample: an M-series Mac
absorbs work that a mid-range laptop shows as stalls, and a fix that only
helps there (#10767's left-nav toggle) is invisible unthrottled. Sidebar
toggles also report main-thread work per toggle, the most stable of the
numbers.
To compare two refs, run `perf-compare.mjs` inside the API's environment. It
builds each ref in its own worktree, serves it against the local API, runs the
spec from this checkout, then stops it before the next side starts:

```bash
make up C=api ARGS="--ephemeral"
make env-exec ARGS="-- node scripts/perf-compare.mjs --scenario core-paths --base origin/main --head HEAD --out perf-report"
```

`--scenario core-issue` or `core-inbox` measures one path. The `PERF_*`
settings above pass through. It writes `base.json`, `head.json` and
`comparison.md`, whose table is the one `perf-core-report.mjs` prints; attach
it to the PR. The **UI performance report** workflow does not offer these
scenarios, because a GitHub runner has no local API to run them against.

## Desktop: tab switches and Inbox clicks

`desktop-tabs.spec.ts` measures what only Desktop has, in the real Electron
app (production renderer from `electron-vite build`) on the core-path
dataset (MUL-9609). Desktop mounts one tab at a time, so a tab switch
unmounts the outgoing page and mounts the incoming one from the Query cache.

- **tabs**: open six tabs (issue list, Inbox, the `large` issue read down to
  its notified comment, `p99`, a 120-comment and a small issue), then click
  through the tab strip twice.
- **inbox**: the core-path Inbox scenario inside Desktop: click twenty Work
  panel Inbox rows, then go back and forth between the first five.

Each step reports the time from the click until the content is in view, the
next paint (Event Timing), main-thread task time and Long Animation Frames,
plus mounted DOM nodes. Every launch gets a fresh app-data directory and a
temporary HOME with daemon auto-start off, so it never touches an installed
Multica, its CLI profiles or its daemon, and hands the `multica://` handler
back on exit.

```bash
make up C=api ARGS="--ephemeral"
# Build each ref's Desktop against the API and keep its output under apps/desktop.
(cd apps/desktop && VITE_API_URL=http://localhost:<api-port> VITE_WS_URL=ws://localhost:<api-port>/ws \
  pnpm exec electron-vite build && rm -rf .perf-builds/head && cp -R out .perf-builds/head)
PERF_SCENARIO=desktop PERF_REPORT_PATH=$PWD/perf/desktop.json \
  PERF_DESKTOP_TARGETS=main=apps/desktop/.perf-builds/main,head=apps/desktop/.perf-builds/head \
  make env-exec ARGS="-- pnpm exec playwright test --config=playwright.perf.config.ts desktop-tabs.spec.ts"
```

`PERF_SCENARIO=desktop-tabs` or `desktop-inbox` runs one scenario. Rounds
alternate between the `PERF_DESKTOP_TARGETS` builds (default: this
checkout's `apps/desktop/out`); `PERF_REPEATS`, `PERF_WARMUP` and
`PERF_CPU_THROTTLE` work as in the core-path benchmark. `PERF_TRACE_DIR`
writes a Chrome trace and CPU profile of the last round's last tab cycle.
The renderer bundle is not minified, so profiles carry real function names.

## Core paths on Desktop, idle CPU and memory growth

The same core paths run in the Electron app, plus two scenarios only Desktop
has (MUL-9612). `scripts/perf-desktop.mjs` builds Desktop for production
against the environment's API, starts that build with a DevTools port, runs
the specs over CDP with `PERF_PLATFORM=desktop`, and stops the app:

```bash
make up C=api ARGS="--ephemeral"
make env-exec ARGS="-- node scripts/perf-desktop.mjs --out $PWD/perf/desktop"
node scripts/perf-core-report.mjs web=perf/main.json \
  desktop=perf/desktop/core-paths.json desktop=perf/desktop/desktop-resources.json
```

It writes `core-paths.json`, `desktop-resources.json`, `desktop.log` (the
app's output) and `report.md`. `--scenarios` picks a subset (`core-issue`,
`core-inbox`, `desktop-idle`, `desktop-memory`); `--skip-build` reuses
`apps/desktop/out`, which must have been built against the same API port.
`PERF_*` variables pass through. A full run takes about 35 minutes.

- **Core paths** are the steps above, on the same dataset, with the same
  clock and the same report. Each sample clears the renderer's storage and
  HTTP cache, writes a tab layout holding only the measured route and
  reloads: the app boots onto that tab as it does on launch. That is the
  Desktop counterpart of opening a URL in a fresh browser context.
  Every boot also writes English as the language choice, as the web
  context's `locale: "en-US"` does; the specs match English labels, and
  Desktop would otherwise follow the OS language.
- **`desktop-idle`** opens the big issue (300 comments, 70 runs), waits 15 s,
  then touches nothing for `PERF_IDLE_SECONDS` (300). CPU is sampled every
  second per process role: main, renderer, GPU, utility (network, storage).
  100% is one core. The renderer's own main-thread busy time and any stalls
  are reported beside it.
- **`desktop-memory`** clicks through the Work panel's twenty Inbox rows,
  round and round, for `PERF_MEMORY_SECONDS` (600), sampling resident memory
  every 5 s. It reports the growth slope over the whole window and over its
  second half (the first minutes include caches filling), the first and last
  minute, the JS heap at the end before and after GC, and whether switches
  got slower.

CPU and memory come from `ps` (`lib/process-tree.ts`): cumulative CPU time
and RSS of the Electron main process and its descendants, read from outside
the app. Processes without an Electron `--type` under it (the local daemon
Desktop starts, and its agents) are reported as `other` and left out of the
app total. Memory is RSS, not macOS's "memory footprint" in Activity Monitor;
compare it run to run, not with that number.

The app runs unpackaged from `apps/desktop/out` with a production renderer
bundle; packaging, signing and auto-update are not part of it. It gets its own
userData directory (`Multica Canary Perf <env>`) and starts signed out; the
specs sign in as the dataset's user. Keep its window visible: Chromium
throttles a hidden window, so `desktop-idle` fails rather than report one.

`--attach` measures the development Desktop of `make up C=desktop
ARGS="--cdp"` instead and leaves it running. Its React is the development
build: slower, and it records profiling data, so use it only for problems
that exist only there, such as the User Timing leak of MUL-8781:

```bash
make up C=api,desktop ARGS="--ephemeral --cdp"
make env-exec ARGS="-- node scripts/perf-desktop.mjs --attach --scenarios desktop-memory --out $PWD/perf/dev"
```

## Inbox jump

`inbox-jump.spec.ts` opens a notification about a reply deep in a long issue
from the sidebar Inbox and measures the time from the pointer press to the
reply being mounted and on screen, plus long tasks and how many comment bodies
mounted on the way. It runs against production builds and an isolated local
API, as the issue-history scenario does (MUL-9538).

The first run seeds a deterministic issue shaped like GitHub #10775: 49
comments (18 replies, agent reports with tables and code), 72 runs, 24
sub-issues and 20 activity entries, plus a small neighbour issue. It writes
the ids and the user's token to `PERF_MANIFEST`; later runs reuse it, so every
build is measured on the same rows. Each sample:

- **cold**: a fresh browser context loads the Inbox, waits
  `PERF_COLD_SETTLE_MS` (default 1500), then opens the notification. Every
  sample starts from an unread notification.
- **warm**: in the same context, it opens the neighbour, then the long issue's
  notification again.

```bash
make up C=api ARGS="--ephemeral"
# Build each ref with NEXT_PUBLIC_API_URL='' NEXT_PUBLIC_WS_URL='' and serve it
# with REMOTE_API_URL pointing at the API (see the issue-history section).
PERF_SCENARIO=inbox-jump PERF_REPEATS=6 \
  PERF_MANIFEST=<absolute-path>/inbox-jump-manifest.json \
  PERF_TARGETS="main=http://localhost:3101,head=http://localhost:3102" \
  PERF_REPORT_PATH=<absolute-report-path> \
  make env-exec ARGS="-- pnpm exec playwright test --config=playwright.perf.config.ts inbox-jump.spec.ts"
```

Rounds alternate between the `PERF_TARGETS` builds, after one warmup round.
`PERF_LATENCY_MS` adds a round-trip latency to every request, standing in for
a remote API. `PERF_TRACE_DIR` writes a Chrome trace and CPU profile of the
last round, or of every round with `PERF_TRACE_ALL=1`. The trace is bounded
by the `bench:click` and `bench:visible` user-timing marks.

The report holds per-target medians. Budgets on those medians are enforced
only when set: `PERF_BUDGET_WARM_MS`, `PERF_BUDGET_WARM_LONGEST_TASK_MS`,
`PERF_BUDGET_COLD_MS` and `PERF_BUDGET_COLD_LONGEST_TASK_MS`. The MUL-9538
targets are 100, 50, 300 and none. Machine load moves these numbers, and the
report records it, so compare builds from one alternating run, not across
runs.

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
