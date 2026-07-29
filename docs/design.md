# Multica Design System

This document defines Multica's visual language and interaction rules. All UI work follows it.

Page-level information architecture and the page pattern layer are proposed in [the page-pattern decision record](decisions/proposed/architecture/2026-07-11-page-pattern-layer.md).

---

## 1. Principles

1. **Restraint reads as quality.** Subtract by default. Every element needs a reason to exist — a spare divider, a decorative icon, a just-in-case hint is noise. Whitespace is design.
2. **Hierarchy comes from grey; color is a signal.** The interface is mostly neutral. Color appears only to carry meaning: status, brand, error. When two regions compete for attention, the fix is to push one back, not to color both.
3. **Consistency beats personality.** The same kind of interaction gets the same feedback everywhere. A hover in the sidebar, a dropdown, and a table row should feel identical, and that comes from tokens rather than hardcoded values.

---

## 2. Color

Built on OKLCh and expressed as CSS variables. Use shadcn tokens; **never hardcode a Tailwind color** such as `text-gray-500` or `bg-blue-600`.

### 2.1 Surface levels

The surface system describes the relationship between containers. It is not an instruction to wrap every block of content in a card. Base tokens are in `packages/ui/styles/tokens.css` and cover light and dark together.

| Level | Token / class | Use for | Not for |
|---|---|---|---|
| App shell | `app-shell` / `bg-app-shell` | The outermost window, and the gap between sidebar and page canvas | Page body, form groups |
| Page canvas | `page-canvas` / `bg-page-canvas` | The page body: lists, boards, chat, anything that scrolls continuously | Standalone settings groups, overlays |
| Surface / card | `surface`, `surface-border`, `--surface-shadow` | Form groups, settings groups, and summary cards with their own boundary | Every list row, every board column, a whole page body |
| Floating surface | `surface-raised`, `--floating-shadow` | Dialog, dropdown, popover, sheet, floating chat | Persistent page layout |

Rules:

- Page canvas is the default content plane. To group content, reach for spacing and a divider first; use a surface only for an independently actionable or independently meaningful block.
- In light mode, `app-shell` and the sidebar are `#f3f3f4`, `page-canvas` is `#fbfbfb`, and a card is `#ffffff`. Do not rely on a border alone to separate a settings group from the page. The three form a restrained hierarchy that brightens from the outside in.
- Cards use `bg-surface border-surface-border shadow-[var(--surface-shadow)]`; overlays use `bg-surface-raised ring-surface-border shadow-[var(--floating-shadow)]`.
- `surface-hover` means the pointer is over it. `surface-selected` means sustained selection and stays neutral grey, without an added brand tint. A selected item under the pointer keeps `surface-selected` and must not fall back to hover.
- Keyboard focus is always a `focus-visible` ring. Never substitute a shadow, a size change, or a large brand fill.
- Do not hand-write parallel classes for light and dark. Base surface tokens already resolve per theme and sync native controls through `color-scheme`.

### 2.2 The neutral ramp

Neutrals cover about 90% of the interface, and the grey level *is* the information hierarchy.

| Role | Token | Use for |
|---|---|---|
| Background | `page-canvas` / `background` | Page body |
| Card / overlay | `surface` / `surface-raised` | Bounded content groups and temporary overlays |
| Secondary surface | `muted` / `secondary` | Hover backgrounds, tag fills |
| Border | `border` | Dividers, input borders |
| Input border | `input` | Slightly heavier than `border` |
| Primary text | `foreground` | Headings, body |
| Secondary text | `muted-foreground` | Descriptions, metadata, placeholders |
| Strongest text | `primary` | Button labels (inverted), key tags |

**Rule:** at most three text levels on one screen (`foreground`, `muted-foreground`, and one semantic color). A fourth means the hierarchy itself needs rethinking.

### 2.3 Semantic color

Color carries meaning; it does not decorate.

| Token | Meaning | Use for |
|---|---|---|
| `brand` | Brand identity | Logo, brand buttons, very sparing emphasis |
| `destructive` | Danger or error | Delete buttons, validation errors, destructive actions |
| `success` | Success | Status labels such as done or resolved |
| `warning` | Warning | Attention states, due reminders |
| `info` | Information | Hints, links, secondary markers |
| `priority` | Priority | High-priority labels |

Rules:

