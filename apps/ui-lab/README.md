# Multica UI Lab

An internal, independently running design workbench for Multica Web and Desktop.

```sh
pnpm install
pnpm dev:ui-lab
```

Vite starts at http://127.0.0.1:4310 (or the next available port). No backend,
workspace, login, or environment file is required. This does not start Electron
or the public web application.

The interface defaults to English. Use EN / 中文 in the header to switch the
workbench and all previews together. The browser remembers your language; switching
languages preserves design overrides, saved schemes, and local sample edits.

## Catalog structure

The overview links to six modules: Foundations, Components, Patterns, Product,
Layouts, and Changes. Each module has an index and a collapsible navigation group.
Pages marked **To build** are placeholders; their content and editing controls are
not implemented yet. Button, Dialog and the component gallery live under
Components. Foundations → Colors edits semantic colors, and Motion previews shared Dialog motion; production issue list/detail previews live under Layouts. Changes
contains the working token review, saved designs, and CSS export pages. Impact
analysis and source application remain planned.

Pages have hash URLs (for example `#/components/button`) that support direct links,
reload, and browser back/forward. Drafts and edit history stay in the workbench
while navigating. Only live preview pages display the property inspector.

To add content, update the page entry in `src/catalog.ts`, provide the renderer,
and add English/Chinese copy in `src/locales/`. The registry supplies navigation,
module indexes, availability counts, and route validation. Preview scenes still
use the validated iframe protocol in `src/protocol.ts`.

## Workflow

1. Choose Button, Dialog, Motion, the component gallery, or an issue list/detail fixture.
2. Adjust a semantic color in the light or dark theme. Typography, radius and
   issue row height apply to both themes. Changes are isolated to the preview.
3. Switch to the original or compare both versions side by side. On small
   workbench windows the comparison scrolls horizontally instead of shrinking
   the previews to unreadable sizes.
4. Undo/redo edits, reset to the source defaults, or save a named scheme.
5. Export or copy CSS and merge the declarations into the matching `@theme`,
   `:root`, and `.dark` blocks in `packages/ui/styles/tokens.css`. The Lab never
   writes to source files. The CSS is a patch to merge, not a replacement file.

The current draft and up to 20 named schemes are stored in this browser's
localStorage, scoped to the dev server origin. Saved schemes contain absolute
overrides; reopening them against newer code applies them over the new defaults.
Defaults are read directly from the shared CSS, never from a second theme file.
This first version does not sync schemes or import files. Export CSS before
clearing browser data or changing ports. All sample edits stay inside the frame.

## Preview fidelity

- The gallery imports production primitives directly from `@multica/ui`. Its
  cards only arrange specimens; they do not override control styles. CSS resets
  remain in the base layer so they cannot override Tailwind component utilities.
- Business previews mount the production `IssuesPage`, `IssueDetail`, and
  `AppSidebar` from `@multica/views`. They include the real ListRow, toolbars,
  property pickers, rich-text editor, comment cards and responsive sidebar.
  There are no recreated list/detail/sidebar templates or page-specific CSS.
- `product-preview.tsx` supplies the platform providers used by these views:
  navigation, workspace identity, React Query, auth/chat stores, i18n, tooltips,
  sidebar and a disconnected realtime context. Preview assets load lazily; a
  ready handshake applies the latest design after the frame finishes mounting.
- `product-fixtures.ts` replaces only API results with typed, deterministic data.
  Each frame owns its fixture client. Issue edits and comments stay in memory;
  refreshing restores them. Unsupported operations reject explicitly and never
  fall through to the network. Navigation is limited to issue list/detail.
  This is a visual workbench, not a full backend simulator. Other groupings,
  advanced filter combinations, uploads and agent execution are not supported.
- `--issue-row-height` is used by the production `ListRow`. Its default remains
  36px. First-paint spacers use the same token before scroll restoration;
  virtualization starts from the rendered seed row height and measures subsequent
  changes. Table and board density are independent. Other design changes still
  export to the shared token file.
- The frame's sidebar follows product breakpoints. Use the real sidebar toggle
  when the preview width triggers automatic collapse. Theme, portals and fonts
  are isolated from the workbench controls and the original comparison frame.

## Verify

```sh
pnpm --filter @multica/ui-lab typecheck
pnpm --filter @multica/ui-lab lint
pnpm --filter @multica/ui-lab test
pnpm --filter @multica/ui-lab build
```

The pure tests cover source token parsing, CSS export scope, restored defaults,
undo/redo, persistence, the validated iframe message protocol, fixture isolation
and rejection of unsupported network operations. Browser smoke
checks should include theme/typography changes, original comparison, Dialog
portals, local scheme reload and CSS download.

## Button workbench

The Button page is the first dedicated component page. It mounts the shared
`Button` with all eight variants and eight sizes. The playground supports text,
leading/trailing icons, icon-only content, disabled, loading and invalid states.
Hover, focus and pressed feedback come from real pointer/keyboard interaction;
the gallery does not synthesize these pseudo states. Popover and Dialog examples
exercise the real portal components. Example submissions stay local.

The size selector controls the playground and variant matrix in both frames.
Twelve `--button-*` variables in `packages/ui/styles/tokens.css` cover height,
horizontal padding and icon/text gap for `xs`, `sm`, `default` and `lg`. Icon-only
buttons share the height ladder. The initial values preserve the production
geometry, including inline icon padding offsets. Explicit class overrides at
call sites retain precedence (for example `h-7`, `size-6` and `px-2`).

