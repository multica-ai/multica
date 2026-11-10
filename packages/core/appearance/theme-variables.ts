/**
 * The public CSS variables a theme may set. This list is the contract:
 * docs/theming.md is generated from it, and a test checks that every
 * variable with a `default` is read in the stylesheets as
 * `var(--name, default)`, so an unset variable renders exactly as before.
 *
 * Foundation tokens (colours, fonts, the text scale, button geometry) are
 * declared in packages/ui/styles/tokens.css and appearance.css; their
 * defaults are read from there. Everything else is never declared: the
 * component reads it with a fallback, which is why a theme can set it on
 * any ancestor (`.theme-dark`, a `data-mc` anchor) and have it apply.
 *
 * Renaming or removing a variable breaks themes. Keep the old name working
 * until the next THEME_API_VERSION.
 */

export type ThemeVariableSection =
  | "colors"
  | "status"
  | "shape"
  | "typography"
  | "controls"
  | "navigation"
  | "issues"
  | "editor"
  | "code";

export interface ThemeVariable {
  /** Without the leading `--`. */
  name: string;
  section: ThemeVariableSection;
  description: string;
  /** The fallback in the stylesheet. Absent for foundation tokens. */
  default?: string;
  /** Only when dark mode falls back to something else. */
  darkDefault?: string;
  /** For the docs, when the default is composed outside the stylesheets. */
  defaultText?: string;
}

export const THEME_VARIABLE_SECTIONS: Record<ThemeVariableSection, string> = {
  colors: "Colours and surfaces",
  status: "Status and priority",
  shape: "Shape and depth",
  typography: "Typography",
  controls: "Controls and overlays",
  navigation: "Navigation and page",
  issues: "Issues",
  editor: "Editor content",
  code: "Code",
};

type Entry = readonly [name: string, description: string, def?: string, darkDef?: string];

const DEFAULT_TEXT: Record<string, string> = {
  "font-ui": "Inter, or the Appearance choice",
  "font-cjk": "system CJK sans, or the Appearance choice",
  "font-content-latin": "`--font-ui`",
  "font-content-cjk": "`--font-cjk`",
  "font-mono": "Geist Mono",
  "button-primary": "`--primary`",
  "button-primary-foreground": "`--primary-foreground`",
  "button-primary-border": "`--button-primary`",
};

function section(name: ThemeVariableSection, entries: readonly Entry[]): ThemeVariable[] {
  return entries.map(([variable, description, def, darkDef]) => ({
    name: variable,
    section: name,
    description,
    ...(def === undefined ? {} : { default: def }),
    ...(darkDef === undefined ? {} : { darkDefault: darkDef }),
    ...(DEFAULT_TEXT[variable] ? { defaultText: DEFAULT_TEXT[variable] } : {}),
  }));
}

