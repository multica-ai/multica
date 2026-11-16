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

Zip this folder — the manifest plus every file it names — and upload it in
**Settings → Plugins**. You need no server of your own: Multica stores the
artifact, serves the panel script from it, and binds your installation to that
one immutable version.

`ui/main.js` is one file with no `import`. That is the contract, not a
simplification for the example: Multica serves the entry inside one generated
document with no module graph, so a bare module specifier has nowhere to
resolve. Bundle your dependencies in.

While you are iterating, `MULTICA_PLUGIN_DIR` publishes straight from disk
instead of asking you to zip and upload after every edit. It still produces an
ordinary version — re-publishing an unchanged version number lands as
`1.0.0+dev.N` — so a panel always runs code somebody consented to.

## Note on scopes

This plugin declares no `net:` scope, so its surface gets
`connect-src 'none'` — it literally cannot send data anywhere. Everything it
does goes through the host bridge, which is the point.