Button variables are shared between light/dark themes and export to `:root`;
typography still exports to `@theme`, and colors to `:root` / `.dark`. Existing
save, undo, redo, original comparison and CSS export apply to these variables.
Reset Button parameters clears only the button overrides. Exported CSS must be
merged into the shared source file; the workbench does not write source files.

## Color editor

Color roles appear as labeled swatch rows; click one to open the color picker.
The sRGB picker supports
pointer/touch dragging and keyboard adjustment, HEX input, quick colors, copy,
and original/current comparison. OKLCH controls remain under Precision adjustment.
A color-plane or hue drag previews live and commits one undo step on release.

Color.js converts edited HEX values to OKLCH for persistence/export. Opening the
picker does not quantize the source color. Out-of-sRGB colors retain their exact
OKLCH value; only the HEX/plane preview uses a gamut-mapped approximation. HEX
input accepts 3 or 6 digits; opacity has a separate numeric/scrub control. Color
and OKLCH edits preserve existing alpha, including transparent dark-mode borders. The chroma validation
range is 0–0.4 to include saturated sRGB colors such as pure blue.

## Property inspector

The right panel follows the compact property grouping and swatch-triggered popup
pattern described in [Figma's property panel guide](https://help.figma.com/hc/en-us/articles/360039832014-Design-prototype-and-explore-layer-properties-in-the-right-sidebar).
Design shows component dimensions separately from global appearance, colors and
typography. The Changes tab lists original/current values with per-token reset.

Numeric fields commit on Enter or blur, clamp to the supported bounds, and reject
non-numeric text. Arrow keys adjust by the field's step; Shift uses 10× and Alt
uses 0.1×. Drag the field prefix to scrub with live preview and one undo step on
release. Escape cancels a scrub or discards typed input. Color rows open the
existing color editor toward the canvas; Escape closes it and returns focus.


## Dialog and Motion

`#/components/dialog` and `#/foundations/motion` mount the real shared Dialog.
Choose a form or long-content composition. The frame toolbar opens, closes and
replays the dialog, including after its exit animation completes. Playback speed
is 1×, 0.5× or 0.25× and is local to each comparison frame; it is never saved or
exported as a product setting.

The inspector edits `--dialog-enter-duration`, `--dialog-exit-duration`,
`--dialog-enter-easing` and `--dialog-exit-easing`. Popup and backdrop consume
these variables. Existing 100ms/ease defaults are preserved, with shared easing
presets sourced from `@multica/ui/lib/motion`. Saved schemes, undo/redo and CSS
export include these tokens. They affect shared Dialog consumers only, not
Popover, Tooltip, AlertDialog or JavaScript animation constants.

Reduced motion disables popup/backdrop animation, including during slow playback.
Verify Escape, focus restoration, required field validation, long-content scroll,
rapid open/close, replay, and both themes. Frame commands accept only a known
same-origin parent and a validated action. Timeline scrubbing, recording and
frame-state export are not implemented.

## Component rules

Button and Dialog have a collapsed Usage & code section backed directly by
`packages/ui/docs/{button,dialog}{,.zh}.md`, with Markdown downloads. These files
are also readable by agents and document supported APIs, usage, correct/incorrect
examples, affected tokens and how to apply exported changes. Keep the rules next
to the production components up to date when their contracts change.


## Colors

`#/foundations/colors` exposes 43 existing semantic colors in seven groups:
surfaces, content, actions, feedback, borders/focus, charts and sidebar. Search
by localized label or token name, then select a swatch to edit it in the right
inspector. Original comparison, both themes, live preview, saved schemes,
undo/redo and CSS export use the same draft as all other component pages.

Background, card and popover aliases point to their existing source tokens;
selecting an alias edits its target rather than replacing the relationship.
HEX is a gamut-mapped RGB display; the separate opacity control preserves
transparency, while OKLCH retains the source color. Percentage and decimal
alpha are equivalent when restoring defaults. Specialty tokens such as scrollbar
and find highlights are not part of this initial catalog.

The **Usage & contrast** tab maps the current main action, active filter, line
Tabs, Input, focus and error roles to editable tokens. It mounts production
Button, Tabs and Input; hover/press/focus come from real browser interactions,
with local disabled/error toggles. Click a mapping token to edit it. The controls
keep their current production styles instead of simulating states with Lab CSS.

The light brand defaults to `#0070E3`. Dark mode retains its brighter source
brand because the same token also paints body-text links on dark surfaces.

Contrast is measured from computed styles after transitions, recalculated on
interaction, token edits and theme changes. The checker composites nested alpha
backgrounds and foregrounds in sRGB and compares unrounded WCAG ratios. Text is
checked at 4.5:1; focused borders at 3:1 against both adjacent surfaces, respecting
background clipping. Disabled controls are exempt. Unsupported backgrounds or
group opacity show “Not measured”, not a pass. This covers these sample color
pairs only: placeholders, tab indicators, focus-ring geometry and complete WCAG
compliance still need separate review. Current source colors may fail; the
checker reports failures without silently changing production tokens.

Use each sidebar’s header icon to collapse it; restore icons appear on the
corresponding side of the canvas heading. Desktop
panel preferences persist across routes and reloads, separately from design
drafts. On narrow screens the left toggle opens the navigation drawer; the
right panel closes from its own header and reopens from the canvas heading.
