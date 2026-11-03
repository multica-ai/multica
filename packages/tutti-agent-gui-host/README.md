# Tutti Agent GUI host

This package runs the pinned, unmodified Tutti Agent GUI in its own HTML document.
Multica supplies authenticated data and commands through a private MessageChannel;
the iframe receives no authentication token. The GUI owns its React root, full
upstream styles, portals, fonts, transcript and composer. Multica owns the
session list and header outside the iframe, and hides the redundant embedded
provider and session rails with host-scoped CSS. The original Chat page keeps
its existing styles and navigation.

The host's `multica-theme.css` and `multica-components.css` adapt the intact
source UI to Multica's shared semantic tokens, fonts and control treatments.
They do not replace the source transcript or composer. The host also mounts
Agent and model selection through the public composer footer accessory slot.
Inter, Source Serif and Geist Mono are loaded in this document; parent font
faces do not cross the iframe boundary. Both theme attributes and the `.dark`
class follow the authenticated host snapshot, including source menu portals.
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

The build uses relative asset URLs and publishes the same output to the desktop
renderer public directory and Web public directory as `tutti-agent-gui/`.
The desktop renderer Vite hook and Web dev/build scripts run this automatically.
The generated directories are ignored. Source changes inside this isolated
bundle require rebuilding or restarting the app's development command; the
outer application retains its existing hot reload behavior.

The host build limits the composer mention categories to Sessions, Tasks and
Agents. A guarded pre-transform changes only the pinned source's shared tab
order and the English/Chinese composer hint in the compiled bundle, so empty
browsing, search results and keyboard cycling use the same three categories.
The upstream files remain unchanged;
the build fails if the expected declaration changes. The public hidden-category
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
This is also a guarded presentation adaptation; the vendor snapshot stays intact.

Build caching includes host source, shared Multica token styles, the vendored package closure, root lockfile,
dependency manifests, and upstream attribution. Concurrent Web/desktop startup
shares a build lock. The distribution contains Tutti's LICENSE and NOTICE,
UI System's NOTICE, and the pinned upstream repository/revision under
`licenses/tutti/`.

Vite 8.0.1's Rolldown default and esbuild minification paths both panic while
replacing chunk hashes after upstream Unicode strings such as `━`. Unminified
production output builds successfully, so this package currently disables JS
minification and source maps. This increases transfer and parse size; the
complete source UI, including lazy Monaco and Mermaid modules, is retained.
Do not rewrite upstream UI strings to work around a bundler bug.

## Host contract

The iframe announces `multica:agent-gui:ready` version 1. Its parent verifies
the exact frame window and origin, then transfers one MessagePort with an init
snapshot. All business requests use that port and exact Multica workspace,
session, task, and attachment identities. Abort stops awaiting a request and
never retries a send. Closing the iframe rejects outstanding calls.

Theme and locale updates apply to the iframe document. Source menus and dialogs
remain inside that document. Composer append requests wait through startup,
are applied sequentially by the source composer, and are acknowledged only
after its public completion callback. Existing attachment IDs are reused.
Legacy Multica Task links keep their original `mention://issue/<id>` protocol
and open the existing issue page through the host, including source pointer and
keyboard activation.

An unsent source composer draft belongs to its mounted GUI document. Background
visibility/focus changes keep that document alive. Routing away or switching a
desktop tab can unmount it; restoring an unsent source draft after that unmount
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
