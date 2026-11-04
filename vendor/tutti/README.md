# Tutti Agent GUI source vendor

This directory vendors the complete `packages/agent/gui` UI module and its
frontend dependency closure from
[`tutti-os/tutti`](https://github.com/tutti-os/tutti/tree/207beeea554435618dffd3b579d0ffe112b065c4).
It was imported at `207beeea554435618dffd3b579d0ffe112b065c4` and no longer
follows upstream: Multica owns and changes this code directly.

Source files import host modules from `@multica/tutti-agent-gui-host`. That
dependency is declared at the repository root, not in these packages: the host
depends on these packages, and a cycle of `node_modules` symlinks makes webpack
walk directories without end.

Agent instruction files are omitted; Multica repository instructions govern
integration work here. No daemon, provider executable, native binary,
`node_modules`, Git metadata or independent build output is included.

## Scope

The 18 frontend production packages are:

- Agent GUI and Agent Activity Core
- Analytics Core
- Browser Workbench Node
- Connector Contracts and Connector Renderer
- UI I18n Runtime, React Hooks, Rich Text and UI System
- Workbench Snapshot and Workbench Surface
- Workspace External Core, File Manager, File Preview, File Reference,
  Issue Manager and User Project

Four additional packages preserve the source's type/configuration contracts:
Client Tuttid TS, Event Protocol, Event Stream Core and Config TSConfig.
The generated client is a type dependency of Agent GUI; it does not start a
Tutti server. The Multica integration supplies activity and command behavior
through the UI package's existing host/runtime ports.

The exact package paths and every copied file are recorded in
`source-manifest.json`. The current snapshot contains 2,483 unchanged upstream
files, 8 files changed by local patches and 22 local package metadata
overlays.

## Package metadata overlays

Each original manifest is preserved byte for byte as `package.upstream.json`.
The local `package.json` keeps the original name, version, source exports,
types and genuine package dependencies, with these explicit changes:

- Packages are private. Upstream build/test/publish scripts and package
  command binaries are omitted because the isolated Multica UI host owns
  compilation and static asset delivery.
- npm dependencies and required web peers use exact versions from the
  upstream lockfile. React/React DOM use 19.2.6 and Tiptap uses 3.23.6 inside
  the isolated GUI document; Multica's existing versions remain independent.
- Only type development dependencies are retained. The optional React Native
  peer and upstream native/tooling development dependencies are omitted.
  Browser Node's optional Electron compatibility declaration is retained.
- Agent GUI's existing type-only Client Tuttid TS and Event Protocol imports
  are explicit local dependencies.
- Rich Text's four Tiptap editor extensions are explicit runtime dependencies
  because `RichTextTriggerEditor.tsx` imports them at runtime; upstream lists
  them as development dependencies. Their ProseMirror peer is also supplied.

The root pnpm overrides constrain Tiptap children only when their parent is
version 3.23.6. This prevents broad upstream ranges from selecting Multica's
3.31.3 editor extensions inside the source GUI, while leaving Multica's
existing editor dependency resolutions intact.

`pnpm-lock.upstream.yaml` records the original resolution source. The root
Multica lockfile owns installation. Public source exports remain complete,
including upstream source entrypoints that its published build omits.

## Styling and licenses

The isolated host loads the original UI System, Workbench Surface and Agent
GUI styles with Tailwind scanning over these frontend packages. Importing
these styles into Multica's parent document would also apply upstream
`html`, `body`, theme and scrollbar rules; keep them inside the independent
UI document.

Upstream's Apache-2.0 license and repository attribution are preserved in
`LICENSE` and the root `NOTICE`. UI System's own `packages/ui/system/NOTICE`
retains third-party MIT/ISC attribution. Keep all three when distributing the
vendored UI. Per-file attribution and comments are unchanged. The independent
host build copies them into `licenses/tutti/` alongside the static assets.