const COLORS: Entry[] = [
  ["app-shell", "The frame around the page: rail, bottom bar, desktop toolbar and tab bar."],
  ["page-canvas", "The page that lists, boards and conversations sit on."],
  ["surface", "Bounded content groups: cards, board cards, panels."],
  ["surface-foreground", "Text on a surface."],
  ["surface-raised", "Menus, popovers and dialogs."],
  ["surface-hover", "Row and card hover."],
  ["surface-selected", "Selected rows and the active nav item."],
  ["surface-selected-foreground", "Text on a selected row."],
  ["surface-border", "Hairline around surfaces and the page."],
  ["background", "Shadcn alias; follows `page-canvas` unless set."],
  ["foreground", "Body text."],
  ["card", "Shadcn alias; follows `surface` unless set."],
  ["card-foreground", "Text on a card."],
  ["popover", "Shadcn alias; follows `surface-raised` unless set."],
  ["popover-foreground", "Text in menus and popovers."],
  ["primary", "Primary buttons and strong marks; also used as text."],
  ["primary-foreground", "Text on primary."],
  ["secondary", "Secondary buttons."],
  ["secondary-foreground", "Text on secondary."],
  ["muted", "Quiet fills: group headers, code blocks, skeletons."],
  ["muted-foreground", "Secondary text. Must clear 4.5:1 on every surface."],
  ["faint-foreground", "Marks that are not text: chevrons, separators, empty-state icons."],
  ["accent", "Hover fill in menus and lists."],
  ["accent-foreground", "Text on accent."],
  ["border", "Dividers and table borders."],
  ["input", "Input borders."],
  ["ring", "Focus ring."],
  ["sidebar", "Sidebar background (shadcn sidebar)."],
  ["sidebar-foreground", "Sidebar text."],
  ["sidebar-primary", "Sidebar primary marks."],
  ["sidebar-primary-foreground", "Text on sidebar primary."],
  ["sidebar-accent", "Rail hover and active fill."],
  ["sidebar-accent-foreground", "Text on sidebar accent."],
  ["sidebar-border", "Sidebar borders."],
  ["sidebar-ring", "Sidebar focus ring."],
  ["brand", "Product colour: links, checkboxes, selection highlights."],
  ["brand-foreground", "Text on brand."],
  ["button-primary", "Primary button fill when it should differ from `primary`."],
  ["button-primary-foreground", "Primary button text."],
  ["button-primary-border", "Primary button border."],
  ["destructive", "Errors and destructive actions."],
  ["success", "Success states."],
  ["warning", "Warnings."],
  ["info", "Informational marks."],
  ["chart-1", "First chart series."],
  ["chart-2", "Second chart series."],
  ["chart-3", "Third chart series."],
  ["chart-4", "Fourth chart series."],
  ["chart-5", "Fifth chart series."],
  ["find-match", "In-page find match."],
  ["find-match-active", "The current find match."],
  ["find-match-foreground", "Text on a find match."],
  ["scrollbar-track", "Scrollbar track."],
  ["scrollbar-thumb", "Scrollbar thumb."],
  ["scrollbar-thumb-hover", "Scrollbar thumb on hover."],
];

const STATUS: Entry[] = [
  ["status-backlog", "Backlog status icon.", "var(--muted-foreground)"],
  ["status-todo", "Todo status icon.", "var(--muted-foreground)"],
  ["status-in-progress", "In progress status icon.", "var(--warning)"],
  ["status-in-review", "In review status icon.", "var(--success)"],
  ["status-done", "Done status icon.", "var(--info)"],
  ["status-blocked", "Blocked status icon.", "var(--destructive)"],
  ["status-cancelled", "Cancelled status icon.", "var(--muted-foreground)"],
  ["priority-urgent", "Urgent priority icon and badge.", "var(--destructive)"],
  ["priority-high", "High priority icon and badge."],
  ["priority-medium", "Medium priority icon and badge.", "var(--warning)"],
  ["priority-low", "Low priority icon and badge.", "var(--info)"],
  ["priority-none", "No-priority icon.", "var(--muted-foreground)"],
];

const SHAPE: Entry[] = [
  ["radius", "Base corner radius. Every `rounded-*` step and component radius below derives from it."],
  ["surface-shadow", "Resting shadow of cards and the page."],
  ["floating-shadow", "Dialogs and sheets."],
  ["menu-shadow", "Menus, selects and popovers."],
  ["tooltip", "Tooltip fill (dark in both modes)."],
  ["tooltip-shadow", "Tooltip shadow."],
  ["icon-stroke-width", "Stroke width of line icons.", "2"],
];

const TYPOGRAPHY: Entry[] = [
  ["font-ui", "Interface font. The Appearance page's choice wins over a theme's."],
  ["font-cjk", "Interface font for Chinese, Japanese and Korean."],
  ["font-content-latin", "Font for issue descriptions, comments, chat and documents."],
  ["font-content-cjk", "CJK font for that content."],
  ["font-mono", "Code font."],
  ["content-leading", "Line height of content paragraphs.", "1.625"],
  ["content-leading-cjk", "Line height of CJK content paragraphs.", "1.75"],
  ["text-micro", "Counters, badges, timestamps."],
  ["text-micro--line-height", "Line height of `text-micro`."],
  ["text-caption", "Chips, hints, tooltips, issue identifiers."],
  ["text-caption--line-height", "Line height of `text-caption`."],
  ["text-label", "Metadata rows, table headers, activity."],
  ["text-label--line-height", "Line height of `text-label`."],
  ["text-body", "Interface body text."],
  ["text-body--line-height", "Line height of `text-body`."],
  ["text-body-lg", "Dialog copy, onboarding prose."],
  ["text-body-lg--line-height", "Line height of `text-body-lg`."],
  ["text-title-sm", "Small headings."],
  ["text-title-sm--line-height", "Line height of `text-title-sm`."],
  ["text-title", "Section headings."],
  ["text-title--line-height", "Line height of `text-title`."],
  ["text-title-lg", "Card and dialog titles."],
  ["text-title-lg--line-height", "Line height of `text-title-lg`."],
  ["text-display-sm", "Page titles."],
  ["text-display-sm--line-height", "Line height of `text-display-sm`."],
  ["text-display", "Hero numbers and empty-state headlines."],
  ["text-display--line-height", "Line height of `text-display`."],
];

