# Desktop Side conversations and resource workspace

## Delivered behavior

Side conversations use the same right workspace as folder, file and web previews. The existing main transcript, session list, native Chat and turn Fork remain separate.

- Select transcript text in one turn, then choose **Ask in side**. A saved Side draft opens with the quotation and a source link. Selecting text does not submit a model request.
- Open a queued prompt's menu and choose **Run in side**. The prompt leaves the main queue and starts in an independent Side. The current main execution continues.
- Open the right workspace and use **+ → New side conversation**, **Open folder**, **Open file** or **Open webpage**.
- Tabs can be selected with the keyboard, closed, resized and expanded. Newly selected tabs scroll into view. On narrow detail panes, the resource workspace takes focus and offers **Return to main chat**.
- Side conversations stay in the right workspace and are excluded from the left session list, search, archives and project counts. Ordinary Fork conversations remain in the left list. Closing a resource tab does not delete its conversation. Saved tab layout (open/closed state, tabs, selected tab and file selection) is scoped to account, workspace and main conversation. Switching conversations restores that conversation’s layout synchronously; the previous account/workspace-wide layout is not imported because it has no conversation owner. The new-conversation page starts closed and empty on every visit and its temporary layout is not persisted. A Side created from that page is attached to the newly saved main conversation before navigation. Panel width remains a shared preference; expansion, dialogs and loading/error indicators reset when switching conversations.
- Source location opens the source conversation and scrolls to the selected turn/quotation, using the existing locator for virtualized history.

A Side inherits its source provider (Codex or Claude Code), working directory and project. Its model can be selected independently. The Side provider selector is read-only: a provider change must not detach a Side into an ordinary session and silently drop its source context. Create a Side from a source using the desired provider instead.

Browser tasks prefer the built-in Side browser, especially for a page already open on the right or its existing login session. This is a recommendation: external browsers and automation tools may be used when the user requests them or they better fit the task, under the selected tool's permissions and confirmation requirements. Side tools remain scoped to the current conversation's open Side tabs; other browsers may use different login sessions.

## Request and state flow

```text
Selection / right +
  → host.openSide / sideCreate
  → persist independent local conversation + source snapshot
  → right Side tab → existing Agent GUI
  → session.send → existing local provider runner

Queued prompt
  → pause and persist main queue
  → sideCreate(requestId, pending prompt, files, references, model)
  → persist main queue removal
  → resume remaining queue
  → host.startSide → idempotent session.send
  → clear pending after its accepted turn exists

File / folder
  → native picker or source working directory
  → canonical root validation → previewList / previewRead
  → shared read-only resource tab

Web
  → validated HTTP(S) address
  → scoped native WebContentsView
  → title/history/loading/error events → resource toolbar
```

`sideCreate` saves the source messages through the selected turn. Side starts with its own empty visible transcript and no inherited native provider session ID. The first native request receives the bounded source snapshot as quoted data. Follow-up requests resume the Side's own provider session. Parent content created after the snapshot is not inherited automatically. Built-in Multica MCP remains available for explicit earlier-context queries. Nested Side snapshots retain their parent context.

The main and every Side use separate GUI storage namespaces, so their queue, draft and model state do not overwrite one another. Mounted Side editors retain unsent input across tab changes, panel closure and main-conversation switches. File opens, native picker results and Side creations update the layout of the conversation that initiated them, even if it is no longer selected; late completions do not navigate away from the current conversation. Native webpage views are released on a main-conversation switch and recreated from the saved URL on return. Unsaved composer text is not guaranteed across an application restart; saved conversations, turns, source metadata and workspace tabs are durable.

## Queue ownership and recovery

A queued message has a stable request receipt. The Side receipt includes the source session and original client submit ID. The backend rejects replay of that transferred request on the main conversation and returns an accepted Side turn for a duplicate submission. A failed initial persistence removes only the unaccepted turn and message, allowing a safe retry.

The queue pauses before Side creation; creation persists before main removal; removal persists before Side execution. If execution cannot be confirmed, the saved Side exposes **Run transferred message**. Reloading an old main queue removes entries whose ownership has already transferred. No automatic model retry is inferred from a disconnected renderer.

## Preview boundaries

A folder selection authorizes that canonical folder; a file selection authorizes only that file. Symlink and traversal escapes are rejected. Grants are stored under the current account/workspace. Source working directories also authorize their contained resources.

Preview supports text/code with line numbers and line-link highlighting, images/SVG, PDF, and sandboxed HTML preview/source. Files over 10 MB and unsupported binary formats report an error. Text reads stop at 1 MB; code displays at most 5,000 lines and labels truncation. Code is a plain monospaced read-only preview, not an editor or syntax-highlighting workspace.

Transcript Markdown recognizes bare `file://` addresses, absolute paths, relative file paths, and standalone inline-code paths as clickable resources. Authored Markdown links, angle autolinks, and link references share this route. Existing workspace resource links retain their internal navigation; webpage and local file links open the right workspace, including keyboard activation. Code fences and inline shell commands remain unchanged. Local URLs are decoded once, retain source line locations, and pass through the existing source-folder authorization and canonical-path checks. Codex and Claude display instructions recommend descriptive Markdown links with absolute local paths instead of launching the browser automatically.

