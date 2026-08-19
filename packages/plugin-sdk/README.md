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

An ordinary script in a sandboxed iframe. Multica never executes plugin code on
its servers — a surface runs in the user's browser, or on your own server behind
a hook.

The frame is mounted with `sandbox="allow-scripts"` and **not**
`allow-same-origin`, so it has an opaque origin. Consequences worth knowing
before you write one:

- **No browser storage.** `localStorage`, `sessionStorage` and cookies all throw
  or are empty. Use `multica.storage` — it is server-side, scoped per workspace
  or per member, and survives the frame.
- **`Origin: null` on your own requests.** If your surface calls your backend
  directly, that backend must accept a null origin in CORS.
- **A CSP you did not write.** The host generates the document and derives
  `connect-src` from the `net:` scopes in your manifest. Declare every host you
  intend to reach; a surface with no `net:` scope cannot make network requests
  at all. `net:` is an exact host — declare `net:api.example.com` separately from
  `net:example.com`.

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

The host pushes design tokens in at init and again on every theme switch, and
the SDK writes them as custom properties on `:root`. Use `var(--foreground)`,
`var(--background)`, `var(--border)`, `var(--radius)` and friends and your
surface will look native without shipping a stylesheet.

`multica.ui.onThemeChange(fn)` if you need to react in JS.

## Sizing

The frame does not auto-size. Call `multica.ui.resize(px)` after your content
settles; the host clamps the value.