const CONTROLS: Entry[] = [
  ["button-radius", "Button corners.", "var(--radius)"],
  ["button-radius-sm", "Corners of extra-small and small buttons.", "calc(var(--radius) * 0.75)"],
  ["button-height-xs", "Extra-small button height."],
  ["button-height-sm", "Small button height."],
  ["button-height-default", "Default button height."],
  ["button-height-lg", "Large button height."],
  ["button-padding-xs", "Extra-small button side padding."],
  ["button-padding-sm", "Small button side padding."],
  ["button-padding-default", "Default button side padding."],
  ["button-padding-lg", "Large button side padding."],
  ["button-gap-xs", "Icon gap in extra-small buttons."],
  ["button-gap-sm", "Icon gap in small buttons."],
  ["button-gap-default", "Icon gap in default buttons."],
  ["button-gap-lg", "Icon gap in large buttons."],
  ["input-radius", "Inputs, textareas and select triggers.", "var(--radius)"],
  ["card-radius", "Cards.", "calc(var(--radius) * 1.5)"],
  ["menu-radius", "Dropdown, context and select menus.", "calc(var(--radius) * 1.5)"],
  ["menu-item-radius", "Items inside a menu.", "calc(var(--radius) * 0.75)"],
  ["menu-item-hover-background", "Highlighted menu item.", "var(--accent)"],
  ["menu-item-hover-color", "Text of the highlighted menu item.", "var(--accent-foreground)"],
  ["popover-radius", "Popovers.", "var(--radius)"],
  ["dialog-radius", "Dialogs and alert dialogs.", "calc(var(--radius) * 1.5)"],
  ["tooltip-radius", "Tooltips.", "var(--radius)"],
  ["avatar-radius", "Avatars. Set a length for square avatars.", "calc(infinity * 1px)"],
  ["label-radius", "Issue label chips.", "calc(infinity * 1px)"],
];

const NAVIGATION: Entry[] = [
  ["page-radius", "Corners of the page card inside the frame.", "calc(var(--radius) * 1.5)"],
  ["page-border-color", "Hairline around the page card.", "var(--surface-border)"],
  ["page-shadow", "Shadow of the page card.", "var(--surface-shadow)"],
  ["rail-item-radius", "Space buttons in the rail.", "calc(var(--radius) * 1.5)"],
  ["rail-item-color", "Rail button icon and label.", "var(--muted-foreground)"],
  ["rail-item-hover-background", "Rail button hover.", "color-mix(in oklab, var(--sidebar-accent) 60%, transparent)"],
  ["rail-item-active-background", "The current space.", "var(--sidebar-accent)"],
  ["rail-item-active-color", "Icon and label of the current space.", "var(--sidebar-accent-foreground)"],
  ["nav-item-height", "Minimum height of a row in the space panel.", "2rem"],
  ["nav-item-radius", "Rows in the space panel.", "var(--radius)"],
  ["nav-item-color", "Row text.", "var(--muted-foreground)"],
  ["nav-item-hover-background", "Row hover.", "var(--surface-hover)"],
  ["nav-item-hover-color", "Row text on hover.", "var(--foreground)"],
  ["nav-item-active-background", "The current row.", "var(--surface-selected)"],
  ["nav-item-active-color", "Text of the current row.", "var(--surface-selected-foreground)"],
];

