# Theming Multica

A Multica theme is one JSON file. It sets design tokens for light and dark
mode, can declare options that appear under **Settings → Appearance → Theme
options**, and can ship its own CSS that targets the stable anchors listed
below. Themes never run JavaScript.

Install a theme with **Settings → Appearance → Import theme**. Themes are
saved on the device, like every other Appearance setting.

The built-in themes use exactly this format; Pop's options and CSS live in
`packages/core/appearance/themes.ts` and are a working example.

## The file

```json
{
  "apiVersion": 1,
  "id": "paper",
  "name": "Paper",
  "author": "Someone",
  "version": "1.2.0",
  "description": "Warm paper, serif reading text.",
  "tokens": {
    "light": { "page-canvas": "#f6efdf", "font-content-latin": "\"Paper Serif\", serif" },
    "dark": { "page-canvas": "oklch(0.2 0.01 60)" }
  },
  "frame": { "light": {}, "dark": {} },
  "options": [
    {
      "id": "header",
      "type": "select",
      "label": { "en": "Group headers", "zh-Hans": "分组标题" },
      "default": "block",
      "choices": [
        { "value": "flat", "label": "Flat" },
        { "value": "block", "label": "Block", "swatch": "oklch(0.85 0.1 90)" }
      ]
    },
    { "id": "hyphens", "type": "toggle", "label": "Hyphenate", "default": true }
  ],
  "css": "@font-face { font-family: \"Paper Serif\"; src: url(fonts/serif.woff2); }",
  "assets": { "fonts/serif.woff2": "data:font/woff2;base64,..." }
}
```

