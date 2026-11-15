# Theming Multica

A Multica theme is a folder with a `manifest.json` and a `theme.css`, plus any
fonts and images the CSS uses. The CSS overrides public variables for light
and dark mode and can restyle the stable anchors listed below; the manifest
names the theme and can declare options that appear under **Settings →
Appearance → Theme options**. Themes never run JavaScript.

> **Beta.** The variables, anchors and manifest format below are a beta
> API. They may change without notice until it is declared stable; check
> this page when a theme stops looking right after an update.

**Settings → Appearance → Manage** lists the themes installed on the device
and the community theme store; the built-in themes are in the theme picker. Install a theme from the
store there, or with **Import theme**, from a `.zip` of the folder or from the
folder itself; a theme `.zip` attached to a chat or comment installs with
**Install theme** on the attachment. Themes are saved on the device, like every other Appearance
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
theme: high contrast replaces the muted and border colours, font choices
other than the defaults replace the font variables, and Compact or
Comfortable density replaces the issue row and group header heights, board
card padding and gap, and the space panel's row height and padding. Colour
is the theme's alone; to let people pick one, declare an option.

## Variables

Every variable a theme may set is listed in the [reference](#variable-reference)
at the end, with its default. They come in layers:

- **Foundations** (colours, surfaces, shadows, `--radius`, the text scale,
  fonts) are what everything else is built from. Changing `--surface` or
  `--radius` moves every component that uses it.
- **Components and product** (menus, buttons, nav rows, issue rows, the
  board, status colours) default to a foundation. Set one to change just
  that component: `--menu-radius: 0` squares menus and nothing else.
- **Layout and spacing** (a spacing scale `--space-xs` to `--space-xl`, the
  page's side padding, the navigation panel's starting width, the widest the
  reading column and object pages get) sets the room around everything.
  Board card and page paddings read the scale, so changing `--space-sm`
  moves each of them that has no variable of its own set.
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

### Colour slots

Labels, custom statuses and select options carry the colour someone picked,
a `#rrggbb`. A theme recolours them through ten slots named after the
colour picker's presets: `--palette-gray`, `-red`, `-orange`, `-yellow`,
`-green`, `-teal`, `-blue`, `-indigo`, `-purple` and `-pink`. A preset lands
on its own slot. Any other colour is placed in OKLab: one with little chroma
on gray, the rest on the slot whose hue is nearest, so pale and dark shades
stay with their hue. A slot the theme leaves unset keeps every colour in it
as picked, so declare only the ones you want to change:

```css
.theme-dark {
  --palette-red: oklch(0.7 0.14 25);
  --palette-yellow: oklch(0.82 0.12 90);
  --label-chip-color: oklch(0.2 0.02 260);
}
```

Label text is dark or light by the colour as picked, so a slot shows both:
a pale pick and a deep pick of red both land in `--palette-red`. Once slots
change, set `--label-chip-color` too, per slot if needed:
`[data-mc="label-chip"][data-palette="yellow"] { --label-chip-color: … }`.
The label chip and a custom status's icon carry `data-color` (the hex) and
`data-palette` (the slot). The built-in statuses keep their own variables,
`--status-*`.

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

## Backdrop and glass

A theme can put a picture behind the app: a gradient, an image, or an
animated SVG. Set `--backdrop` per mode to a CSS `background` value, and
the app paints it on a layer under the whole window, on Desktop and on the
web. While a mode has a backdrop, the frame (rail, window toolbar, tab bar
and bottom bar) and the page turn to glass: each keeps its own colour,
`--app-shell` for the frame and `--page-canvas` for the page, at the
opacity the theme sets, over the backdrop.

```css
.theme-dark {
  --backdrop: url(night.svg) center / cover no-repeat;
  --backdrop-color: #1e1e4a;
  --glass-opacity: 72%;
  --glass-blur: 18px;
  --frame-glass-opacity: 0%;
  --frame-glass-blur: 0px;
}
```

| Variable | What it does |
| --- | --- |
| `--backdrop` | The layers behind the window, as `background` takes them. Images must be in the theme folder. An SVG's own CSS animation plays. |
| `--backdrop-color` | Required with a backdrop. The solid colour under it: shown while the images load, and what `theme:check` measures the glass on. Use the colour the backdrop averages to. |
| `--glass-opacity` | How much of `--page-canvas` (and `--background`) covers the backdrop in the page: `100%` is solid. |
| `--glass-blur` | How much the backdrop is blurred behind the page. |
| `--frame-glass-opacity`, `--frame-glass-blur` | The same for the frame. They default to the page's values; `0%` and `0px` leave the backdrop clear around the page. |

Use these variables instead of making `--app-shell` transparent or setting a
`background` on `body`: the app then knows there is a backdrop, keeps the
frame and the page distinct, and decodes a new backdrop before it replaces the old one, so switching
themes or saving `theme.css` never flashes the window. Option sliders can
set the glass directly: an option with the id `glass-opacity` sets
`--glass-opacity`.

`theme:check` measures text over glass: the frame's text on `--app-shell` at
`--frame-glass-opacity` over `--backdrop-color`, and the page's on
`--page-canvas` at `--glass-opacity` over it. A variable that reads one of
these colours, such as `--surface: var(--page-canvas)`, keeps the opaque
colour, as it does in the app, and so does everything in dialogs, menus and
tooltips, which sit outside the frame. A backdrop without a solid
`--backdrop-color` is an error. The colour is a stand-in for the picture,
so look at the brightest and darkest parts of the backdrop too, and raise
the glass opacity or blur where the text gets hard to read.
`examples/themes/night-sky` in the repository is a complete example: an
animated sky in each mode and the glass on two sliders.

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
| `[data-mc="panel-section"]` | A labelled group of rows, headed by an `h3`, in the sidebar or a detail page's property panel. Scope it with `[data-mc="sidebar"] [data-mc="panel-section"]` for the sidebar only. |
| `[data-mc="page"]` | The rounded canvas that holds the current page. |
| `[data-mc="page-header"]` | The bar at the top of a page, with its title and actions. |
| `[data-mc="window-toolbar"]` | Desktop only: the toolbar above the rail. |
| `[data-mc="tab-bar"]` | Desktop only: the tab strip above the page. |
| `[data-mc="bottom-bar"]` | The status bar under the page, on windows wide enough to show it. |
| `[data-mc="issue-group-header"]` | A status group header in issue lists; `data-status` is the status key (`backlog`, `todo`, `in_progress`, `in_review`, `done`, `blocked`, `cancelled`, or a custom key). |
| `[data-mc="issue-row"]` | One issue in a list; `data-status`, `data-priority` (`urgent`, `high`, `medium`, `low`, `none`), `data-assignee-type` (`member`, `agent`, `squad` or `none`), `data-parity` (`even` or `odd`, the row's index within its status group, so stripes survive virtual scrolling where `:nth-child` does not) and `data-selected` (`true` or `false`, ticked for bulk actions). |
| `[data-mc="board-column"]` | One column on a board. |
| `[data-mc="board-card"]` | One issue card on a board; `data-status`, `data-priority` and `data-assignee-type` as on `issue-row`. Cards have no `data-parity` or `data-selected`. |
| `[data-mc="status-icon"]` | The status glyph (an `svg`). A custom status adds `data-color`, its hex colour, and `data-palette`, its [colour slot](#colour-slots). |
| `[data-mc="priority-icon"]` | The priority glyph (an `svg`). |
| `[data-mc="label-chip"]` | A label; `data-color` is its hex colour and `data-palette` its [colour slot](#colour-slots). The chip paints with `var(--label-color)` and `var(--label-foreground)`, so a rule can restyle it without `!important`. `--label-color` is the slot's colour when the theme declares one, else the colour as picked; `--label-foreground` is `--label-chip-color` when set, else dark or light text by the picked colour. |
| `[data-mc="content"]` | Rich text: issue descriptions, comments, chat messages and documents, reading and editing. `data-density` is `document` or `compact` when reading. |
| `[data-mc="comment"]` | A top-level comment card; `data-author-type` is `member`, `agent` or `system`. |
| `[data-mc="comment-reply"]` | A reply inside a comment card; `data-author-type` as on `comment`. |
| `[data-mc="comment-composer"]` | The box for writing a new comment. |
| `[data-mc="chat-window"]` | The chat panel. |
| `[data-mc="chat-input"]` | The message box in chat. |
| `[data-mc="chat-message"]` | One message in a chat; `data-role` is `user` or `assistant`. |
| `[data-mc="chat-bubble"]` | The bubble around a user message. Assistant replies have none: they are full-width rich text. |
| `[data-mc="inbox-item"]` | One notification in the inbox; `data-read` is `true` or `false`, and `data-selected` is `true` on the one open in the detail pane. |
| `[data-mc="unread-dot"]` | The unread marker on an inbox item. |
| `[data-mc="toaster"]` | The toast region. Each toast inside is an `li` with `data-type` `success`, `error`, `info`, `warning`, `loading` or `default`. A plain toast usually has no `data-type`, so match it with both `li:not([data-type])` and `li[data-type="default"]`. |
| `[data-mc="settings-page"]` | The settings page. |
| `[data-mc="issue-detail"]` | An open issue, on its page or in the peek; `data-status`, `data-priority`, and `data-view` (`page` or `peek`). |
| `[data-mc="issue-title"]` | The issue's title above its description, reading and editing. |
| `[data-mc="issue-properties"]` | The properties sidebar next to an issue. |
| `[data-mc="issue-activity"]` | The activity and comments under an issue. |
| `[data-mc="object-page"]` | The page of one project, agent, squad, runtime, member, autopilot or skill; `data-type` is `project`, `agent`, `squad`, `runtime`, `member`, `autopilot` or `skill`. |
| `[data-mc="command-palette"]` | The search and command palette. |
| `[data-mc="palette-item"]` | A row in the palette; `data-type` is `command`, `page`, `workspace`, `issue`, `project`, `member`, `agent`, `squad`, `object` or `option` (a choice inside a palette page). The highlighted row has `data-selected`. |
| `[data-mc="avatar"]` | An avatar; `data-type` is `member`, `agent`, `squad` or `system`. |
| `[data-mc="empty-state"]` | A placeholder for something with nothing in it yet; `data-variant` is `page` (a centred state) or `section` (a quiet row in a page section). |
| `[data-mc="editor-toolbar"]` | The formatting toolbar over selected text; `data-mode` is `format` or `link`. |
| `[data-mc="editor-menu"]` | A menu floating over the editor while typing; `data-kind` is `slash` or `mention`. |
| `[data-mc="tab"]` | Desktop only: one tab in `tab-bar`; `data-active` and `data-pinned` are `true` or `false`. |

Each label chip sets `--label-color` (its hex colour) and `--label-foreground`
(a text colour readable on it). Read them to draw a label another way, such
as a dot in front of its name:

```css
[data-mc="label-chip"] { background: transparent; color: inherit; }
[data-mc="label-chip"]::before { content: ""; width: 0.5rem; height: 0.5rem; border-radius: 50%; background: var(--label-color); }
```

A plain toast has no `data-type`, so style the default look with both
selectors:

```css
[data-mc="toaster"] :is(li:not([data-type]), li[data-type="default"]) { border-radius: 0.75rem; }
```

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
  `--faint-foreground` 3:1. A frame (rules on the rail, window toolbar,
  tab bar or bottom bar) is checked against its own colours. With a
  backdrop, the frame and the page are checked as glass over
  `--backdrop-color`; see [Backdrop and glass](#backdrop-and-glass). The check follows `:root`,
  `html`, the mode classes and option attributes the way the browser does,
  with every option at its default; when a colour it measures is also set
  somewhere else, such as inside `@media` or on `body`, it says so instead
  of passing it.

## Embedded content

Plugin surfaces and HTML blocks (`` ```html `` fences in issues and chat)
run in sandboxed frames, so a theme's CSS does not reach them. Instead both receive the same
set of [embed tokens](#embed-token-reference) as custom properties on their
`:root`: surfaces and text, accents, status, priority and label colours,
chart series, code highlighting, the interface, content and code fonts, two
text sizes, radii and `--color-scheme`. Each one is resolved where the embed
sits, so the theme's values and its rules on an ancestor anchor apply, and
it crosses as a plain value: a colour, a length in pixels, a font stack or a
keyword. Nothing that could load a resource or read another variable gets
through.

They follow the theme and mode as they change: a plugin surface receives a
new set (`multica.ui.onThemeChange` in the
[SDK](../packages/plugin-sdk/README.md#theme)), and an HTML block that
uses any of them rebuilds with the new values, reruns its scripts and takes
the app's `color-scheme`. In an HTML block, `--background` is the
block's own surface, so a document that paints it sits flush in its frame.

```html
<div style="background: var(--surface); color: var(--foreground);
            border-radius: var(--radius-card); font: var(--text-body) var(--font-ui)">
  <span style="color: var(--status-done)">Done</span>
</div>
```

Fonts arrive as names: an embed shows the app's font only if the system has
it, and otherwise the next family in the stack.

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

### Ask an agent

Any agent can build a theme: ask for one in a chat, an issue or a comment.
The theme reference in Multica's built-in platform skill tells the agent to
read this guide, to pass `theme:check` with 0 errors before it delivers, and
to attach `theme:preview` screenshots when it can run them. It is a reference
inside the existing skill rather than a skill of its own, so it adds a line
to the skill's routing table instead of an entry to every agent's skill list.

- **Desktop on the agent's computer.** The agent writes the theme into the
  themes folder. Once it passes the checks it is listed under **Theme**,
  where you pick it once: a new theme never replaces the one in use by
  itself. Every later save to the theme in use applies within a second, so
  ask for changes and watch them land.
- **On the web, or with an agent on another machine.** The agent attaches
  the theme as a `.zip`. **Install theme** on the attachment reads it with
  the same checks and confirmation as **Import theme**; a newer version of
  the same id replaces the old one.

The checker runs without installing the repository, from a sparse clone
and Node 22.18 or newer:

```sh
git clone --depth 1 --filter=blob:none --sparse https://github.com/multica-ai/multica.git multica-themes
git -C multica-themes sparse-checkout set docs scripts packages/core packages/ui
node multica-themes/scripts/theme-check.mjs path/to/paper
```

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

Contrast is measured with every option at its default, then once for each
other value of a `select` and the other state of a `toggle`, one option at a
time with the rest at their defaults. Options are not combined. A failure
names the value it comes from, for example
`Contrast, dark mode (Screen = Amber): …`. Options that set a variable
(`color`, `variable-select`, `number`, `slider`, `text`, `font`) are measured
at their defaults only, so check the other values you offer yourself.

Rail items are measured the way the rail paints them: `--rail-item-color`
on `--app-shell` at rest, and `--rail-item-active-color` on
`--rail-item-active-background` (the current space) and on
`--rail-item-hover-background`, each with the fallback in the
[reference](#variable-reference) when it is not set. The default hover fill
is `--sidebar-accent` at 60% over `--app-shell`, and it is measured that way.
When a mode has frame rules, the rail takes the frame's colours and a failure
reads `Contrast, dark mode (frame): …`; without them it takes the page's
colours and reads `Contrast, light mode: …`, like the other page pairs.

Contrast covers the page, the frame, labels and two places a theme reaches
only indirectly. Labels are measured in each colour slot the theme
declares: `--label-chip-color` on the slot, or, when it is not set, whichever
of the dark and light text reads worse on it, since either can land there.
A failure reads `Contrast, light mode (label): …`. The tooltip and display
chips:

- **Tooltips** are always drawn with dark-mode text: the tooltip is a
  `.dark` scope, so its `--foreground` is the app's dark value in both
  modes, not the theme's `.theme-dark` one. The text is measured on
  `--tooltip` in each mode. Set `--tooltip-foreground` to choose it.
- **Pressed display chips** (the on/off chips in the Display and sidebar
  settings popovers) fill with `--input`, the input border colour, and use
  `--foreground`. They are measured inside the popover, with the rules a
  theme sets on `[data-slot="popover-content"]`. Set
  `--display-chip-pressed-background` and `--display-chip-pressed-color` to
  colour them apart from input borders.

Rules on a slot are applied by specificity, then order, as the browser
does. A variable set on `<html>` reaches the slot with the value it has
there: `--display-chip-pressed-color: var(--foreground)` on `.theme-light`
keeps the page's `--foreground`, even if the popover sets its own.

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
3. Add a file `themes/<id>.json`, named by the manifest's `id`, to the
   registry with a pull request. Each theme has its own file, so pull
   requests for different themes don't conflict. The pull request runs
   `theme:check` and `theme:preview` on the release and comments with the
   result; a maintainer merges once it passes.

New releases reach the store within about an hour, after they pass the same
checks; people who installed the theme then see **Update**. A release that
fails keeps the previous one listed. The app only installs the exact
`theme.zip` the checks passed, and reads it with the same parser and CSS
sanitizer as an imported theme.

Each store card shows how many times the theme was downloaded from the
Multica server it lists on: every install and every update counts once.
Themes not yet installed are listed most downloaded first. A self-hosted
server counts its own downloads.

A self-hosted server reads the official registry by default.
`MULTICA_THEME_REGISTRY_URL` points it at another registry's `index.json`.

## Variable reference

Generated from `packages/core/appearance/theme-variables.ts`. Where light
and dark differ, both defaults are given.

<!-- theme-variables:start (generated by packages/views/common/theme-variables.test.ts) -->

### Colours and surfaces

| Variable | Default (light / dark) | Use |
| --- | --- | --- |
| `--app-shell` | `oklch(0.964435 0.001327 286.375)` / `oklch(0.155 0.005 285.823)` | The frame around the page: rail, bottom bar, desktop toolbar and tab bar. |
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
| `--primary` | `oklch(0.21 0.006 285.885)` / `oklch(0.92 0.004 286.32)` | Primary buttons and strong marks; also used as text. Fills switches that are on unless `switch-checked-background` is set. |
| `--primary-foreground` | `oklch(0.985 0 0)` / `oklch(0.21 0.006 285.885)` | Text on primary. |
| `--secondary` | `oklch(0.967 0.001 286.375)` / `oklch(0.274 0.006 286.033)` | Secondary buttons. |
| `--secondary-foreground` | `oklch(0.21 0.006 285.885)` / `oklch(0.985 0 0)` | Text on secondary. |
| `--muted` | `oklch(0.967 0.001 286.375)` / `oklch(0.274 0.006 286.033)` | Quiet fills: group headers, code blocks, skeletons. |
| `--muted-foreground` | `oklch(0.505 0.016 285.938)` / `oklch(0.705 0.015 286.067)` | Secondary text. Must clear 4.5:1 on every surface. |
| `--faint-foreground` | `oklch(0.606 0.016 285.938)` / `oklch(0.60 0.015 286.067)` | Marks that are not text: chevrons, separators, empty-state icons. |
| `--accent` | `oklch(0.967 0.001 286.375)` / `oklch(0.274 0.006 286.033)` | Hover fill in menus and lists. |
| `--accent-foreground` | `oklch(0.21 0.006 285.885)` / `oklch(0.985 0 0)` | Text on accent. |
| `--border` | `oklch(0.945 0.003 286.32)` / `oklch(1 0 0 / 6%)` | Dividers and table borders. |
| `--input` | `oklch(0.92 0.004 286.32)` / `oklch(1 0 0 / 15%)` | Input borders. Also fills pressed display chips and switches that are off, unless `display-chip-pressed-background` and `switch-unchecked-background` are set. |
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
| `--pr-open` | `oklch(59.6% 0.145 163.225)` / `oklch(76.5% 0.177 163.223)` | Open pull request icon. |
| `--pr-merged` | `oklch(54.1% 0.281 293.009)` / `oklch(70.2% 0.183 293.541)` | Merged pull request icon and pill. The pill's text defaults one shade darker in light mode. |
| `--pr-closed` | `oklch(58.6% 0.253 17.585)` / `oklch(71.2% 0.194 13.428)` | Closed pull request icon. |

### Label and custom status colours

| Variable | Default (light / dark) | Use |
| --- | --- | --- |
| `--palette-gray` | the colour as picked | Labels, custom statuses and select options whose colour is the gray preset (`#6b7280`) or nearest to it. |
| `--palette-red` | the colour as picked | Labels, custom statuses and select options whose colour is the red preset (`#ef4444`) or nearest to it. |
| `--palette-orange` | the colour as picked | Labels, custom statuses and select options whose colour is the orange preset (`#f97316`) or nearest to it. |
| `--palette-yellow` | the colour as picked | Labels, custom statuses and select options whose colour is the yellow preset (`#eab308`) or nearest to it. |
| `--palette-green` | the colour as picked | Labels, custom statuses and select options whose colour is the green preset (`#22c55e`) or nearest to it. |
| `--palette-teal` | the colour as picked | Labels, custom statuses and select options whose colour is the teal preset (`#14b8a6`) or nearest to it. |
| `--palette-blue` | the colour as picked | Labels, custom statuses and select options whose colour is the blue preset (`#3b82f6`) or nearest to it. |
| `--palette-indigo` | the colour as picked | Labels, custom statuses and select options whose colour is the indigo preset (`#6366f1`) or nearest to it. |
| `--palette-purple` | the colour as picked | Labels, custom statuses and select options whose colour is the purple preset (`#a855f7`) or nearest to it. |
| `--palette-pink` | the colour as picked | Labels, custom statuses and select options whose colour is the pink preset (`#ec4899`) or nearest to it. |
| `--label-chip-color` | dark or light, whichever reads on the picked colour | Text on a label. Set it with the slots it has to read on; `[data-palette]` on the chip names its slot. |

### Shape and depth

| Variable | Default (light / dark) | Use |
| --- | --- | --- |
| `--radius` | `0.5rem` | Base corner radius. Every `rounded-*` step and component radius below derives from it. |
| `--surface-shadow` | `0 1px 2px rgb(15 23 42 / 0.04), 0 1px 1px rgb(15 23 42 / 0.03)` / `0 1px 2px rgb(0 0 0 / 0.2), 0 1px 1px rgb(0 0 0 / 0.16)` | Resting shadow of cards and the page. |
| `--floating-shadow` | `0 16px 40px rgb(15 23 42 / 0.14), 0 3px 10px rgb(15 23 42 / 0.08)` / `0 20px 48px rgb(0 0 0 / 0.46), 0 4px 12px rgb(0 0 0 / 0.28)` | Dialogs and sheets. |
| `--menu-shadow` | `0 8px 24px rgb(15 23 42 / 0.08), 0 2px 6px rgb(15 23 42 / 0.05)` / `0 10px 28px rgb(0 0 0 / 0.3), 0 2px 8px rgb(0 0 0 / 0.18)` | Menus, selects and popovers. |
| `--tooltip` | `oklch(0.21 0.006 285.885)` / `oklch(0.3 0.006 286.033)` | Tooltip fill (dark in both modes). |
| `--tooltip-foreground` | `var(--foreground)` | Tooltip text. Unset, it is the app's dark-mode `--foreground` in both modes, whatever the theme sets on `.theme-dark`. |
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
| `--display-chip-pressed-background` | `var(--input)` | Fill of a pressed on/off chip in the Display and sidebar settings popovers. |
| `--display-chip-pressed-color` | `var(--foreground)` | Text of a pressed display chip. |
| `--switch-checked-background` | `var(--primary)` | Track of a switch that is on. |
| `--switch-unchecked-background` | `var(--input)` / `color-mix(in oklab, var(--input) 80%, transparent)` | Track of a switch that is off. |
| `--switch-checked-thumb` | `var(--background)` / `var(--primary-foreground)` | Thumb of a switch that is on. |
| `--switch-unchecked-thumb` | `var(--background)` / `var(--foreground)` | Thumb of a switch that is off. |
| `--switch-border-color` | `transparent` | Outline of a switch, on or off. |
| `--popover-radius` | `var(--radius)` | Popovers. |
| `--floating-border-color` | `color-mix(in oklab, var(--surface-border), var(--foreground) 6%)` | Hairline around layers that float over the page without a backdrop: menus, popovers, select lists and floating panels. |
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
| `--nav-item-padding-y` | `0.375rem` | Top and bottom padding of a row in the space panel; with the text, it sets the row's height when that is above the minimum. |
| `--nav-item-radius` | `var(--radius)` | Rows in the space panel. |
| `--nav-item-color` | `var(--muted-foreground)` | Row text. |
| `--nav-item-hover-background` | `var(--surface-hover)` | Row hover. |
| `--nav-item-hover-color` | `var(--foreground)` | Row text on hover. |
| `--nav-item-active-background` | `var(--surface-selected)` | The current row. |
| `--nav-item-active-color` | `var(--surface-selected-foreground)` | Text of the current row. |

### Backdrop and glass

| Variable | Default (light / dark) | Use |
| --- | --- | --- |
| `--backdrop` | `none` | What the window shows behind the frame and the page, as `background` layers: colours, gradients and images in the theme folder, such as `url(sky.svg) center / cover no-repeat`. An SVG's own CSS animation plays. It shows through the glass below. |
| `--backdrop-color` | `var(--app-shell)` | Solid colour under the backdrop: shown while it loads, and the colour `theme:check` measures the glass on. Required with a backdrop; use the colour the backdrop averages to. |
| `--glass-opacity` | `100%` | How much of the page's own colour covers the backdrop: `100%` is solid, lower lets the backdrop through. Only applies with a backdrop. |
| `--glass-blur` | `0px` | Blur of the backdrop seen through the page. |
| `--frame-glass-opacity` | `var(--glass-opacity, 100%)` | How much of `app-shell` covers the backdrop in the frame: rail, toolbar, tab bar and bottom bar. |
| `--frame-glass-blur` | `var(--glass-blur, 0px)` | Blur of the backdrop seen through the frame. |

### Layout and spacing

| Variable | Default (light / dark) | Use |
| --- | --- | --- |
| `--space-xs` | `0.5rem` | Spacing step: the gap between board cards. |
| `--space-sm` | `0.75rem` | Spacing step: board card padding, the issue page's side padding on phones. |
| `--space-md` | `1rem` | Spacing step: the page's side padding. |
| `--space-lg` | `1.5rem` | Spacing step: an object page's side padding, the issue page's top padding on phones. |
| `--space-xl` | `2rem` | Spacing step: an object page's top padding, the issue page's padding. |
| `--page-padding-x` | `var(--space-md, 1rem)` | Side padding of page headers, the toolbars under them and full-width lists. |
| `--sidebar-panel-width` | `240px` | Starting width of the navigation panel, kept between 200px and 320px. Once the person drags its edge, their width wins. |
| `--content-max-width` | `clamp(56rem, 70cqi, 70rem)` | Widest the reading column gets on an issue page and in chat. |
| `--object-page-max-width` | `760px` | Widest the body of a project, agent, squad, runtime or other object page gets. |

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
| `--board-card-padding-y` | `var(--space-sm, 0.75rem)` | Top and bottom padding of a board card. |
| `--board-card-padding-x` | `0.625rem` | Side padding of a board card. |
| `--board-card-gap` | `var(--space-xs, 0.5rem)` | Space between cards in a board column. |

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

## Embed token reference

Generated from `packages/core/appearance/embed-tokens.ts`. Every token is
set when the host defines it; `--background`, colours and fonts follow the
[variables](#variable-reference) of the same name.

<!-- embed-tokens:start (generated by packages/views/common/embed-tokens.test.ts) -->

### Surfaces and text

| Token | Kind | Use |
| --- | --- | --- |
| `--background` | colour | What the embed sits on. In an HTML block, the block's own surface. |
| `--foreground` | colour | Body text. |
| `--muted-foreground` | colour | Secondary text. |
| `--faint-foreground` | colour | Hints and placeholders; not for text that must be read. |
| `--surface` | colour | A panel or card on the page. |
| `--surface-raised` | colour | A surface above another: menus, popovers. |
| `--surface-hover` | colour | A row or control under the pointer. |
| `--surface-selected` | colour | A selected row or control. |
| `--surface-border` | colour | The border of a surface. |
| `--page-canvas` | colour | The page behind the content column. |
| `--card` | colour | Card fill. |
| `--card-foreground` | colour | Text on a card. |
| `--popover` | colour | Popover fill. |
| `--popover-foreground` | colour | Text in a popover. |
| `--muted` | colour | A quiet fill: code, tags, tracks. |
| `--border` | colour | Dividers and outlines. |
| `--input` | colour | Input borders. |
| `--ring` | colour | Focus rings. |
| `--color-scheme` | keyword | `light` or `dark`, for `color-scheme: var(--color-scheme)`. |

### Accent and intent

| Token | Kind | Use |
| --- | --- | --- |
| `--primary` | colour | Primary buttons and strong accents. |
| `--primary-foreground` | colour | Text on `--primary`. |
| `--secondary` | colour | Secondary buttons. |
| `--secondary-foreground` | colour | Text on `--secondary`. |
| `--accent` | colour | Hover and highlight fill. |
| `--accent-foreground` | colour | Text on `--accent`. |
| `--brand` | colour | Links and the brand accent. |
| `--destructive` | colour | Errors and destructive actions. |
| `--success` | colour | Success. |
| `--warning` | colour | Warnings. |
| `--info` | colour | Information. |

### Status and priority

| Token | Kind | Use |
| --- | --- | --- |
| `--status-backlog` | colour | Backlog. |
| `--status-todo` | colour | Todo. |
| `--status-in-progress` | colour | In progress. |
| `--status-in-review` | colour | In review. |
| `--status-done` | colour | Done. |
| `--status-blocked` | colour | Blocked. |
| `--status-cancelled` | colour | Cancelled. |
| `--priority-urgent` | colour | Urgent priority. |
| `--priority-high` | colour | High priority. |
| `--priority-medium` | colour | Medium priority. |
| `--priority-low` | colour | Low priority. |
| `--priority-none` | colour | No priority. |

### Label colours

| Token | Kind | Use |
| --- | --- | --- |
| `--palette-gray` | colour | Labels and custom statuses in the gray slot, as the theme paints them. |
| `--palette-red` | colour | Labels and custom statuses in the red slot, as the theme paints them. |
| `--palette-orange` | colour | Labels and custom statuses in the orange slot, as the theme paints them. |
| `--palette-yellow` | colour | Labels and custom statuses in the yellow slot, as the theme paints them. |
| `--palette-green` | colour | Labels and custom statuses in the green slot, as the theme paints them. |
| `--palette-teal` | colour | Labels and custom statuses in the teal slot, as the theme paints them. |
| `--palette-blue` | colour | Labels and custom statuses in the blue slot, as the theme paints them. |
| `--palette-indigo` | colour | Labels and custom statuses in the indigo slot, as the theme paints them. |
| `--palette-purple` | colour | Labels and custom statuses in the purple slot, as the theme paints them. |
| `--palette-pink` | colour | Labels and custom statuses in the pink slot, as the theme paints them. |

### Charts

| Token | Kind | Use |
| --- | --- | --- |
| `--chart-1` | colour | Chart series 1. |
| `--chart-2` | colour | Chart series 2. |
| `--chart-3` | colour | Chart series 3. |
| `--chart-4` | colour | Chart series 4. |
| `--chart-5` | colour | Chart series 5. |

### Code highlighting

| Token | Kind | Use |
| --- | --- | --- |
| `--code-block-background` | colour | Code block fill. |
| `--code-block-color` | colour | Code block text. |
| `--code-keyword` | colour | Keywords and built-ins. |
| `--code-string` | colour | Strings and added lines. |
| `--code-number` | colour | Numbers and literals. |
| `--code-comment` | colour | Comments. |
| `--code-function` | colour | Function names. |
| `--code-type` | colour | Types and class names. |
| `--code-tag` | colour | Tag names. |
| `--code-property` | colour | Properties and attributes. |
| `--code-variable` | colour | Variables. |
| `--code-punctuation` | colour | Punctuation and tag brackets. |
| `--code-deletion` | colour | Deleted lines. |

### Type

| Token | Kind | Use |
| --- | --- | --- |
| `--font-ui` | font stack | Interface font stack. |
| `--font-sans` | font stack | The same as `--font-ui`. |
| `--font-content` | font stack | Font stack for rich text: descriptions, comments, documents. |
| `--font-mono` | font stack | Code font stack. |
| `--text-caption` | length (px) | Caption size, after the person's font size. |
| `--text-body` | length (px) | Body size, after the person's font size. |

### Shape

| Token | Kind | Use |
| --- | --- | --- |
| `--radius` | length (px) | Base corner radius. |
| `--radius-card` | length (px) | Cards and panels. |
| `--radius-button` | length (px) | Buttons. |
| `--radius-input` | length (px) | Inputs. |

<!-- embed-tokens:end -->