const ISSUES: Entry[] = [
  ["issue-row-height", "Height of a row in the issue list."],
  ["issue-row-padding-x", "Side padding of an issue row.", "1rem"],
  ["issue-row-hover-background", "Issue row hover.", "var(--surface-hover)"],
  ["issue-row-selected-background", "Selected issue row.", "var(--surface-selected)"],
  ["issue-row-identifier-color", "Issue identifier (MUL-123) in a row.", "var(--muted-foreground)"],
  ["issue-group-header-height", "Status group header in the list.", "2.5rem"],
  ["issue-group-header-radius", "Corners of a group header.", "var(--radius)"],
  ["issue-group-header-background", "Group header fill.", "var(--muted)"],
  ["issue-group-header-hover-background", "Group header hover.", "var(--accent)"],
  ["board-column-radius", "Board columns.", "calc(var(--radius) * 1.5)"],
  ["board-column-background", "Board column fill.", "color-mix(in oklab, var(--muted) 40%, transparent)"],
  ["board-card-radius", "Board cards.", "var(--radius)"],
  ["board-card-background", "Board card fill.", "var(--surface)"],
  ["board-card-hover-background", "Board card hover.", "var(--surface-hover)"],
  ["board-card-border-color", "Board card border.", "var(--surface-border)"],
  ["board-card-shadow", "Board card shadow.", "var(--surface-shadow)"],
];

const HEADING_SIZES = [
  "var(--text-display-sm)",
  "var(--text-title)",
  "var(--text-body-lg)",
  "var(--text-body)",
  "var(--text-label)",
  "var(--text-caption)",
];
const HEADING_LEADING = ["1.3", "1.35", "1.4", "1.45", "1.5", "1.5"];

const HEADINGS: Entry[] = HEADING_SIZES.flatMap((size, index): Entry[] => {
  const level = index + 1;
  const h = `h${level}`;
  return [
    [`${h}-font`, `Font of level ${level} headings.`, "var(--font-content)"],
    [`${h}-size`, `Size of level ${level} headings.`, size],
    [`${h}-weight`, `Weight of level ${level} headings.`, "600"],
    [`${h}-style`, `\`italic\` for italic level ${level} headings.`, "normal"],
    [`${h}-variant`, `\`small-caps\` and other font variants.`, "normal"],
    [`${h}-letter-spacing`, `Tracking of level ${level} headings.`, "normal"],
    [`${h}-line-height`, `Line height of level ${level} headings.`, HEADING_LEADING[index]],
    [`${h}-color`, `Colour of level ${level} headings.`, level === 6 ? "var(--muted-foreground)" : "currentColor"],
  ];
});

const EDITOR: Entry[] = [
  ...HEADINGS,
  ["paragraph-spacing", "Space above and below paragraphs.", "0.625rem"],
  ["list-indent", "Indent of bulleted lists.", "1.25rem"],
  ["list-indent-ordered", "Indent of numbered lists.", "1.5rem"],
  ["list-item-spacing", "Space between list items.", "0.25rem"],
  ["list-marker-color", "Bullets and numbers.", "var(--muted-foreground)"],
  ["checkbox-size", "Task list checkboxes.", "0.95rem"],
  ["checkbox-color", "Checked checkbox fill.", "var(--brand)"],
  ["checklist-done-color", "Text of a completed task.", "var(--muted-foreground)"],
  ["link-color", "Links in content.", "var(--brand)"],
  ["link-decoration", "`underline` or `none`.", "underline"],
  ["link-decoration-color", "Link underline.", "color-mix(in srgb, var(--brand) 40%, transparent)"],
  ["link-decoration-color-hover", "Link underline on hover.", "var(--brand)"],
  ["link-underline-offset", "Gap between text and underline.", "2px"],
  ["mention-color", "@mentions.", "var(--primary)"],
  ["mention-weight", "Weight of @mentions.", "600"],
  ["bold-weight", "Bold text.", "600"],
  ["bold-color", "Colour of bold text.", "currentColor"],
  ["italic-color", "Colour of italic text.", "currentColor"],
  ["strikethrough-color", "Colour of struck-through text.", "var(--muted-foreground)"],
  ["highlight-background", "`==highlighted==` text.", "color-mix(in srgb, #facc15 45%, transparent)"],
  ["highlight-radius", "Corners of highlighted text.", "0.2em"],
  ["blockquote-border-width", "Quote bar width.", "3px"],
  ["blockquote-border-color", "Quote bar colour.", "color-mix(in srgb, var(--muted-foreground) 30%, transparent)"],
  ["blockquote-padding", "Space between the bar and the text.", "0.75rem"],
  ["blockquote-color", "Quote text.", "var(--muted-foreground)"],
  ["blockquote-background", "Quote fill.", "transparent"],
  ["blockquote-style", "`italic` or `normal`.", "italic"],
  ["hr-color", "Horizontal rules.", "var(--border)"],
  ["hr-thickness", "Horizontal rule thickness.", "1px"],
  ["table-border-width", "Table borders.", "1px"],
  ["table-border-color", "Table border colour.", "var(--border)"],
  ["table-radius", "Table corners.", "var(--radius)"],
  ["table-header-background", "Header row fill.", "color-mix(in srgb, var(--muted) 50%, transparent)"],
  ["table-header-color", "Header text.", "var(--muted-foreground)"],
  ["table-header-weight", "Header weight.", "500"],
  ["table-row-hover-background", "Row hover.", "color-mix(in srgb, var(--muted) 30%, transparent)"],
  ["table-cell-padding", "Cell padding.", "0.5rem 0.75rem"],
];

