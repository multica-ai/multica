# Tutti Agent GUI host

This package mounts the Tutti Agent GUI, vendored and owned in `vendor/tutti`,
directly in Desktop's document for its local conversations. `src/mount.tsx` is one
instance: its runtime lives as long as the component, and its styles apply only
under its root element. Desktop loads it on demand. Desktop owns the session list
and header around it, and the embedded provider rail has been removed. Web does not build the GUI, and `/chat` on either platform does
not use it.

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

The workspace's AI Sessions entry opens `/conversations` in its own application
tab on Desktop. Its page owns selection through URL parameters. On Web that path
redirects to `/chat`, keeping the query.

The complete current draft follows the provider selection, including references
and attachments. Switching again on the home composer keeps the same draft;
selecting another saved conversation still restores that conversation's own draft.
Model discovery uses the provider's catalog. A model preference is stored per
session or draft and travels with the queued prompt.

## Build and serve

```sh
pnpm --filter @multica/tutti-agent-gui-host build
```

This runs `scripts/prepare.mjs`, which generates what a bundler cannot produce:
`src/agent-gui.generated.css`, the GUI's own Tailwind output with every rule limited
to `.multica-agent-gui` and `html`, `body` and `:root` rewritten to that element; and
the HTML preview's libraries. The desktop renderer Vite hook runs it automatically.
Generated files are ignored. A stylesheet change needs a rerun or an app restart.

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
vendored packages and the lockfile; concurrent runs share a lock. It publishes
Tutti's LICENSE and NOTICE, UI System's NOTICE, and the Chart.js and D3 licenses to
Desktop's `public/licenses/agent-gui/`.

## Host contract

`createHostServicesConnection` supplies direct, typed service groups in the same
document. Native commands call `ConversationService`; there is no command RPC
dispatcher or structured-clone boundary. Navigation and native file actions live
under `hostApi`; storage stays partitioned by account, workspace and surface.
Calls remain asynchronous. Abort stops awaiting a call and never retries a send.
Unmounting rejects outstanding calls. A hidden or unfocused instance cannot act
for the user: other than reads and admitted queue items, calls fail with
`inactive_host`. Workspace identity is checked before and after asynchronous work.

Local conversations are read through an injected `LocalConversationSource`; the
runtime only projects them, and they outlive it.

`e2e-harness/` is a test rig that drives the GUI over a MessagePort with fake data.
Neither app ships it. Build it with `build-e2e-harness.mjs`, which still writes to
`apps/web/public/tutti-agent-gui` so the existing specs can be served from there.
Specs describe conversations as `session.load` details; only this rig answers that
method.

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
The composition in `src/runtime/` wires the GUI to native conversation
models and services. Multica remains the execution and persistence authority.
Unimplemented execution capabilities are declared unsupported. Local filesystem
operations reject explicitly; browser drops use the real Multica attachment
upload API, preserving source per-file error states and the existing upload
size limit. Agent configuration changes generate real source composer-default
invalidation events through the public host event seam.

## Validation

```sh
pnpm --filter @multica/tutti-agent-gui-host typecheck
pnpm --filter @multica/tutti-agent-gui-host test
node packages/tutti-agent-gui-host/scripts/build-e2e-harness.mjs
# Serve apps/web/public with any static server, then:
PLAYWRIGHT_BASE_URL=http://127.0.0.1:<port> pnpm exec playwright test agent-gui-direct-mount
```

These specs use fake host data. They must not execute an installed Agent CLI or
consume a model account.

All file publication uses Node path/filesystem APIs and relative browser asset
URLs. No POSIX-only copy command or platform-specific filesystem path is used.
The available validation runs on macOS; packaged Windows launch remains a
platform validation gate.