Right-click a local file text link to use **Copy Path** or **Open in Finder** on macOS (**Show in Folder** elsewhere). The GUI forwards the authored href and source session through the Desktop bridge; the main process resolves relative paths against that session's working directory and validates the canonical location with the existing folder grants. These actions use metadata rather than preview reads, so large DMG/ZIP files are supported. Missing or inaccessible targets report an error without opening a menu. Image and editable targets retain their existing native menus.

Webpages load as independent native views, including pages that disallow iframe embedding. They have no application preload, Node integration or desktop bridge. Permissions are denied and navigation is restricted to HTTP(S). Views are hidden while menus/dialogs/resize gestures cover them and are disposed when tabs close or the account changes. Browser login state is separate from Multica's own renderer.

## Import Chrome login state

Open a Side webpage, choose **Import Chrome**, select a local Chrome profile, then choose **Import**. On macOS, allow the Chrome Safe Storage Keychain request if prompted. The dialog reports imported, skipped and failed Cookie counts, or a cancellation, authorization error or empty result. It remains open after an error so the selection can be retried.

This supports Google Chrome Stable profiles at the standard macOS location only. It imports Cookies, not bookmarks, passwords, extensions or Local Storage; some sites will still require signing in again. Expired, damaged and partitioned Cookies are skipped by the existing macOS source adapter. Windows, Linux, Dia and custom Chrome data directories are outside this Side integration.

Cookies merge into the current account/workspace's existing Side webpage session: matching domain/path/name entries are replaced and unrelated Cookies are retained. Multica's own login session and other account/workspace sessions are excluded. Every open Side webpage sharing the target session refreshes after a nonzero import. Source files and Cookie values remain in main; the renderer receives profile display metadata and aggregate results only.

Cancellation, closing the target tab or disposing the preview service aborts preparation before any write. Once writing begins, the import finishes rather than promising rollback; a cancellation request at that point keeps the dialog open until the result arrives. Only one import may run in a workspace at a time. Repeating an import updates the same matching Cookies. Disk-flush failures are reported explicitly because the imported Cookies may not survive restart.

The added preload/main IPC methods share the existing main-frame and scope checks. The host injects the macOS source adapter into `ResourceWebPreview`; `resource-chrome-profile-import.ts` owns request validation, cancellation and writes. No server API, database migration, local conversation history format or session partition key changes are required.

Fixture verification (no real Chrome profile, Keychain access or model calls):

```sh
pnpm --filter @multica/desktop exec vitest run src/main/resource-chrome-profile-import.test.ts src/main/resource-web-preview.test.ts src/renderer/src/pages/chrome-profile-import.test.tsx src/renderer/src/pages/local-side-workspace.test.tsx
node --test vendor/tutti/packages/browser/workbench-node/src/chrome-cookie-import/macos/chromeCookieImport.test.ts vendor/tutti/packages/browser/workbench-node/src/chrome-cookie-import/macos/macosChromeCookieImportAdapter.test.ts
MULTICA_RUN_NATIVE_PREVIEW_SMOKE=1 pnpm --filter @multica/desktop exec electron scripts/side-preview-smoke.cjs
```

With a desktop renderer Vite server running, `e2e/chrome-profile-import.spec.ts` mounts the production webpage preview against fixture IPC. It verifies selection, counts, scope wiring, hiding/restoring the native view, Escape/focus restoration and overflow in light/wide and dark/narrow layouts. Its screenshots are saved under `.tmp/chrome-profile-design/`. Real website login continuity and interactive Keychain authorization require manual acceptance on macOS.

## Code boundaries

- `packages/tutti-agent-gui-host/src/side-entry.tsx`: selected-text toolbar, resource links, source locator, queue menu action.
- `packages/tutti-agent-gui-host/src/runtime/create-multica-agent-gui-runtime.ts`: queue transfer and ownership reconciliation.
- `packages/tutti-agent-gui-host/src/side-context.ts`: bounded quoted source context.
- `apps/desktop/src/local-host/local-conversation-service.ts`: durable Side creation, receipts and execution.
- `apps/desktop/src/local-host/local-resource-reader.ts`: canonical local file grants.
- `apps/desktop/src/main/resource-web-preview.ts`: isolated native webpage lifecycle.
- `apps/desktop/src/renderer/src/pages/local-side-workspace.tsx`: shared resource tabs and existing Agent GUI composition.
- `apps/desktop/src/renderer/src/pages/local-resource-preview.tsx`: folder tree, file preview and webpage toolbar.

The source-vendor GUI remains unchanged; existing pinned compile adapters inject narrow turn/queue hooks. Desktop framing uses Multica's shared Button, IconButton, Dialog, menu, input and resizable primitives and semantic typography/surface tokens.

## Validation

