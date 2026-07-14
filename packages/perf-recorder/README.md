# @multica/perf-recorder

Dev/Profiling-only frontend performance flight recorder for Multica Web & Desktop
renderers. See the full RFC in **MUL-4466**.

Start recording → interact with the app → stop → get a list of **Incidents**
(slow React commits, slow resources, long tasks / dropped frames) with an
exportable JSON report. **Production never loads, requests, or exposes it.**

## Design constraints (hard gates)

- **Browser-only & self-contained.** No imports from Multica domain model, stores,
  query client, router, or business components. Only `bippy` (pinned) + browser APIs.
- **Hook before React.** React commit timing comes from bippy patching
  `window.__REACT_DEVTOOLS_GLOBAL_HOOK__`; React only reports to the hook that
  exists when `react-dom` first evaluates, so the hook install must precede it
  (Desktop uses a two-stage bootstrap — see below).
- **Standard mode, no content snapshot.** Collectors never read `innerText` /
  `value` / props / state / request bodies. Resource URLs are stripped of
  query/hash/credentials at the collector entry. `MutationObserver` contributes
  only a count. The panel is plain-DOM in a Shadow root, so it never observes
  itself.

## Entry points

| Export | Use |
| --- | --- |
| `@multica/perf-recorder` | ESM API: `installRecorderHook()`, `createRecorder(config)` |
| `@multica/perf-recorder/install` | `installRecorderHook()` only — no react-dom, evaluate before React |
| `@multica/perf-recorder/auto` | self-mounting; source of `dist/auto.global.js` |
| `dist/auto.global.js` | self-executing IIFE (bippy inlined) for the eventual `<script src>` |

## Host wiring (in-repo, dev-only)

**Desktop** (`apps/desktop/src/renderer/src/main.tsx`) — two-stage bootstrap:
install the hook, *then* dynamically import `./app-bootstrap` (which owns the
`react-dom` import). Proven by `perf-recorder-loader.test.ts`.

**Web** (`apps/web/app/layout.tsx`) — server-gated `beforeInteractive` `<Script>`,
same pattern as `react-grab`. In the in-repo phase, copy the built global to the
served path:

```bash
pnpm --filter @multica/perf-recorder build
cp packages/perf-recorder/dist/auto.global.js apps/web/public/__perf-recorder.global.js
```

Both are opt-in per developer via `VITE_PERF_RECORDER` in a local, gitignored env
file, and both branches are DCE'd out of Production builds.

## Extraction (later, per RFC §6.3)

Move this package to its own repo, publish, and swap the host references from the
local path to `//unpkg.com/<pkg>/dist/auto.global.js` — the dev-only gate and load
order do not change. Extraction is "move code + change reference", not a redesign.

## Verify

```bash
pnpm --filter @multica/perf-recorder typecheck
pnpm --filter @multica/perf-recorder test
pnpm --filter @multica/perf-recorder build
```