| Field | Rules |
| --- | --- |
| `apiVersion` | Must be `1`. |
| `id` | 1–40 characters of `a-z`, `0-9` and `-`. Built-in ids (`default`, `pop`, `github`, `blueprint`, `notion`) are reserved. Importing a theme with an installed id replaces it. |
| `name` | Required, at most 40 characters. |
| `author`, `version`, `description` | Optional, shown when installing. |
| `tokens.light`, `tokens.dark` | Token values per mode; see [Tokens](#tokens). Unknown names are ignored. |
| `frame` | Tokens for what sits on the frame around the page: the rail, the desktop toolbar and the tab bar. |
| `options` | Up to 12; see [Options](#options). |
| `css` | Up to 512 KB; see [CSS](#css). |
| `assets` | Up to 24 files the CSS can reference, as base64 `data:` URLs keyed by relative path. Fonts (`woff2`, `woff`, `ttf`, `otf`) and images (`png`, `jpeg`, `webp`, `gif`, `avif`, `svg+xml`). |

The whole file can be at most 1 MB. When a file fails a check, nothing is
installed and every problem is listed with its line in the CSS.

## Tokens

Token values may be colours (any CSS colour syntax), lengths, numbers,
shadows and `var()` references. Values that could load a resource or leave
the declaration are dropped.

What the person picks on the Appearance page wins over a theme: the accent
colour replaces `brand`, and font choices other than the defaults replace the
font tokens.

**Surfaces:** `app-shell` (the frame), `page-canvas`, `surface`,
`surface-foreground`, `surface-raised`, `surface-hover`, `surface-selected`,
`surface-selected-foreground`, `surface-border`, `surface-shadow`,
`floating-shadow` (dialogs), `menu-shadow` (menus and popovers), `tooltip`,
`tooltip-shadow`.

**Text and controls:** `background`, `foreground`, `card`,
`card-foreground`, `popover`, `popover-foreground`, `primary`,
`primary-foreground`, `secondary`, `secondary-foreground`, `muted`,
`muted-foreground`, `faint-foreground`, `accent`, `accent-foreground`,
`border`, `input`, `ring`, `radius`.

**Sidebar:** `sidebar`, `sidebar-foreground`, `sidebar-primary`,
`sidebar-primary-foreground`, `sidebar-accent`, `sidebar-accent-foreground`,
`sidebar-border`, `sidebar-ring`.

**Brand and buttons:** `brand` (links, active states), `brand-foreground`,
`button-primary`, `button-primary-foreground`, `button-primary-border`.
`primary` also colours text, so set `button-primary` when the button fill
would not work as a text colour.

**Status:** `destructive`, `success`, `warning`, `info`, `priority-high`.
Issue statuses use `warning` (started), `info` (done) and
`muted-foreground` (not started, closed).

**Charts, search and scrollbars:** `chart-1`, `chart-2`, `chart-3`,
`chart-4`, `chart-5`, `find-match`, `find-match-active`,
`find-match-foreground`, `scrollbar-track`, `scrollbar-thumb`,
`scrollbar-thumb-hover`.

**Layout and reading:** `issue-row-height`, `content-leading` (line height
of content text), `content-leading-cjk` (the same for Chinese, Japanese and
Korean).

**Fonts:** `font-ui` (Latin interface font), `font-cjk` (CJK interface
font), `font-content-latin` and `font-content-cjk` (content text: issue
descriptions, comments, chat and documents), `font-mono` (code). Values are
font stacks: family names, quoted or not, separated by commas, and `var()`
references. Load a font file with `@font-face` in the theme's CSS.

## Options

An option becomes an attribute on `<html>` while the theme is active, so the
theme's CSS can react to it:

- `toggle`: `data-theme-option-<id>="on"` when on, absent when off.
- `select`: `data-theme-option-<id>="<value>"`. Give every choice a `swatch`
  colour and the choices render as colour dots.

Option ids are 1–32 characters of `a-z`, `0-9` and `-`, starting with a
letter; choice values the same, starting with a letter or digit. `label`,
`description` and choice labels are a string or an object keyed by locale
(`en`, `zh-Hans`, `zh-Hant`, `ja`, `ko`, `es`, `fr`); English is the fallback.

```css
:root[data-theme-option-header="block"] [data-mc="issue-group-header"] {
  background: var(--muted);
}
```

## CSS

The CSS is injected after the app's own styles while the theme is active,
and removed when someone switches theme. At equal specificity, the theme
wins.

**Anything that loads a resource is rejected,** because CSS can read page
content through attribute selectors and send it out in a request. That
means:

- `url()` may only name a file in `assets` (`url(fonts/serif.woff2)`) or a
  fragment (`url(#filter)`). It is rewritten to the asset's `data:` URL.
- `@import`, `image()`, `image-set()`, `cross-fade()`, `element()`, `src()`
  and `expression()` are rejected, as are `behavior` and `-moz-binding`.

Escapes, case and comments do not get around the check: it reads the CSS the
way a browser does.

### Anchors

Target these, not class names or DOM structure: those change without
notice. Anchors and slots stay until the next `apiVersion`.

| Anchor | What it marks |
| --- | --- |
| `[data-mc="app-rail"]` | The icon rail on the far left. |
| `[data-mc="sidebar"]` | The navigation panel next to the rail; `data-state` is `expanded` or `collapsed`. |
| `[data-mc="page"]` | The rounded canvas that holds the current page. |
| `[data-mc="window-toolbar"]` | Desktop only: the toolbar above the rail. |
| `[data-mc="tab-bar"]` | Desktop only: the tab strip above the page. |
| `[data-mc="issue-group-header"]` | A status group header in issue lists; `data-status` is the status key (`backlog`, `todo`, `in_progress`, `in_review`, `done`, `blocked`, `cancelled`, or a custom key). |
| `[data-mc="issue-row"]` | One issue in a list. |
| `[data-mc="board-column"]` | One column on a board. |
| `[data-mc="board-card"]` | One issue card on a board. |
| `[data-mc="status-icon"]` | The status glyph (an `svg`). |
| `[data-mc="priority-icon"]` | The priority glyph (an `svg`). |
| `[data-mc="label-chip"]` | A label. |
| `[data-mc="content"]` | Rich text: issue descriptions, comments, chat messages and documents, reading and editing. `data-density` is `document` or `compact` when reading. |
| `[data-mc="comment"]` | A top-level comment card. |
| `[data-mc="comment-reply"]` | A reply inside a comment card. |
| `[data-mc="comment-composer"]` | The box for writing a new comment. |
| `[data-mc="chat-window"]` | The chat panel. |
| `[data-mc="chat-input"]` | The message box in chat. |
| `[data-mc="inbox-item"]` | One notification in the inbox. |
| `[data-mc="settings-page"]` | The settings page. |

UI primitives carry a `data-slot`; these are public too: `button` (also
`data-variant` and `data-size`), `input`, `textarea`, `switch`, `checkbox`,
`select-trigger`, `select-content`, `select-item`,
`dropdown-menu-content`, `dropdown-menu-item`, `popover-content`,
`tooltip-content`, `dialog-overlay`, `dialog-content`,
`alert-dialog-content`, `sheet-content`, `tabs-list`, `tabs-trigger`,
`card`, `kbd`, `separator`.

`<html>` carries `data-theme="<id>"`, the option attributes, and the `dark`
class in dark mode.

## Safe mode

A theme can hide or break parts of the interface. To get back, open the app
with `?theme=default` in the address, or hold Shift for a moment right after
it loads (on Desktop: reload with ⌘R / Ctrl+R, then hold Shift). The theme is
paused until the window is closed, and the Appearance page offers to turn it
back on.

## Writing a theme

1. Start from the example above or from Pop in
   `packages/core/appearance/themes.ts`.
2. Import the file. If a check fails, the dialog lists every problem.
3. Edit and import again: the same `id` replaces the installed copy, and
   option choices are kept.