const CODE: Entry[] = [
  ["code-font-size", "Inline code size.", "var(--text-body)"],
  ["code-background", "Inline code fill.", "color-mix(in srgb, var(--foreground) 3%, transparent)"],
  ["code-border-color", "Inline code border.", "color-mix(in srgb, var(--foreground) 5%, transparent)"],
  ["code-color", "Inline code text.", "color-mix(in srgb, var(--foreground) 75%, transparent)"],
  ["code-padding", "Inline code padding.", "0.125rem 0.375rem"],
  ["code-radius", "Inline code corners.", "var(--radius-sm)"],
  ["code-block-background", "Code block fill.", "var(--muted)"],
  ["code-block-border", "Code block border, e.g. `1px solid var(--border)`.", "none"],
  ["code-block-radius", "Code block corners.", "var(--radius)"],
  ["code-block-padding", "Code block padding.", "0.75rem 1rem"],
  ["code-block-color", "Code block text.", "var(--foreground)"],
  ["code-block-font-size", "Code block size.", "var(--text-label)"],
  ["code-block-line-height", "Code block line height.", "1.6"],
  ["code-keyword", "Keywords and built-ins.", "oklch(0.55 0.16 255)", "oklch(0.7 0.14 255)"],
  ["code-string", "Strings and added lines.", "oklch(0.55 0.14 155)", "oklch(0.7 0.14 155)"],
  ["code-comment", "Comments.", "var(--muted-foreground)"],
  ["code-number", "Numbers and literals.", "oklch(0.58 0.16 30)", "oklch(0.72 0.14 30)"],
  ["code-function", "Function and section names.", "oklch(0.55 0.14 280)", "oklch(0.72 0.12 280)"],
  ["code-property", "Attributes and properties.", "oklch(0.58 0.12 60)", "oklch(0.72 0.1 60)"],
  ["code-variable", "Variables.", "oklch(0.58 0.14 20)", "oklch(0.72 0.12 20)"],
  ["code-type", "Types and classes.", "oklch(0.55 0.14 200)", "oklch(0.72 0.12 200)"],
  ["code-deletion", "Removed lines.", "oklch(0.55 0.2 25)", "oklch(0.7 0.18 25)"],
  ["code-tag", "HTML and XML element names.", "oklch(0.55 0.16 255)", "oklch(0.72 0.14 255)"],
  ["code-punctuation", "HTML and XML brackets.", "var(--muted-foreground)"],
];

export const THEME_VARIABLES: readonly ThemeVariable[] = [
  ...section("colors", COLORS),
  ...section("status", STATUS),
  ...section("shape", SHAPE),
  ...section("typography", TYPOGRAPHY),
  ...section("controls", CONTROLS),
  ...section("navigation", NAVIGATION),
  ...section("issues", ISSUES),
  ...section("editor", EDITOR),
  ...section("code", CODE),
];

export const THEME_VARIABLE_NAMES: ReadonlySet<string> = new Set(THEME_VARIABLES.map((v) => v.name));
