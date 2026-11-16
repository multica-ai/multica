# Themes

A Multica theme restyles the Multica app, on Desktop and on the web, through
public CSS variables. A user can ask for one anywhere: a chat, an issue, a
comment. This is the contract for building one and handing it over.

## What a theme is

A folder, or a `.zip` of it, holding:

- `manifest.json`: `apiVersion` (1), `id`, `name`, `version`, and optionally
  `author`, `description` and `options` (sliders, toggles and choices the user
  can change in Settings).
- `theme.css`: variables set on `.theme-light` and `.theme-dark`.
- The images and fonts `theme.css` reaches with a relative `url()`.

What the checker enforces, so design within it from the start:

- **No code, no network.** Every `url()` points at a file inside the theme.
- **Public variables only**, as listed in the guide; a variable of the theme's
  own is fine when the theme reads it with `var()`.
- **The id** is 1–40 characters of a-z, 0-9 and `-`, and not a built-in
  theme's id. Keep it once chosen: installing the same id again replaces the
  old version, while a new id adds a second theme.
- **Contrast.** Text must reach WCAG AA on the surfaces it sits on, in both
  modes. A backdrop is measured as glass over `--backdrop-color`.

Style both light and dark unless the user asks for a single mode.

## Get the guide and the checker

The guide and the checker live in the public repository. A sparse clone
needs only git and Node 22.18 or newer:

```sh
git clone --depth 1 --filter=blob:none --sparse https://github.com/multica-ai/multica.git multica-themes
git -C multica-themes sparse-checkout set docs scripts packages/core packages/ui
```

Read `multica-themes/docs/theming.md` before writing anything: variables,
selectors, options, backdrop and glass, and the contrast rules all live
there, and they change between releases. Keep the clone for follow-ups and
`git -C multica-themes pull` before reusing it.

## Check before you deliver

```sh
node multica-themes/scripts/theme-check.mjs <theme folder or zip>
```

Deliver only when it reports **0 errors**. Fix the warnings you can, and tell
the user why you kept any that remain. When you have a full clone with
`pnpm install` done, `pnpm theme:preview <theme folder>` renders `light.png`
and `dark.png`; look at them, and attach them to your reply.

## Deliver

Desktop watches a themes folder in its user data:

| System | Themes folder |
|---|---|
| macOS | `~/Library/Application Support/Multica/themes` |
| Windows | `%APPDATA%\Multica\themes` |
| Linux | `$XDG_CONFIG_HOME/Multica/themes` (default `~/.config/Multica/themes`) |

- **Desktop on this computer** (the `Multica` folder above exists): write the
  theme into `<themes folder>/<id>/`, creating `themes` if needed. Desktop
  lists it once it passes the checks but does not switch to it: tell the user
  to pick it once under Settings → Appearance → Theme. From then on every
  save to the theme in use applies within about a second, so make follow-up
  changes in those files.
- **Anywhere else**, or when the user is on the web or another computer: zip
  the theme with `manifest.json` at the top of the zip as `<id>.zip`, and
  attach it to your reply (`multica attachment upload <path>` in a chat,
  `--attachment <path>` on `multica issue comment add`). The attachment shows
  **Install theme**, which runs the same checks and asks before installing.
  For a follow-up, raise `version` and attach a new zip; installing it
  replaces the previous one.

In the reply, say where the theme went (the folder, or the attachment) and,
for the folder, that it needs picking once; what the checker reported; and
include the previews when you have them.
