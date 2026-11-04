# Tutti Agent GUI host

This package mounts the Tutti Agent GUI, vendored and owned in `vendor/tutti`,
directly in the app's document. `src/mount.tsx` is one instance: its runtime lives
as long as the component, and its styles apply only under its root element. Apps
load it on demand, and server bundles leave it out. Multica owns the session list
and header around it, and hides the redundant embedded provider and session rails
with host-scoped CSS. The original Chat page keeps its styles and navigation.

The host's `multica-theme.css` and `multica-components.css` adapt the intact
source UI to Multica's shared semantic tokens, fonts and control treatments.
They do not replace the source transcript or composer. The host also mounts
Agent and model selection through the public composer footer accessory slot.
Inter, Source Serif and Geist Mono load with the GUI. Both theme attributes and
the `.dark` class follow the authenticated host snapshot, on each instance's root
and on the container overlays portal into (`getPortalContainer()`). Nothing goes
on `<html>`, whose `data-theme` the app's own tokens respond to.
The build reads the public Multica token stylesheet and generates an ignored
`.multica-tokens.css` with stronger root selectors. Token values stay unchanged
and have one maintained source. This avoids nesting upstream Tailwind utility
directives inside a cascade layer while retaining the source style order.

The workspace's Conversations navigation entry opens `/conversations` in its
own application tab on desktop. Its page owns selection through URL parameters
and does not write the native Chat's active-session pointer. The `/chat` page
retains its original UI. A cold GUI without a selected session or agent starts
with a real callable, runtime-bound agent when available; explicit selections
are never replaced by that default.

The parent session list reuses Multica's native Chat components with explicit
Conversations links and successful-deletion callbacks. Changing Agent selects
a fresh conversation scope; the old draft remains with its original scope.
For local Codex / Claude Code providers, the complete current draft also follows
the provider selection, including references and attachments. Switching again
on the home composer keeps the same draft; selecting another saved conversation
still restores that conversation's own draft.
Model discovery uses the selected Agent's runtime catalog. A model preference
is stored per session or draft and is sent as an optional model override on the
next turn; it does not update the Agent's default. See
`docs/engineering/conversation-flow-two-pane.md` for the execution boundary.

## Build and serve

```sh
pnpm --filter @multica/tutti-agent-gui-host build
```

This runs `scripts/prepare.mjs`, which generates what no bundler can produce the
same way on both apps: `src/agent-gui.generated.css`, the GUI's own Tailwind output
with every rule limited to `.multica-agent-gui` and `html`, `body` and `:root`
rewritten to that element; and the HTML preview's libraries. The desktop renderer
Vite hook and Web dev/build scripts run it automatically. Generated files are
ignored. A stylesheet change needs a rerun or an app restart.

The composer mention categories are limited to Sessions, Tasks and Agents: the
vendored tab order and English/Chinese composer hint say so, so empty browsing,
search results and keyboard cycling use the same three categories. The public hidden-category
setting alone does not cover the source's search-result tabs or keyboard order.
The application passes a stable hidden-category array through that public
setting to preserve the source search controller across composer updates.
This category policy does not disable the separate attachment upload control.

The scroll-to-bottom affordance uses the actual timeline end position, with
one CSS pixel of rounding tolerance, independently of automatic follow mode.
The host measures the scroll container's DOM geometry for both ordinary and
virtual transcripts, including the virtual list's total-size spacer, because
the virtual controller's cached end state can lag behind content resizes.
Scroll, content and viewport resizes refresh the affordance without resuming
follow mode or moving a detached reader.

The preparation step's cache covers host source, shared Multica token styles, the
vendored packages and the lockfile; concurrent Web/desktop startup shares a lock.
It publishes Tutti's LICENSE and NOTICE, UI System's NOTICE, and the Chart.js and
D3 licenses to each app's `public/licenses/agent-gui/`.

## Host contract

`createInProcessRpcClient` connects a GUI to its host in the same document. Calls
stay asynchronous and copy their data, so the two sides never alias objects and a
host event cannot re-enter a runtime operation in progress. Requests use exact
Multica workspace, session, task and attachment identities. Abort stops awaiting a
request and never retries a send. Unmounting rejects outstanding calls. A hidden
or unfocused instance cannot act for the user: other than reads and admitted queue
items, its requests fail with `inactive_host`.

Local conversations are read through an injected `LocalConversationSource`; the
runtime only projects them, and they outlive it.

`e2e-harness/` is a test rig that drives the GUI over a MessagePort with fake data.
Neither app ships it. Build it with `build-e2e-harness.mjs`.

Composer append requests wait through startup,
are applied sequentially by the source composer, and are acknowledged only
after its public completion callback. Existing attachment IDs are reused.
Legacy Multica Task links keep their original `mention://issue/<id>` protocol
and open the existing issue page through the host, including source pointer and
keyboard activation.

An unsent source composer draft belongs to its mounted GUI. Background
visibility/focus changes keep it mounted. Routing away or switching a
desktop tab unmounts it; restoring an unsent source draft after that unmount
is not currently supported by a public source draft persistence port. Multica's
durable cancellation restore still uses its existing host pipeline and ACK.

The source `AgentGUI` public entry provides its own UI/runtime/i18n providers.
The runtime adapter in `src/runtime/` owns semantic projection into the source
activity engine. Multica remains the execution and persistence authority.
Unimplemented execution capabilities are declared unsupported. Local filesystem
operations reject explicitly; browser drops use the real Multica attachment
upload API, preserving source per-file error states and the existing upload
size limit. Agent configuration changes generate real source composer-default
invalidation events through the public host event seam.

## Validation

```sh
pnpm --filter @multica/tutti-agent-gui-host typecheck
pnpm --filter @multica/tutti-agent-gui-host test
make env-exec ARGS='-- pnpm exec playwright test e2e/tutti-agent-gui.spec.ts'
```

The end-to-end test uses a test-created runtime with no daemon attached. It must
not execute an installed Agent CLI or consume a model account.

All file publication uses Node path/filesystem APIs and relative browser asset
URLs. No POSIX-only copy command or platform-specific filesystem path is used.
The available validation runs on macOS; packaged Windows launch remains a
platform validation gate.
