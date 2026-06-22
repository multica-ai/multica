# Deep-link from a multica issue conversation to the csc session in CoStrict

**Date:** 2026-06-21
**Status:** Design — pending implementation plan

## Goal

After an agent conversation runs on a multica issue, the user should be able to
open that exact csc conversation session inside the CoStrict (costrict-web)
platform, in the workspace where the agent did its work, and see the same
transcript.

multica runs embedded as an iframe inside costrict-web. The link must navigate
the parent window, not the iframe.

## Background: how the pieces connect

The investigation across the four repos established that the **csc session id is
the same identifier everywhere** — no translation layer is needed.

- **csc** (`/Users/linkai/code/csc`) generates a session id via `randomUUID()`
  and persists each session's transcript at
  `~/.claude/projects/<sanitized-cwd>/<sessionId>.jsonl`.
- **multica** (`server/`) spawns the `csc` CLI (stream-json backend, see
  `server/pkg/agent/csc.go`) for an agent run, and stores the returned
  `session_id` plus `work_dir` on the task row
  (`multica_agent_task_queue.session_id` / `.work_dir`, migration 020).
- **cs-cloud** (`/Users/linkai/code/cs-cloud`) runs `csc serve`, proxies
  `/conversations` → `/session`, and maps `sessionID → cwd`. The "conversation
  id" costrict-web uses **is** the csc session id.
- **costrict-web** (`/Users/linkai/code/costrict-web/portal/packages/app-ai-native`)
  embeds multica at `/multica?embedded=opencode&preferred_email=...` and has a
  session deep-link: **`/workspace/<workspaceID>?session=<sessionId>`**. It
  already listens for `postMessage({ type: "multica:navigate", href })` from the
  iframe — but the handler currently only `console.log`s.

### Confirmed deployment assumption

The multica daemon, cs-cloud, and csc run on the **same device** and share the
**same `~/.claude/projects` store**. Therefore a session created by multica's
csc CLI run is discoverable on disk by cs-cloud's `csc serve`, and openable in
costrict-web by its `session_id`.

## Data flow

```
multica issue → agent run → daemon spawns csc → csc writes
   ~/.claude/projects/<workDir>/<sessionId>.jsonl
   (session_id + work_dir stored on multica_agent_task_queue)

User clicks "View in CoStrict" on a run in multica's execution log (embedded)
   → multica posts to parent window:
        { type: "multica:navigate", target: "session", sessionId, workDir }
   → costrict-web matches workDir against its workspaces' directory paths
        (longest-prefix match)
   → resolves workspaceID, navigates to /workspace/<workspaceID>?session=<sessionId>
   → costrict-web opens the session tab; csc serve reads the SAME .jsonl
```

The `session_id` is identical end to end. No new id-mapping table, no API
round-trip to resolve the session.

## Key decisions (settled during brainstorming)

1. **Topology:** same device, same csc store. Linking by `session_id` works.
2. **Workspace resolution:** the parent (costrict-web) resolves the workspaceID
   from `workDir`. multica never needs to know costrict workspace UUIDs.
3. **Link placement:** per agent-run, in the execution log
   (`ExecutionLogSection` / `InlineTranscriptPanel`).
4. **Visibility:** embedded-only. The link renders only when multica runs inside
   costrict-web (detected via `window.desktopAPI.coStrictToken` and/or
   `?embedded=opencode`). Standalone multica hides it — there is no parent frame
   to handle the navigation.
5. **`workDir` matching:** longest-prefix match against workspace directory
   paths, so agents running in a subdirectory/worktree of a workspace still
   resolve.

## Changes

### Change 1 — multica backend (`server/`)

Expose the csc session id on the UI-facing task response. The column already
exists; it is simply not serialized today.

- `server/internal/handler/agent.go`:
  - Add `SessionID string \`json:"session_id,omitempty"\`` to
    `AgentTaskResponse`.
  - In `taskToResponse` (currently returns `WorkDir` but not the session id),
    populate `SessionID` from `t.SessionID`, guarding `.Valid`.