- Semantic color belongs on small elements — badge, icon, border. For a larger fill use a 10–20% alpha variant such as `bg-destructive/10`.
- No more than two or three semantic colors on one screen at once. Red, yellow, green, blue, and purple together means the information density is too high and the screen needs reorganizing, not recoloring.

### 2.4 Dark mode

Dark mode is designed, not inverted.

- Backgrounds are deep grey (`oklch(0.18 ...)`), not pure black, which glares on an LCD.
- Borders are `oklch(1 0 0 / 10%)` — white at 10% — subtler than in light mode.
- Semantic colors lift for contrast; `success` goes from `0.55` to `0.65`, for example.
- Verify every UI change in both modes.

---

## 3. Type

### 3.1 Families

| Role | Font | Use for |
|---|---|---|
| Body / UI | Inter (`--font-sans`) | The default for all interface text; CJK falls back automatically to PingFang SC, Microsoft YaHei, or Noto Sans CJK SC |
| Code / data | Geist Mono (`--font-mono`) | Code blocks, IDs, timestamps, monospaced data |
| Heading | `--font-heading` (= `--font-sans`) | Page and section titles |

The font stack is declared in both `apps/web/app/layout.tsx` and `apps/desktop/src/renderer/src/globals.css`; change them together.

### 3.2 Size discipline

**Three core sizes plus one special case, for the whole project:**

| Class | Size | Role | Use for |
|---|---|---|---|
| `text-base` (16px) | Body | Page titles, primary content | Page titles, editor body, empty-state copy |
| `text-sm` (14px) | Default | The workhorse | Menu items, buttons, forms, list rows, body text |
| `text-xs` (12px) | Supporting | Metadata, labels | Badge text, timestamps, status bar, secondary detail |
| `text-[0.8rem]` | Transitional | Small buttons only | shadcn `size="sm"` buttons |

Do not use:

- `text-lg`, `text-xl`, `text-2xl` and larger. A task tool optimizes for information density and does not need display type.
- Arbitrary pixel values such as `text-[11px]` or `text-[13px]`. Stay on the Tailwind scale.
- More than two sizes in one block. If a third seems necessary to separate levels, try `font-medium` against `font-normal`, or `text-muted-foreground`, first.

### 3.3 Weight

Two weights only.

| Weight | Use for |
|---|---|
| `font-normal` (400) | Body, descriptions, most text |
| `font-medium` (500) | Labels, buttons, navigation items, titles, selected state |

**Never** `font-bold` or `font-semibold`. Heavy text breaks the light, dense rhythm the product is going for. For stronger emphasis, use a larger size or `foreground`, not more weight.

---

## 4. Spacing

Built on Tailwind's 4px grid. Spacing carries information — it tells the reader what belongs to what.

### 4.1 What each step means

| Space | Tailwind | Meaning |
|---|---|---|
| 4px | `gap-1` / `p-1` | **Tightly bound** — icon and its label, label and value |
| 6px | `gap-1.5` / `p-1.5` | **Inside a component** — button padding, row spacing |
| 8px | `gap-2` / `p-2` | **Different items, same group** — between form fields, between list rows |
| 12px | `gap-3` / `p-3` | **Within a section** — card padding |
| 16px | `gap-4` / `p-4` | **Between groups** — separating blocks |
| 24px | `gap-6` / `p-6` | **Between major sections** — top-level page regions |

**Rule: needing a divider means the spacing is too tight.** Separate with more space before adding a `<Separator />`. A divider is the last resort.

### 4.2 Container strategy, lightest first

To separate two regions visually:

1. **Spacing alone** — increase the gap. Preferred.
2. **One divider** — a single hairline in `border-border`.
3. **A background change** — one region in `bg-surface-hover` or `bg-surface`.
4. **A full card** — border, radius, and padding. The heaviest tool.

Use the lightest one that works.

---

## 5. Interaction states

This is where consistency is won or lost. Each state must look the same across every component.

```
rest → hover → pressed → selected/active → focused → disabled
```

### 5.1 Hover

Hover says "I see you". The change is slight and immediate.

| Element | Effect | Token |
|---|---|---|
| List or menu item | Background lightens to grey | `hover:bg-muted` |
| Ghost button | Grey background, text to foreground | `hover:bg-muted hover:text-foreground` |
| Secondary button | Background deepens 20% | `hover:bg-secondary/80` |
| Primary button | Background deepens 20% | `hover:bg-primary/80` |
| Text link | Underline appears | `hover:underline` |
| Tab | Text goes from secondary to primary | `hover:text-foreground` |
| Icon button | Grey background | `hover:bg-muted` |
| Destructive button | Fill deepens | `hover:bg-destructive/20` |