Desktop unit/component scope: 76 tests across resource reading, local service, parent-service routing, flow composition and shared workspace. Host unit/runtime scope: 57 tests. The final GUI regression suite covers queue transfer, quoted Side entry, empty Side, existing Fork, provider drafts/references, HTML/SVG rendering and mention panels. The final combined run passed all 25 tests; see `../../.tmp/side-all-final.log`. The production workspace regression also checks keyboard resizing, overflow tabs, expansion, restoration, closing, narrow mode, draft preservation and zero page errors.

```sh
pnpm --filter @multica/desktop test src/local-host/local-resource-reader.test.ts src/local-host/local-conversation-service.test.ts src/renderer/src/platform/local-conversation-service.test.ts src/renderer/src/pages/local-conversation-flow.test.tsx src/renderer/src/pages/local-side-workspace.test.tsx
pnpm --filter @multica/tutti-agent-gui-host test
pnpm --filter @multica/desktop typecheck
pnpm --filter @multica/tutti-agent-gui-host typecheck
pnpm --filter @multica/tutti-agent-gui-host build
pnpm --filter @multica/desktop build
PLAYWRIGHT_BASE_URL=http://localhost:16099 pnpm exec playwright test e2e/local-side-workspace.spec.ts e2e/local-conversation-queue.spec.ts e2e/local-turn-fork.spec.ts e2e/local-provider-gui.spec.ts e2e/agent-html-preview.spec.ts e2e/tutti-mention-panel.spec.ts --output=.tmp/side-e2e-all-final
MULTICA_RUN_NATIVE_PREVIEW_SMOKE=1 pnpm --filter @multica/desktop exec electron scripts/side-preview-smoke.cjs
```

The native webpage smoke uses a temporary profile and owned localhost fixture, verifies no Node/process/desktop API, view bounds, history, scope checks and close/dispose. It performs zero model calls. Desktop CUA acceptance checks saved Side creation, quote/source location, input retention, file tree/code and native webpage rendering in light/dark. Current evidence and visual comparison are recorded in project-root `design-qa.md`.

Default tests inject fake provider executables/runners; they do not discover or execute user-installed model CLIs. No real Codex or Claude inference was performed in this Side acceptance. Windows/Linux native acceptance, signed installers, comprehensive screen-reader coverage and arbitrary third-party webpage compatibility are outside this validation.


## Layout regressions fixed during acceptance

The resizable library applies an inline flex display to panels, overriding the native `hidden` display rule. A scoped important hidden rule now removes hidden panels from layout. Separators are mounted only when both panels are visible; keeping a separator registered while its panel is hidden otherwise leaves the library with no matching panels when keyboard resizing resumes. The regression test first reproduced that error and now verifies resize, expand, close and restore behavior in a real browser using the production workspace component. Side iframe panels themselves stay mounted.

The new empty-state wrapper also needs a flex column so the existing GUI fills its available height. The native acceptance caught a top-aligned composer before this correction; final evidence confirms a bottom dock. The floating welcome carousel is a sibling of its hero body, so Side-specific hiding covers both without changing the main welcome artwork.

ESLint completed with zero errors. Two existing host hook warnings remain in `application.tsx` and `composer-footer-controls.tsx`; the scoped desktop lint is clean. Desktop build retains the existing mixed static/dynamic emoji-picker import warning. The final managed desktop restart restored both Side tabs and the saved folder/file preview; the original follow-system theme was restored.


## Selected-text toolbar polish

The floating toolbar originally declared a border width without a border color, so it inherited the text foreground. In dark mode this produced a bright white outline. A browser regression reproduced the same inherited border color in both themes before the fix.

The toolbar now uses Multica surface-border, raised-surface, radius and menu-shadow tokens, shared extra-small ghost buttons, and 14px icons. The Side action uses the same conversation icon as the Side tabs. Placement measures actual toolbar dimensions instead of assuming a 250px width, keeps an 8px viewport margin and flips below the selection when there is insufficient space above. Resize updates placement.

Validation: all 11 cases in `e2e/local-conversation-queue.spec.ts` pass, including light/dark toolbar styling, narrow-window bounds, Escape dismissal, quoted Side entry and queue transfer for both providers. Host typecheck, scoped ESLint and build pass. Screenshots are `.tmp/side-design/selection-toolbar-light.png` and `selection-toolbar-dark.png`. No real model calls are used.

## Per-conversation layout regression checks

The workspace component tests cover A → B → A isolation, independent open/closed state and selection, remount restoration, legacy-layout exclusion, fresh new-conversation pages, delayed file/picker/Side results, missing conversation summaries, retained Side input and native webpage close/reopen calls. The flow test checks route wiring and confirms that the main editor remains mounted.

```sh
pnpm --filter @multica/desktop test src/renderer/src/pages/local-side-workspace.test.tsx src/renderer/src/pages/local-conversation-flow.test.tsx
```

These tests use fake desktop clients and do not execute model CLIs. Native webpage rendering still requires the Electron smoke/desktop acceptance described above.
