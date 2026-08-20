# Hello Panel

The reference Multica plugin: one `issue_panel` surface that exercises every
part of the Action API a v1 surface can reach.

It is also the fixture the surface end-to-end tests run against, so keep it
boring — it should demonstrate the contract, not the framework of the week.

## What it shows

- `multica.context.get()` — who is looking and which issue the panel is on
- `multica.issue.get()` — reading the issue behind `issues:read`
- `multica.issue.comment()` — a write that lands as **the user**, marked with
  the plugin (`via_plugin_id`), behind `comments:write`
- `multica.storage.user` — per-member state behind `storage:user`
- `multica.ui.resize()` — asking the host for the height it actually needs

## Running it

The manifest and `ui/main.js` must be served over public HTTPS from the same
directory, because `entry` resolves relative to the manifest URL and Multica
never re-hosts plugin code. Install by pasting the manifest URL into
**Settings → Plugins**.

`MULTICA_PLUGIN_DIR` installs (`local:hello-panel`) are for developing the
manifest itself: the install and consent flow work, but the panel cannot load a
script from the server's filesystem and says so instead of rendering blank.

## Note on scopes

This plugin declares no `net:` scope, so its surface gets
`connect-src 'none'` — it literally cannot send data anywhere. Everything it
does goes through the host bridge, which is the point.
