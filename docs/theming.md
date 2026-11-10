# Theming Multica

A Multica theme is a folder with a `manifest.json` and a `theme.css`, plus any
fonts and images the CSS uses. The CSS overrides public variables for light
and dark mode and can restyle the stable anchors listed below; the manifest
names the theme and can declare options that appear under **Settings →
Appearance → Theme options**. Themes never run JavaScript.

> **Beta.** The variables, anchors and manifest format below are a beta
> API. They may change without notice until it is declared stable; check
> this page when a theme stops looking right after an update.

**Settings → Appearance → Manage** lists every theme: the built-in ones, the
ones on the device, and the community theme store. Install a theme from the
store there, or with **Import theme**, from a `.zip` of the folder or from the
folder itself. Themes are saved on the device, like every other Appearance
setting; **Remove** on a theme's card deletes it, and goes back to Default
when it was in use. **Check for updates** looks for new releases of the
themes installed from the store.

The built-in themes compile to exactly this format and live in
`packages/core/appearance/built-in-themes.ts`: Pop shows options and CSS,
GitHub, Blueprint and Notion show fonts, content styling and anchor CSS.
Duplicating one turns it into a folder you can edit; see
[Writing a theme](#writing-a-theme).

## The folder

```text
paper/
├── manifest.json
├── theme.css
└── fonts/
    └── serif.woff2
```

The folder can sit at the top of the `.zip` or be the zip's only folder.
Files other than fonts (`woff2`, `woff`, `ttf`, `otf`) and images (`png`,
`jpg`, `webp`, `gif`, `avif`, `svg`) are ignored. The whole theme can be at
most 1 MB.

### manifest.json

```json
{
  "apiVersion": 1,
  "id": "paper",
  "name": "Paper",
  "author": "Someone",
  "version": "1.2.0",
  "description": "Warm paper, serif reading text.",
  "options": [
    { "id": "page", "type": "heading", "label": "Page" },
    { "id": "paper-color", "type": "color", "label": "Paper", "default": { "light": "#f6efdf", "dark": "#1d1b16" } },
    { "id": "hyphens", "type": "toggle", "label": "Hyphenate", "default": true }
  ]
}
```

| Field | Rules |
| --- | --- |
| `apiVersion` | Must be `1`. |
| `id` | 1–40 characters of `a-z`, `0-9` and `-`. Built-in ids (`default`, `pop`, `github`, `blueprint`, `notion`) are reserved. Importing a theme with an installed id replaces it. |
| `name` | Required, at most 40 characters. |
| `author`, `version`, `description` | Optional, shown when installing. |
| `options` | Up to 24; see [Options](#options). |

### theme.css

```css
@font-face {
  font-family: "Paper Serif";
  src: url(fonts/serif.woff2);
}

.theme-light {
  --page-canvas: var(--paper-color);
  --brand: #8b4513;
  --font-content-latin: "Paper Serif", Georgia;
  --h1-style: italic;
  --blockquote-style: normal;
}

.theme-dark {
  --page-canvas: var(--paper-color);
}

:root[data-theme-option-hyphens] [data-mc="content"] p {
  hyphens: auto;
}
```

Set variables on `.theme-light` and `.theme-dark` (both on `<html>`). The
CSS loads after the app's own styles while the theme is active, so at equal
specificity the theme wins; keep selectors that simple and avoid
`!important`. What the person picks on the Appearance page still wins over a
theme: high contrast replaces the muted and border colours, and font choices
other than the defaults replace the font variables. Colour is the theme's
alone; to let people pick one, declare an option.

## Variables

Every variable a theme may set is listed in the [reference](#variable-reference)
at the end, with its default. They come in layers:

- **Foundations** (colours, surfaces, shadows, `--radius`, the text scale,
  fonts) are what everything else is built from. Changing `--surface` or
  `--radius` moves every component that uses it.
- **Components and product** (menus, buttons, nav rows, issue rows, the
  board, status colours) default to a foundation. Set one to change just
  that component: `--menu-radius: 0` squares menus and nothing else.
- **Editor content** (headings, lists, tables, quotes, links, code and
  syntax colours) styles issue descriptions, comments, chat and documents.

A component variable is never declared by the app; components read it with
their default as a fallback. So it can be set on any ancestor, not only on
`.theme-light`:

```css
.theme-light [data-mc="chat-window"] {
  --code-block-background: transparent;
}
```

Values that could load a resource are rejected anywhere in `theme.css`, see
[What CSS may not do](#what-css-may-not-do).

## Options

Options render on the Appearance page while the theme is active. Their
types follow Obsidian's Style Settings. `toggle` and `select` mark `<html>`
with an attribute; the others set the variable `--<id>` on `<html>` for both
modes (per mode for `color`), always: the chosen value or the default. So
`theme.css` can read them with a plain `var(--<id>)`.

| Type | Fields | Result |
| --- | --- | --- |
| `heading` | — | Starts a group; `description` is shown under it. |
| `toggle` | `default`: `true` or `false` | `data-theme-option-<id>="on"` when on, absent when off. |
| `select` | `choices`: `[{ "value", "label", "swatch"? }]`, `default` | `data-theme-option-<id>="<value>"`. With a `swatch` on every choice, the choices render as colour dots. |
| `color` | `default`: a hex colour, or `{ "light", "dark" }` | `--<id>`: a hex colour per mode, picked with a colour well for each. |
| `number` | `default`, optional `min`, `max`, `step`, `unit` | `--<id>`: the number with `unit` appended (`px`, `rem`, `%`, …). |
| `slider` | as `number`; `min` and `max` required | as `number`. |
| `text` | `default`: a CSS value | `--<id>`: what was typed, when it is a safe value. |
| `font` | `default`: a font stack | `--<id>`: a font stack (family names and `var()`). |
| `variable-select` | `choices`: `[{ "value", "label" }]` with CSS values, `default` | `--<id>`: the chosen value. |

Option ids are 1–48 characters of `a-z`, `0-9` and `-`, starting with a
letter; `select` choice values are 1–32 of the same. `label`, `description`
and choice labels are a string or an object keyed by locale (`en`,
`zh-Hans`, `zh-Hant`, `ja`, `ko`, `es`, `fr`); English is the fallback.

```json
{ "id": "page-width", "type": "slider", "label": "Page width", "default": 46, "min": 30, "max": 60, "step": 2, "unit": "rem" }
```

```css
[data-mc="content"] { max-width: var(--page-width); }
```

## Selectors

Target anchors, slots and state hooks, not class names or DOM structure:
those change without notice. Anchors, slots and hooks stay until the next
`apiVersion`.

### Anchors

| Anchor | What it marks |
| --- | --- |
| `[data-mc="app-rail"]` | The icon rail on the far left. |
| `[data-mc="sidebar"]` | The navigation panel next to the rail; `data-state` is `expanded` or `collapsed`. |
| `[data-mc="nav-item"]` | A row in the sidebar; `data-active` is `true` on the current one. |
| `[data-mc="page"]` | The rounded canvas that holds the current page. |
| `[data-mc="window-toolbar"]` | Desktop only: the toolbar above the rail. |
| `[data-mc="tab-bar"]` | Desktop only: the tab strip above the page. |
| `[data-mc="issue-group-header"]` | A status group header in issue lists; `data-status` is the status key (`backlog`, `todo`, `in_progress`, `in_review`, `done`, `blocked`, `cancelled`, or a custom key). |
| `[data-mc="issue-row"]` | One issue in a list; `data-status`, `data-priority` (`urgent`, `high`, `medium`, `low`, `none`) and `data-assignee-type` (`member`, `agent`, `squad` or `none`). |
| `[data-mc="board-column"]` | One column on a board. |
| `[data-mc="board-card"]` | One issue card on a board; same attributes as `issue-row`. |
| `[data-mc="status-icon"]` | The status glyph (an `svg`). |
| `[data-mc="priority-icon"]` | The priority glyph (an `svg`). |
| `[data-mc="label-chip"]` | A label; `data-color` is its hex colour. |
| `[data-mc="content"]` | Rich text: issue descriptions, comments, chat messages and documents, reading and editing. `data-density` is `document` or `compact` when reading. |
| `[data-mc="comment"]` | A top-level comment card; `data-author-type` is `member`, `agent` or `system`. |
| `[data-mc="comment-reply"]` | A reply inside a comment card; `data-author-type` as on `comment`. |
| `[data-mc="comment-composer"]` | The box for writing a new comment. |
| `[data-mc="chat-window"]` | The chat panel. |
| `[data-mc="chat-input"]` | The message box in chat. |
| `[data-mc="chat-message"]` | One message in a chat; `data-role` is `user` or `assistant`. |
| `[data-mc="inbox-item"]` | One notification in the inbox; `data-read` is `true` or `false`. |
| `[data-mc="settings-page"]` | The settings page. |

UI primitives carry a `data-slot`; these are public too: `button` (also
`data-variant` and `data-size`), `input`, `textarea`, `switch`, `checkbox`,
`select-trigger`, `select-content`, `select-item`,
`dropdown-menu-content`, `dropdown-menu-item`, `popover-content`,
`tooltip-content`, `dialog-overlay`, `dialog-content`,
`alert-dialog-content`, `sheet-content`, `tabs-list`, `tabs-trigger`,
`card`, `kbd`, `separator`. Line icons are `svg.icon`.

### State on `<html>`

| Class or attribute | When |
| --- | --- |
| `.theme-light`, `.theme-dark` | The mode in use. (`.dark` is set too, in dark mode.) |
| `.is-desktop`, `.is-web` | The Desktop app or a browser. |
| `.mod-macos`, `.mod-windows`, `.mod-linux` | The operating system. |
| `.is-focused` | The window has focus. |
| `.is-sidebar-collapsed` | The navigation panel is collapsed. |
| `[data-theme="<id>"]` | The active theme. |
| `[data-theme-option-<id>]` | The theme's `toggle` and `select` options. |

```css
.mod-macos.is-desktop:not(.is-focused) [data-mc="app-rail"] { opacity: 0.7; }
.theme-dark [data-mc="issue-row"][data-priority="urgent"] { box-shadow: inset 2px 0 var(--priority-urgent); }
```

## What CSS may not do

**Anything that loads a resource is rejected,** because CSS can read page
content through attribute selectors and send it out in a request. That
means:

- `url()` may only name a file in the theme folder (`url(fonts/serif.woff2)`)
  or a fragment (`url(#filter)`). It is rewritten to the file's `data:` URL.
- `@import`, `image()`, `image-set()`, `cross-fade()`, `element()`, `src()`
  and `expression()` are rejected, as are `behavior` and `-moz-binding`.

Escapes, case and comments do not get around the check: it reads the CSS the
way a browser does. When a theme fails a check, nothing is installed and
every problem is listed with its line in `theme.css`.

These are also good practice, and `pnpm theme:check` (below) fails a theme
that breaks them:

- Use only public variables and anchors. Class names and DOM structure
  change without notice. A variable that is not in the
  [reference](#variable-reference) is allowed only when it is an option id
  or the theme reads it itself with `var()`.
- Set variables on `.theme-light` and `.theme-dark`, and keep everything
  else behind a descendant selector, so the theme picker can read a
  theme's colours back from its CSS.
- Name font families only in `--font-ui` and `--font-content-latin`: no
  `sans-serif`, `serif` or other generic family, and no `ui-*` keyword. The
  CJK font follows them, and a generic family matches Han through the
  browser's default for the page language, so the person's CJK choice would
  never apply. The CJK stack ends with its own generic family.
- Keep text readable in both modes (WCAG AA): `--foreground` and
  `--muted-foreground` need 4.5:1 on every surface, `--brand` 4.5:1 on the
  reading surfaces, the primary button label 4.5:1 on the button, and
  `--faint-foreground` 3:1. A frame (rules on the rail, window toolbar or
  tab bar) is checked against its own colours. The check follows `:root`,
  `html`, the mode classes and option attributes the way the browser does,
  with every option at its default; when a colour it measures is also set
  somewhere else, such as inside `@media` or on `body`, it says so instead
  of passing it.

## Safe mode

A theme can hide or break parts of the interface. To get back, open the app
with `?theme=default` in the address, or hold Shift for a moment right after
it loads (on Desktop: reload with ⌘R / Ctrl+R, then hold Shift). The theme is
paused until the window is closed, and the Appearance page offers to turn it
back on.

## Writing a theme

Start from a built-in theme: pick it in **Settings → Appearance**, then
choose **Duplicate “…” as a new theme** from the **Import theme** menu. The
copy gets its own id (`pop-copy`, `pop-copy-2`, …), a manifest with the
theme's options, and a `theme.css` with one declaration per line. Importing
the copy unchanged looks exactly like the original.

### On Desktop: the themes folder

Desktop keeps a `themes` folder in its user data folder. The **Open themes
folder** button next to **Import theme** shows it. Every folder (or `.zip`) in it is
installed when the app starts and listed under **Themes folder** in the
picker. Save a file in it and every open window picks the change up within
about a second, so edit `theme.css` in any editor and watch the app.

Duplicating on Desktop writes the copy straight into this folder and
opens it. A symlinked folder works too, so a theme can live in its own git
repository. If a file stops passing its checks, the last version that did
stays in use and the Appearance page lists the problem. To remove a theme
from the folder, delete its folder. The folder wins over an imported theme
with the same id whenever it is read: on a change and at every start.

### On the web

Duplicating downloads a `.zip`. Unzip it, edit, and import the folder
again: the same `id` replaces the installed copy, and option values are
kept. If a check fails, the import dialog lists every problem.

### Checking a theme

Before importing or sharing a theme, run the same checks from the repository:

```sh
pnpm theme:check path/to/paper          # a folder
pnpm theme:check paper.zip --json       # a zip, as JSON for CI
```

It reports manifest errors, blocked CSS, non-public variables, generic
families in font stacks and WCAG contrast in both modes, each with its line
in `theme.css`, and exits with status 1 when there is an error. Warnings,
such as a colour it cannot resolve, do not fail the check.

To see the theme the way the theme store shows it:

```sh
pnpm theme:preview path/to/paper --out preview   # preview/light.png and preview/dark.png
```

It renders a fixture page (an issue list next to an open issue) built from
the app's own styles and anchors, with the theme applied at its option
defaults.

## Publishing to the theme store

The theme store (beta) lists themes from
[multica-ai/multica-themes](https://github.com/multica-ai/multica-themes).
Its [contributing guide](https://github.com/multica-ai/multica-themes/blob/main/CONTRIBUTING.md)
has the details; in short:

1. Keep the theme in its own GitHub repository.
   [multica-ai/multica-theme-sample](https://github.com/multica-ai/multica-theme-sample)
   is a starting point with a release workflow.
2. Publish a release tagged with the manifest's `version` (`1.0.0` or
   `v1.0.0`) and attach the zipped theme as `theme.zip`.
3. Add the theme to `themes.json` in the registry with a pull request. The
   pull request runs `theme:check` and `theme:preview` on the release and
   comments with the result; a maintainer merges once it passes.

New releases reach the store within about an hour, after they pass the same
checks; people who installed the theme then see **Update**. A release that
fails keeps the previous one listed. The app only installs the exact
`theme.zip` the checks passed, and reads it with the same parser and CSS
sanitizer as an imported theme.

A self-hosted server reads the official registry by default.
`MULTICA_THEME_STORE=off` turns the store off: **Manage** lists only the
themes on the device, and the server makes no registry requests. `MULTICA_THEME_REGISTRY_URL` points it at
another registry's `index.json`.

## Variable reference

Generated from `packages/core/appearance/theme-variables.ts`. Where light
and dark differ, both defaults are given.

<!-- theme-variables:start (generated by packages/views/common/theme-variables.test.ts) -->

### Colours and surfaces

| Variable | Default (light / dark) | Use |
| --- | --- | --- |
| `--app-shell` | `oklch(0.964435 0.001327 286.375)` / `oklch(0.155 0.005 285.823)` | The frame around the page: rail, desktop toolbar and tab bar. |
| `--page-canvas` | `oklch(0.988087 0 0)` / `oklch(0.18 0.005 285.823)` | The page that lists, boards and conversations sit on. |
| `--surface` | `oklch(1 0 0)` / `oklch(0.21 0.006 285.885)` | Bounded content groups: cards, board cards, panels. |
| `--surface-foreground` | `oklch(0.141 0.005 285.823)` / `oklch(0.985 0 0)` | Text on a surface. |
| `--surface-raised` | `oklch(1 0 0)` / `oklch(0.235 0.007 285.885)` | Menus, popovers and dialogs. |
| `--surface-hover` | `oklch(0.967 0.001 286.375)` / `oklch(0.274 0.006 286.033)` | Row and card hover. |
| `--surface-selected` | `oklch(0.95 0.002 286.375)` / `oklch(0.3 0.006 286.033)` | Selected rows and the active nav item. |
| `--surface-selected-foreground` | `oklch(0.141 0.005 285.823)` / `oklch(0.985 0 0)` | Text on a selected row. |
| `--surface-border` | `oklch(0.92 0.004 286.32)` / `oklch(1 0 0 / 10%)` | Hairline around surfaces and the page. |
| `--background` | `var(--page-canvas)` | Shadcn alias; follows `page-canvas` unless set. |
| `--foreground` | `oklch(0.141 0.005 285.823)` / `oklch(0.985 0 0)` | Body text. |
| `--card` | `var(--surface)` | Shadcn alias; follows `surface` unless set. |
| `--card-foreground` | `oklch(0.141 0.005 285.823)` / `oklch(0.985 0 0)` | Text on a card. |
| `--popover` | `var(--surface-raised)` | Shadcn alias; follows `surface-raised` unless set. |
| `--popover-foreground` | `oklch(0.141 0.005 285.823)` / `oklch(0.985 0 0)` | Text in menus and popovers. |
| `--primary` | `oklch(0.21 0.006 285.885)` / `oklch(0.92 0.004 286.32)` | Primary buttons and strong marks; also used as text. |
| `--primary-foreground` | `oklch(0.985 0 0)` / `oklch(0.21 0.006 285.885)` | Text on primary. |
| `--secondary` | `oklch(0.967 0.001 286.375)` / `oklch(0.274 0.006 286.033)` | Secondary buttons. |
| `--secondary-foreground` | `oklch(0.21 0.006 285.885)` / `oklch(0.985 0 0)` | Text on secondary. |
| `--muted` | `oklch(0.967 0.001 286.375)` / `oklch(0.274 0.006 286.033)` | Quiet fills: group headers, code blocks, skeletons. |
| `--muted-foreground` | `oklch(0.505 0.016 285.938)` / `oklch(0.705 0.015 286.067)` | Secondary text. Must clear 4.5:1 on every surface. |
| `--faint-foreground` | `oklch(0.606 0.016 285.938)` / `oklch(0.60 0.015 286.067)` | Marks that are not text: chevrons, separators, empty-state icons. |
| `--accent` | `oklch(0.967 0.001 286.375)` / `oklch(0.274 0.006 286.033)` | Hover fill in menus and lists. |
| `--accent-foreground` | `oklch(0.21 0.006 285.885)` / `oklch(0.985 0 0)` | Text on accent. |
| `--border` | `oklch(0.945 0.003 286.32)` / `oklch(1 0 0 / 6%)` | Dividers and table borders. |
| `--input` | `oklch(0.92 0.004 286.32)` / `oklch(1 0 0 / 15%)` | Input borders. |
| `--ring` | `oklch(0.705 0.015 286.067)` / `oklch(0.552 0.016 285.938)` | Focus ring. |
| `--sidebar` | `oklch(0.964435 0.001327 286.375)` / `oklch(0.21 0.006 285.885)` | Sidebar background (shadcn sidebar). |
| `--sidebar-foreground` | `oklch(0.141 0.005 285.823)` / `oklch(0.985 0 0)` | Sidebar text. |
| `--sidebar-primary` | `oklch(0.21 0.006 285.885)` / `oklch(0.488 0.243 264.376)` | Sidebar primary marks. |
| `--sidebar-primary-foreground` | `oklch(0.985 0 0)` | Text on sidebar primary. |
| `--sidebar-accent` | `oklch(0.935 0.003 286.375)` / `oklch(0.274 0.006 286.033)` | Rail hover and active fill. |
| `--sidebar-accent-foreground` | `oklch(0.21 0.006 285.885)` / `oklch(0.985 0 0)` | Text on sidebar accent. |
| `--sidebar-border` | `oklch(0.92 0.004 286.32)` / `oklch(1 0 0 / 10%)` | Sidebar borders. |
| `--sidebar-ring` | `oklch(0.705 0.015 286.067)` / `oklch(0.552 0.016 285.938)` | Sidebar focus ring. |
| `--brand` | `oklch(0.56079497 0.19449888 256.41258418)` / `oklch(0.65 0.16 255)` | Product colour: links, checkboxes, selection highlights. |
| `--brand-foreground` | `oklch(0.985 0 0)` | Text on brand. |
| `--button-primary` | `--primary` | Primary button fill when it should differ from `primary`. |
| `--button-primary-foreground` | `--primary-foreground` | Primary button text. |
| `--button-primary-border` | `--button-primary` | Primary button border. |
| `--destructive` | `oklch(0.577 0.245 27.325)` / `oklch(0.704 0.191 22.216)` | Errors and destructive actions. |
| `--success` | `oklch(0.55 0.16 145)` / `oklch(0.65 0.15 145)` | Success states. |
| `--warning` | `oklch(0.75 0.16 85)` / `oklch(0.70 0.16 85)` | Warnings. |
| `--info` | `oklch(0.55 0.18 250)` / `oklch(0.65 0.18 250)` | Informational marks. |
| `--chart-1` | `oklch(0.55 0.16 255)` / `oklch(0.72 0.16 255)` | First chart series. |
| `--chart-2` | `oklch(0.66 0.13 255)` / `oklch(0.62 0.13 255)` | Second chart series. |
| `--chart-3` | `oklch(0.76 0.10 255)` / `oklch(0.52 0.10 255)` | Third chart series. |
| `--chart-4` | `oklch(0.85 0.06 255)` / `oklch(0.42 0.06 255)` | Fourth chart series. |
| `--chart-5` | `oklch(0.92 0.03 255)` / `oklch(0.32 0.03 255)` | Fifth chart series. |
| `--find-match` | `oklch(0.92 0.15 100)` / `oklch(0.82 0.16 100)` | In-page find match. |
| `--find-match-active` | `oklch(0.82 0.17 62)` / `oklch(0.76 0.18 60)` | The current find match. |
| `--find-match-foreground` | `oklch(0.22 0.03 92)` / `oklch(0.18 0.02 92)` | Text on a find match. |
| `--scrollbar-track` | `transparent` | Scrollbar track. |
| `--scrollbar-thumb` | `oklch(0 0 0 / 10%)` / `oklch(1 0 0 / 8%)` | Scrollbar thumb. |
| `--scrollbar-thumb-hover` | `oklch(0 0 0 / 18%)` / `oklch(1 0 0 / 18%)` | Scrollbar thumb on hover. |

### Status and priority

| Variable | Default (light / dark) | Use |
| --- | --- | --- |
| `--status-backlog` | `var(--muted-foreground)` | Backlog status icon. |
| `--status-todo` | `var(--muted-foreground)` | Todo status icon. |
| `--status-in-progress` | `var(--warning)` | In progress status icon. |
| `--status-in-review` | `var(--success)` | In review status icon. |
| `--status-done` | `var(--info)` | Done status icon. |
| `--status-blocked` | `var(--destructive)` | Blocked status icon. |
| `--status-cancelled` | `var(--muted-foreground)` | Cancelled status icon. |
| `--priority-urgent` | `var(--destructive)` | Urgent priority icon and badge. |
| `--priority-high` | `oklch(0.68 0.18 50)` / `oklch(0.72 0.17 50)` | High priority icon and badge. |
| `--priority-medium` | `var(--warning)` | Medium priority icon and badge. |
| `--priority-low` | `var(--info)` | Low priority icon and badge. |
| `--priority-none` | `var(--muted-foreground)` | No-priority icon. |

### Shape and depth

| Variable | Default (light / dark) | Use |
| --- | --- | --- |
| `--radius` | `0.5rem` | Base corner radius. Every `rounded-*` step and component radius below derives from it. |
| `--surface-shadow` | `0 1px 2px rgb(15 23 42 / 0.04), 0 1px 1px rgb(15 23 42 / 0.03)` / `0 1px 2px rgb(0 0 0 / 0.2), 0 1px 1px rgb(0 0 0 / 0.16)` | Resting shadow of cards and the page. |
| `--floating-shadow` | `0 16px 40px rgb(15 23 42 / 0.14), 0 3px 10px rgb(15 23 42 / 0.08)` / `0 20px 48px rgb(0 0 0 / 0.46), 0 4px 12px rgb(0 0 0 / 0.28)` | Dialogs and sheets. |
| `--menu-shadow` | `0 8px 24px rgb(15 23 42 / 0.08), 0 2px 6px rgb(15 23 42 / 0.05)` / `0 10px 28px rgb(0 0 0 / 0.3), 0 2px 8px rgb(0 0 0 / 0.18)` | Menus, selects and popovers. |
| `--tooltip` | `oklch(0.21 0.006 285.885)` / `oklch(0.3 0.006 286.033)` | Tooltip fill (dark in both modes). |
| `--tooltip-shadow` | `0 4px 12px rgb(15 23 42 / 0.16), 0 1px 3px rgb(15 23 42 / 0.12)` / `0 0 0 1px oklch(1 0 0 / 10%), 0 8px 24px rgb(0 0 0 / 0.4)` | Tooltip shadow. |
| `--icon-stroke-width` | `2` | Stroke width of line icons. |

### Typography

| Variable | Default (light / dark) | Use |
| --- | --- | --- |
| `--font-ui` | Inter, or the Appearance choice | Interface font. The Appearance page's choice wins over a theme's. |
| `--font-cjk` | system CJK sans, or the Appearance choice | Interface font for Chinese, Japanese and Korean. |
| `--font-content-latin` | `--font-ui` | Font for issue descriptions, comments, chat and documents. |
| `--font-content-cjk` | `--font-cjk` | CJK font for that content. |
| `--font-mono` | Geist Mono | Code font. |
| `--content-leading` | `1.625` | Line height of content paragraphs. |
| `--content-leading-cjk` | `1.75` | Line height of CJK content paragraphs. |
| `--text-micro` | `calc(11px * var(--font-scale, 1))` | Counters, badges, timestamps. |
| `--text-micro--line-height` | `calc(15px * var(--font-scale, 1))` | Line height of `text-micro`. |
| `--text-caption` | `calc(12px * var(--font-scale, 1))` | Chips, hints, tooltips, issue identifiers. |
| `--text-caption--line-height` | `calc(16px * var(--font-scale, 1))` | Line height of `text-caption`. |
| `--text-label` | `calc(13px * var(--font-scale, 1))` | Metadata rows, table headers, activity. |
| `--text-label--line-height` | `calc(18px * var(--font-scale, 1))` | Line height of `text-label`. |
| `--text-body` | `calc(14px * var(--font-scale, 1))` | Interface body text. |
| `--text-body--line-height` | `calc(20px * var(--font-scale, 1))` | Line height of `text-body`. |
| `--text-body-lg` | `calc(15px * var(--font-scale, 1))` | Dialog copy, onboarding prose. |
| `--text-body-lg--line-height` | `calc(22px * var(--font-scale, 1))` | Line height of `text-body-lg`. |
| `--text-title-sm` | `calc(16px * var(--font-scale, 1))` | Small headings. |
| `--text-title-sm--line-height` | `calc(24px * var(--font-scale, 1))` | Line height of `text-title-sm`. |
| `--text-title` | `calc(18px * var(--font-scale, 1))` | Section headings. |
| `--text-title--line-height` | `calc(28px * var(--font-scale, 1))` | Line height of `text-title`. |
| `--text-title-lg` | `calc(20px * var(--font-scale, 1))` | Card and dialog titles. |
| `--text-title-lg--line-height` | `calc(28px * var(--font-scale, 1))` | Line height of `text-title-lg`. |
| `--text-display-sm` | `calc(24px * var(--font-scale, 1))` | Page titles. |
| `--text-display-sm--line-height` | `calc(32px * var(--font-scale, 1))` | Line height of `text-display-sm`. |
| `--text-display` | `calc(36px * var(--font-scale, 1))` | Hero numbers and empty-state headlines. |
| `--text-display--line-height` | `calc(40px * var(--font-scale, 1))` | Line height of `text-display`. |

### Controls and overlays

| Variable | Default (light / dark) | Use |
| --- | --- | --- |
| `--button-radius` | `var(--radius)` | Button corners. |
| `--button-radius-sm` | `calc(var(--radius) * 0.75)` | Corners of extra-small and small buttons. |
| `--button-height-xs` | `1.5rem` | Extra-small button height. |
| `--button-height-sm` | `1.75rem` | Small button height. |
| `--button-height-default` | `2rem` | Default button height. |
| `--button-height-lg` | `2.25rem` | Large button height. |
| `--button-padding-xs` | `0.5rem` | Extra-small button side padding. |
| `--button-padding-sm` | `0.625rem` | Small button side padding. |
| `--button-padding-default` | `0.625rem` | Default button side padding. |
| `--button-padding-lg` | `0.625rem` | Large button side padding. |
| `--button-gap-xs` | `0.25rem` | Icon gap in extra-small buttons. |
| `--button-gap-sm` | `0.25rem` | Icon gap in small buttons. |
| `--button-gap-default` | `0.375rem` | Icon gap in default buttons. |
| `--button-gap-lg` | `0.375rem` | Icon gap in large buttons. |
| `--input-radius` | `var(--radius)` | Inputs, textareas and select triggers. |
| `--card-radius` | `calc(var(--radius) * 1.5)` | Cards. |
| `--menu-radius` | `calc(var(--radius) * 1.5)` | Dropdown, context and select menus. |
| `--menu-item-radius` | `calc(var(--radius) * 0.75)` | Items inside a menu. |
| `--menu-item-hover-background` | `var(--accent)` | Highlighted menu item. |
| `--menu-item-hover-color` | `var(--accent-foreground)` | Text of the highlighted menu item. |
| `--popover-radius` | `var(--radius)` | Popovers. |
| `--dialog-radius` | `calc(var(--radius) * 1.5)` | Dialogs and alert dialogs. |
| `--tooltip-radius` | `var(--radius)` | Tooltips. |
| `--avatar-radius` | `calc(infinity * 1px)` | Avatars. Set a length for square avatars. |
| `--label-radius` | `calc(infinity * 1px)` | Issue label chips. |

### Navigation and page

| Variable | Default (light / dark) | Use |
| --- | --- | --- |
| `--page-radius` | `calc(var(--radius) * 1.5)` | Corners of the page card inside the frame. |
| `--page-border-color` | `var(--surface-border)` | Hairline around the page card. |
| `--page-shadow` | `var(--surface-shadow)` | Shadow of the page card. |
| `--rail-item-radius` | `calc(var(--radius) * 1.5)` | Space buttons in the rail. |
| `--rail-item-color` | `var(--muted-foreground)` | Rail button icon and label. |
| `--rail-item-hover-background` | `color-mix(in oklab, var(--sidebar-accent) 60%, transparent)` | Rail button hover. |
| `--rail-item-active-background` | `var(--sidebar-accent)` | The current space. |
| `--rail-item-active-color` | `var(--sidebar-accent-foreground)` | Icon and label of the current space. |
| `--nav-item-height` | `2rem` | Minimum height of a row in the space panel. |
| `--nav-item-radius` | `var(--radius)` | Rows in the space panel. |
| `--nav-item-color` | `var(--muted-foreground)` | Row text. |
| `--nav-item-hover-background` | `var(--surface-hover)` | Row hover. |
| `--nav-item-hover-color` | `var(--foreground)` | Row text on hover. |
| `--nav-item-active-background` | `var(--surface-selected)` | The current row. |
| `--nav-item-active-color` | `var(--surface-selected-foreground)` | Text of the current row. |

### Issues

| Variable | Default (light / dark) | Use |
| --- | --- | --- |
| `--issue-row-height` | `36px` | Height of a row in the issue list. |
| `--issue-row-padding-x` | `1rem` | Side padding of an issue row. |
| `--issue-row-hover-background` | `var(--surface-hover)` | Issue row hover. |
| `--issue-row-selected-background` | `var(--surface-selected)` | Selected issue row. |
| `--issue-row-identifier-color` | `var(--muted-foreground)` | Issue identifier (MUL-123) in a row. |
| `--issue-group-header-height` | `2.5rem` | Status group header in the list. |
| `--issue-group-header-radius` | `var(--radius)` | Corners of a group header. |
| `--issue-group-header-background` | `var(--muted)` | Group header fill. |
| `--issue-group-header-hover-background` | `var(--accent)` | Group header hover. |
| `--board-column-radius` | `calc(var(--radius) * 1.5)` | Board columns. |
| `--board-column-background` | `color-mix(in oklab, var(--muted) 40%, transparent)` | Board column fill. |
| `--board-card-radius` | `var(--radius)` | Board cards. |
| `--board-card-background` | `var(--surface)` | Board card fill. |
| `--board-card-hover-background` | `var(--surface-hover)` | Board card hover. |
| `--board-card-border-color` | `var(--surface-border)` | Board card border. |
| `--board-card-shadow` | `var(--surface-shadow)` | Board card shadow. |

### Editor content

| Variable | Default (light / dark) | Use |
| --- | --- | --- |
| `--h1-font` | `var(--font-content)` | Font of level 1 headings. |
| `--h1-size` | `var(--text-display-sm)` | Size of level 1 headings. |
| `--h1-weight` | `600` | Weight of level 1 headings. |
| `--h1-style` | `normal` | `italic` for italic level 1 headings. |
| `--h1-variant` | `normal` | `small-caps` and other font variants. |
| `--h1-letter-spacing` | `normal` | Tracking of level 1 headings. |
| `--h1-line-height` | `1.3` | Line height of level 1 headings. |
| `--h1-color` | `currentColor` | Colour of level 1 headings. |
| `--h2-font` | `var(--font-content)` | Font of level 2 headings. |
| `--h2-size` | `var(--text-title)` | Size of level 2 headings. |
| `--h2-weight` | `600` | Weight of level 2 headings. |
| `--h2-style` | `normal` | `italic` for italic level 2 headings. |
| `--h2-variant` | `normal` | `small-caps` and other font variants. |
| `--h2-letter-spacing` | `normal` | Tracking of level 2 headings. |
| `--h2-line-height` | `1.35` | Line height of level 2 headings. |
| `--h2-color` | `currentColor` | Colour of level 2 headings. |
| `--h3-font` | `var(--font-content)` | Font of level 3 headings. |
| `--h3-size` | `var(--text-body-lg)` | Size of level 3 headings. |
| `--h3-weight` | `600` | Weight of level 3 headings. |
| `--h3-style` | `normal` | `italic` for italic level 3 headings. |
| `--h3-variant` | `normal` | `small-caps` and other font variants. |
| `--h3-letter-spacing` | `normal` | Tracking of level 3 headings. |
| `--h3-line-height` | `1.4` | Line height of level 3 headings. |
| `--h3-color` | `currentColor` | Colour of level 3 headings. |
| `--h4-font` | `var(--font-content)` | Font of level 4 headings. |
| `--h4-size` | `var(--text-body)` | Size of level 4 headings. |
| `--h4-weight` | `600` | Weight of level 4 headings. |
| `--h4-style` | `normal` | `italic` for italic level 4 headings. |
| `--h4-variant` | `normal` | `small-caps` and other font variants. |
| `--h4-letter-spacing` | `normal` | Tracking of level 4 headings. |
| `--h4-line-height` | `1.45` | Line height of level 4 headings. |
| `--h4-color` | `currentColor` | Colour of level 4 headings. |
| `--h5-font` | `var(--font-content)` | Font of level 5 headings. |
| `--h5-size` | `var(--text-label)` | Size of level 5 headings. |
| `--h5-weight` | `600` | Weight of level 5 headings. |
| `--h5-style` | `normal` | `italic` for italic level 5 headings. |
| `--h5-variant` | `normal` | `small-caps` and other font variants. |
| `--h5-letter-spacing` | `normal` | Tracking of level 5 headings. |
| `--h5-line-height` | `1.5` | Line height of level 5 headings. |
| `--h5-color` | `currentColor` | Colour of level 5 headings. |
| `--h6-font` | `var(--font-content)` | Font of level 6 headings. |
| `--h6-size` | `var(--text-caption)` | Size of level 6 headings. |
| `--h6-weight` | `600` | Weight of level 6 headings. |
| `--h6-style` | `normal` | `italic` for italic level 6 headings. |
| `--h6-variant` | `normal` | `small-caps` and other font variants. |
| `--h6-letter-spacing` | `normal` | Tracking of level 6 headings. |
| `--h6-line-height` | `1.5` | Line height of level 6 headings. |
| `--h6-color` | `var(--muted-foreground)` | Colour of level 6 headings. |
| `--paragraph-spacing` | `0.625rem` | Space above and below paragraphs. |
| `--list-indent` | `1.25rem` | Indent of bulleted lists. |
| `--list-indent-ordered` | `1.5rem` | Indent of numbered lists. |
| `--list-item-spacing` | `0.25rem` | Space between list items. |
| `--list-marker-color` | `var(--muted-foreground)` | Bullets and numbers. |
| `--checkbox-size` | `0.95rem` | Task list checkboxes. |
| `--checkbox-color` | `var(--brand)` | Checked checkbox fill. |
| `--checklist-done-color` | `var(--muted-foreground)` | Text of a completed task. |
| `--link-color` | `var(--brand)` | Links in content. |
| `--link-decoration` | `underline` | `underline` or `none`. |
| `--link-decoration-color` | `color-mix(in srgb, var(--brand) 40%, transparent)` | Link underline. |
| `--link-decoration-color-hover` | `var(--brand)` | Link underline on hover. |
| `--link-underline-offset` | `2px` | Gap between text and underline. |
| `--mention-color` | `var(--primary)` | @mentions. |
| `--mention-weight` | `600` | Weight of @mentions. |
| `--bold-weight` | `600` | Bold text. |
| `--bold-color` | `currentColor` | Colour of bold text. |
| `--italic-color` | `currentColor` | Colour of italic text. |
| `--strikethrough-color` | `var(--muted-foreground)` | Colour of struck-through text. |
| `--highlight-background` | `color-mix(in srgb, #facc15 45%, transparent)` | `==highlighted==` text. |
| `--highlight-radius` | `0.2em` | Corners of highlighted text. |
| `--blockquote-border-width` | `3px` | Quote bar width. |
| `--blockquote-border-color` | `color-mix(in srgb, var(--muted-foreground) 30%, transparent)` | Quote bar colour. |
| `--blockquote-padding` | `0.75rem` | Space between the bar and the text. |
| `--blockquote-color` | `var(--muted-foreground)` | Quote text. |
| `--blockquote-background` | `transparent` | Quote fill. |
| `--blockquote-style` | `italic` | `italic` or `normal`. |
| `--hr-color` | `var(--border)` | Horizontal rules. |
| `--hr-thickness` | `1px` | Horizontal rule thickness. |
| `--table-border-width` | `1px` | Table borders. |
| `--table-border-color` | `var(--border)` | Table border colour. |
| `--table-radius` | `var(--radius)` | Table corners. |
| `--table-header-background` | `color-mix(in srgb, var(--muted) 50%, transparent)` | Header row fill. |
| `--table-header-color` | `var(--muted-foreground)` | Header text. |
| `--table-header-weight` | `500` | Header weight. |
| `--table-row-hover-background` | `color-mix(in srgb, var(--muted) 30%, transparent)` | Row hover. |
| `--table-cell-padding` | `0.5rem 0.75rem` | Cell padding. |

### Code

| Variable | Default (light / dark) | Use |
| --- | --- | --- |
| `--code-font-size` | `var(--text-body)` | Inline code size. |
| `--code-background` | `color-mix(in srgb, var(--foreground) 3%, transparent)` | Inline code fill. |
| `--code-border-color` | `color-mix(in srgb, var(--foreground) 5%, transparent)` | Inline code border. |
| `--code-color` | `color-mix(in srgb, var(--foreground) 75%, transparent)` | Inline code text. |
| `--code-padding` | `0.125rem 0.375rem` | Inline code padding. |
| `--code-radius` | `var(--radius-sm)` | Inline code corners. |
| `--code-block-background` | `var(--muted)` | Code block fill. |
| `--code-block-border` | `none` | Code block border, e.g. `1px solid var(--border)`. |
| `--code-block-radius` | `var(--radius)` | Code block corners. |
| `--code-block-padding` | `0.75rem 1rem` | Code block padding. |
| `--code-block-color` | `var(--foreground)` | Code block text. |
| `--code-block-font-size` | `var(--text-label)` | Code block size. |
| `--code-block-line-height` | `1.6` | Code block line height. |
| `--code-keyword` | `oklch(0.55 0.16 255)` / `oklch(0.7 0.14 255)` | Keywords and built-ins. |
| `--code-string` | `oklch(0.55 0.14 155)` / `oklch(0.7 0.14 155)` | Strings and added lines. |
| `--code-comment` | `var(--muted-foreground)` | Comments. |
| `--code-number` | `oklch(0.58 0.16 30)` / `oklch(0.72 0.14 30)` | Numbers and literals. |
| `--code-function` | `oklch(0.55 0.14 280)` / `oklch(0.72 0.12 280)` | Function and section names. |
| `--code-property` | `oklch(0.58 0.12 60)` / `oklch(0.72 0.1 60)` | Attributes and properties. |
| `--code-variable` | `oklch(0.58 0.14 20)` / `oklch(0.72 0.12 20)` | Variables. |
| `--code-type` | `oklch(0.55 0.14 200)` / `oklch(0.72 0.12 200)` | Types and classes. |
| `--code-deletion` | `oklch(0.55 0.2 25)` / `oklch(0.7 0.18 25)` | Removed lines. |
| `--code-tag` | `oklch(0.55 0.16 255)` / `oklch(0.72 0.14 255)` | HTML and XML element names. |
| `--code-punctuation` | `var(--muted-foreground)` | HTML and XML brackets. |

<!-- theme-variables:end -->