- Test: a handler/serializer test feeding a task row both with and without
  `session_id`, asserting the field is present when set and omitted when not.
  This fails closed if a future refactor drops the field (per the repo's API
  Response Compatibility rules).

### Change 2 — multica frontend (`packages/`)

- `packages/core/types/agent.ts`: add `session_id?: string` to `AgentTask`
  (next to the existing `work_dir?`). Optional — older backends omit it.
- **Embedded detection helper** in `packages/core/platform/` (e.g.
  `is-embedded.ts` exporting `isEmbeddedInCostrict()`): returns true when
  `window.desktopAPI?.coStrictToken` is present and/or the page was loaded with
  `?embedded=opencode`. Single source of truth for the embed check.
- **Navigate bridge helper** in `packages/core/platform/` (e.g.
  `costrict-bridge.ts` exporting `postCostrictNavigateToSession({ sessionId, workDir })`):
  calls
  `window.parent.postMessage({ type: "multica:navigate", target: "session", sessionId, workDir }, "*")`.
  (core forbids `next/*` and `react-router-dom`, but `window.parent.postMessage`
  is platform plumbing that matches the existing parent contract — allowed.)
- **UI** in `packages/views/issues/components/execution-log/inline-transcript-panel.tsx`
  (and/or the run row in `execution-log-section.tsx`): render a "View in
  CoStrict" link/button when
  `isEmbeddedInCostrict() && task.session_id && task.work_dir`. Clicking calls
  the navigate bridge helper. Use shadcn button/link styling and semantic
  tokens.
- **i18n:** add the new string(s) to `packages/views/locales/` (en + zh),
  following the glossary in
  `apps/docs/content/docs/developers/conventions*.mdx`.
- Test (`packages/views/`, jsdom): the button renders only when embedded and
  both fields are present; clicking posts the expected message (mock
  `window.parent.postMessage`). Mock `@multica/core` per the repo conventions.

### Change 3 — costrict-web (separate repo: `/Users/linkai/code/costrict-web`)

This is documented here as the **parent-side contract**; implementation lands in
the costrict-web repo as a separate change, not in this worktree.

In `portal/packages/app-ai-native/src/pages/multica/multica-page.tsx`, upgrade
the existing (no-op) `multica:navigate` handler:

- Keep the existing `event.origin` validation.
- When `event.data.target === "session"`:
  - Read `sessionId` and `workDir` from the message.
  - Find the workspace whose `directories[].path` is the longest prefix of
    `workDir` (exact match preferred).
  - If found, `navigate(/workspace/<workspaceID>?session=<sessionId>)` (the app
    already restores a session from the `?session=` query param).
  - If no workspace matches, show a toast (e.g. "Open this folder as a workspace
    in CoStrict first") instead of navigating.
- Backward compatible: messages without `target` keep the current logging
  behavior.

**Interface contract (the message multica emits):**

```ts
{
  type: "multica:navigate",
  target: "session",
  sessionId: string,   // csc session id (UUID)
  workDir: string      // absolute path csc ran in
}
```

## Edge cases

- **No session yet** (task queued/running before csc reports a session id):
  link hidden because `session_id` is absent.
- **`workDir` matches no workspace** (workspace not enabled, or path not
  registered in costrict-web): parent shows a toast; no navigation.
- **Not embedded:** link never renders.
- **Shape/enum drift:** multica's additions are `omitempty`/optional; the parent
  guards on `target` and the presence of fields. No hard dependency on a single
  boolean.

## Testing

- Go: serializer test for `session_id` (present/absent).
- views (jsdom): conditional render + postMessage payload.
- costrict-web (its own suite): `workDir` → workspace resolution unit test.
- Manual end-to-end: run an agent on an issue inside the embedded multica, click
  "View in CoStrict", confirm the same session opens in the matching costrict
  workspace and shows the transcript.

## Out of scope

- Generating a shareable absolute URL for non-embedded multica (no parent frame
  to navigate; would require multica to know the costrict-web base URL).
- Any change to how multica spawns csc or how transcripts are stored.
- Linking chat sessions (`multica_chat_session`) — this design covers issue
  agent-runs only, per the chosen placement. The same bridge helper could be
  reused later for chat with no protocol change.