Rules:

- Hover never changes size (no `scale`) and never adds a shadow.
- A hover background is always lighter than a selected one, so the two states stay distinguishable.
- Use `transition-colors`, `transition-shadow`, or an explicit property list — never `transition-all`. Tailwind's 150ms default is the right duration; do not override it.

### 5.2 Active and selected

Active says "I am chosen". It carries more weight than hover.

| Element | Effect | Token |
|---|---|---|
| Sidebar item | Background, heavier text, medium weight | `data-active:bg-sidebar-accent data-active:font-medium` |
| Tab | Underline indicator, foreground text, medium weight | `data-[state=active]:text-foreground` |
| Selected row | Deeper background | `bg-muted` or `bg-accent` |
| Toggle (on) | Inverted fill | `data-[state=on]:bg-primary data-[state=on]:text-primary-foreground` |

**The distinction that matters:** hover is `bg-muted`; active is `bg-muted` *plus* `font-medium` *plus* `text-foreground`. Active always adds a dimension hover does not have, rather than just a darker background.

### 5.3 Hover must not erase active

This is the easiest place to introduce a bug. The pointer moves onto an already-selected item, the hover style overrides the active style, and the selection appears to flicker off.

**Active state stays recognizable at all times, including under the pointer.** Three ways to get there:

**Use a dimension hover does not touch.** If hover only changes the background, express active through weight and text color, which hover leaves alone:

```
hover:bg-muted
data-active:font-medium data-active:text-foreground
```

**Define the compound state explicitly** when active also uses a background:

```tsx
cn(
  "hover:bg-muted/50",
  "data-active:bg-muted data-active:text-foreground",
  "data-active:hover:bg-muted",   // active + hover keeps the active background
)
```

Without that third line, hovering a selected row drops its background from `muted` back to `muted/50`.

**Or exclude active from hover entirely:**

```
[data-active]:bg-muted [data-active]:text-foreground
not-data-active:hover:bg-muted/50
```

**How to check:** after writing any component with both states, select an item, move the pointer onto it and away again, and confirm nothing flickers or downgrades.

### 5.4 Pressed

A 1px drop for physical feedback, already configured globally on the shadcn button:

```
active:not-aria-[haspopup]:translate-y-px
```

Buttons that open a popup are excluded — the press releases as the menu opens, so the shift only flickers.

### 5.5 Focus

Focus serves keyboard navigation. Every interactive element uses:

```
focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50
```

Use `focus-visible`, not `focus`, so a mouse click does not leave a ring. The ring uses the `ring` token — a mid grey — regardless of the component's own color, so it reads the same everywhere.

### 5.6 Disabled

```
disabled:pointer-events-none disabled:opacity-50
```

One treatment, no per-component variants.

### 5.7 Error and invalid

```
aria-invalid:border-destructive aria-invalid:ring-destructive/20
```

Triggered by `aria-invalid`, so it wires into form validation libraries directly. It changes the border and ring only, never the background. Error text is inline, not a toast or an alert banner.

---

## 6. Icons

**Lucide React** (`lucide-react`), exclusively. No mixing in Heroicons, Phosphor, or others, and no hand-drawn SVG unless Lucide genuinely lacks the shape.

Icon size is bound to component size:

| Component | Icon | Example |
|---|---|---|
| xs (`h-6`) | `size-3` (12px) | Compact buttons, icons inside badges |
| sm (`h-7`) | `size-3.5` (14px) | Small buttons, dense lists |
| default (`h-8`) | `size-4` (16px) | Standard buttons, menu items, table actions |
| lg (`h-9`) | `size-4` (16px) | Large buttons — the icon does not grow |

Rules:

- A standalone decorative icon, such as an empty-state illustration, tops out at `size-8` (32px).
- Icons inherit the parent's text color by default. To de-emphasize, use `text-muted-foreground`.
- Icon-to-text spacing: `gap-1` at xs, `gap-1.5` at sm and default, `gap-2` when the layout is loose.
- Navigation and action icons are `text-muted-foreground` and follow their label to `text-foreground` on hover. Status icons use their semantic color. Active icons are `text-foreground`.

---

## 7. Radius

