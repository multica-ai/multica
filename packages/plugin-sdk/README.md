# @multica/plugin-sdk

What a Multica plugin surface imports.

```js
import { multica } from "@multica/plugin-sdk";

const ctx   = await multica.context.get();
const issue = await multica.issue.get();
await multica.issue.comment({ body: "hello" });
const note  = await multica.storage.user.get("note");
multica.ui.resize(320);
```

## What a surface is

**One** script in a sandboxed iframe.

Bundle this SDK and everything else your surface needs into a single file. There
is no module graph: you publish an artifact, Multica stores it and serves your
entry inside a generated document on its dedicated plugin-content origin. There
is no path back to the author's server and no top-level module graph for a bare
`import` to resolve against. Publishing refuses an entry that has one rather
than letting it fail later in a reader's browser.

The frame is mounted with `sandbox="allow-scripts"` and **not**
`allow-same-origin`, so it has an opaque origin. Consequences worth knowing
before you write one:

- **No browser storage.** `localStorage`, `sessionStorage` and cookies all throw
  or are empty. Use `multica.storage` — it is server-side, scoped per workspace
  or per member, and survives the frame.
- **`Origin: null` on your own requests.** If your surface calls your backend
  directly, that backend must accept a null origin in CORS.
- **A CSP you did not write.** Multica generates the response and derives
  `connect-src` from the `net:` scopes in your manifest. Declare every host you
  intend to reach; with no `net:` scope your surface cannot issue a network
  request at all, including back to your own origin, which is no longer in the
  policy now that Multica serves your code. `net:` is an exact host, so declare
  `net:api.example.com` separately from `net:example.com`.

## Publishing

Zip the manifest with every file it names and upload it in **Settings →
Plugins**. You need no server of your own for the frontend; hook endpoints and
MCP servers are still yours to run.

A published version is immutable. Installing binds a workspace to one version,
and publishing a new one changes nothing there until an administrator upgrades —
so what they approved on the consent screen is what their browsers run.

## What you can do, and what bounds it

Every call becomes a message to the host, which performs it on the signed-in
user's own session. Two limits apply at once:

1. the scopes the workspace admin granted your plugin, and
2. what that particular user could already do themselves.

So a member without access to an issue gets a 404 through your surface too, and
a scope the admin declined is a 403 that names it. Errors are
`MulticaPluginError` with a `status` mirroring HTTP.

A comment you post is authored by **the user**, recorded as having been made
through your plugin. It does not run `@mention` trigger dispatch — a surface
cannot start agent runs as a side effect of posting text.

## Theme

The host pushes design tokens over the private port and again on every theme switch, and
the SDK writes them as custom properties on `:root`. Use `var(--foreground)`,
`var(--background)`, `var(--border)`, `var(--radius)` and friends and your
surface will look native without shipping a stylesheet.

`multica.ui.onThemeChange(fn)` if you need to react in JS.

## Sizing

The frame does not auto-size. Call `multica.ui.resize(px)` after your content
settles; the host clamps the value.