A dynamic scale from `--radius: 0.625rem` (10px):

| Token | Value | Use for |
|---|---|---|
| `rounded-sm` | 6px | Checkbox, small tags |
| `rounded-md` | 8px | Inputs, small buttons, dropdown items |
| `rounded-lg` | 10px | Standard buttons, cards, dialogs |
| `rounded-xl` | 14px | Large cards, sheets |
| `rounded-full` | 999px | Avatars, pill badges |

**Never** hardcode a pixel radius such as `rounded-[6px]`, except where a shadcn component internally needs a responsive calculation like `rounded-[min(var(--radius-md),12px)]`.

---

## 8. Motion

- **Fast and restrained.** Motion helps a reader follow a change; it is not a demonstration.
- **Fade first.** Prefer an opacity transition for appearance and disappearance over a slide.
- **No bounce.** No spring or bounce easing. Use `ease-out`.

| Case | Duration |
|---|---|
| Color or opacity change | 150ms |
| Expand and collapse | 200ms |
| Overlay in and out | 150–200ms |
| Route change | None |

Prefer `transition-colors` for color changes, `transition-opacity` for fades, and `transition-transform` for the pressed offset. Reach for `transition-all` only when several properties genuinely change together.

---

## 9. Components

### 9.1 shadcn first

Use an installed shadcn component whenever one fits. For a new need: check whether shadcn has it and add it with `npx shadcn add <component>`; if a variant is needed, extend the existing component with CVA; only build from scratch when neither works, and then follow the tokens and interaction states in this document.

### 9.2 Button hierarchy

Strongest to weakest:

| Variant | Weight | Use for |
|---|---|---|
| `default` (primary) | ██████ | The page's main action |
| `outline` | ████░░ | Secondary actions |
| `secondary` | ███░░░ | Supporting actions, toolbars |
| `ghost` | █░░░░░ | Icon buttons, inline actions, dense toolbars |
| `destructive` | ████░░ | Delete and other destructive actions |
| `link` | █░░░░░ | Inline text links |

**Rule:** at most one primary button per view. If several actions are equally important, make them all `outline` or all `secondary`.

### 9.3 Dropdown and popover

- Width is `w-auto`. **Never** fix it with `w-52` or `w-56` — the text wraps.
- Menu items are `text-sm` with `size-4` icons.
- Mark the selected item with a checkmark or a leading indicator, not a background change.
- Destructive items use `text-destructive`, sit at the bottom, and are separated by a divider.

### 9.4 Form inputs

- Inputs use `border-input`, and `border-ring` plus a ring on focus.
- Labels are `text-sm font-medium`.
- Help text is `text-xs text-muted-foreground`.
- Error text is `text-xs text-destructive`, directly beneath the input.

---

## 10. Anti-patterns

| Do not | Why | Instead |
|---|---|---|
| Hardcode a color: `text-red-500`, `bg-gray-100` | Breaks theming | `text-destructive`, `bg-muted` |
| Arbitrary values: `text-[11px]`, `w-[137px]` | Leaves the system | Tailwind's scale |
| `font-bold` / `font-semibold` | Too heavy for a dense UI | `font-medium` with `text-foreground` |
| `text-lg` and larger | A density-first tool has no use for display type | `text-base` is the maximum |
| `shadow-sm` / `shadow-md` / `shadow-lg` | Skeuomorphic, fights the flat language | A `border` to separate levels |
| `scale-105` on hover | Abrupt, fights the restraint | `hover:bg-muted` |
| Multi-color gradient backgrounds | Decorative, distracting | A solid token |
| Skeleton loading | Does not match the plain style | A spinner (`Loader2Icon animate-spin`) or inline loading text |
| A toast to confirm an action | Transient; easy to miss | Inline status text; keep Sonner for errors and important notices |
| Fixed-width dropdowns: `w-52` | Uncontrollable wrapping | `w-auto` |
| Pure black: `#000` | Glares on an LCD | The dark-mode `background` token |

---

## 11. Before you submit a UI change

Every color a token, with nothing hardcoded. Sizes confined to `text-xs`, `text-sm`, and `text-base`. Weights confined to `font-normal` and `font-medium`. Hover lighter than active, and active still recognizable while hovered. Icon size matched to component size. Spacing on the Tailwind scale with no arbitrary values. Dark mode verified. No divider that spacing could have replaced. Dropdowns at `w-auto`. At most one primary button in the view.
