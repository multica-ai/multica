/**
 * The theme tokens a sandboxed embed receives: plugin surfaces and ```html
 * blocks get the same set, as custom properties on their `:root`. This list is
 * the public contract for embed authors; docs/theming.md is generated from it.
 *
 * Every value crosses into the frame resolved in the host to a plain colour,
 * length, font stack or keyword, never a `var()` or anything that could load a
 * resource. Renaming or removing a token breaks embeds; add instead.
 */

import { PALETTE_PRESETS, PALETTE_SLOTS } from "./palette";
import { THEME_VARIABLES } from "./theme-variables";

export type EmbedTokenKind = "color" | "length" | "font" | "keyword";

export type EmbedTokenGroup = "surfaces" | "intent" | "status" | "palette" | "chart" | "code" | "type" | "shape";

export interface EmbedToken {
  /** With the leading `--`. */
  name: string;
  kind: EmbedTokenKind;
  group: EmbedTokenGroup;
  description: string;
  /** What the host resolves, in its own stylesheet: `var(<name>)` when absent. */
  source?: string;
  /** In dark mode, when the app's fallback differs. */
  darkSource?: string;
}

export const EMBED_TOKEN_GROUPS: Record<EmbedTokenGroup, string> = {
  surfaces: "Surfaces and text",
  intent: "Accent and intent",
  status: "Status and priority",
  palette: "Label colours",
  chart: "Charts",
  code: "Code highlighting",
  type: "Type",
  shape: "Shape",
};

type Row = readonly [name: string, description: string];

const rows = (group: EmbedTokenGroup, kind: EmbedTokenKind, entries: readonly Row[]): EmbedToken[] =>
  entries.map(([name, description]) => ({ name: `--${name}`, kind, group, description }));

/** A component variable, read with the fallback the app reads it with. */
function themed(group: EmbedTokenGroup, kind: EmbedTokenKind, name: string, description: string): EmbedToken {
  const variable = THEME_VARIABLES.find((v) => v.name === name);
  if (!variable?.default) return { name: `--${name}`, kind, group, description };
  return {
    name: `--${name}`,
    kind,
    group,
    description,
    source: `var(--${name}, ${variable.default})`,
    ...(variable.darkDefault ? { darkSource: `var(--${name}, ${variable.darkDefault})` } : {}),
  };
}

const SURFACES = rows("surfaces", "color", [
  ["background", "What the embed sits on. In an HTML block, the block's own surface."],
  ["foreground", "Body text."],
  ["muted-foreground", "Secondary text."],
  ["faint-foreground", "Hints and placeholders; not for text that must be read."],
  ["surface", "A panel or card on the page."],
  ["surface-raised", "A surface above another: menus, popovers."],
  ["surface-hover", "A row or control under the pointer."],
  ["surface-selected", "A selected row or control."],
  ["surface-border", "The border of a surface."],
  ["page-canvas", "The page behind the content column."],
  ["card", "Card fill."],
  ["card-foreground", "Text on a card."],
  ["popover", "Popover fill."],
  ["popover-foreground", "Text in a popover."],
  ["muted", "A quiet fill: code, tags, tracks."],
  ["border", "Dividers and outlines."],
  ["input", "Input borders."],
  ["ring", "Focus rings."],
]);

const INTENT = rows("intent", "color", [
  ["primary", "Primary buttons and strong accents."],
  ["primary-foreground", "Text on `--primary`."],
  ["secondary", "Secondary buttons."],
  ["secondary-foreground", "Text on `--secondary`."],
  ["accent", "Hover and highlight fill."],
  ["accent-foreground", "Text on `--accent`."],
  ["brand", "Links and the brand accent."],
  ["destructive", "Errors and destructive actions."],
  ["success", "Success."],
  ["warning", "Warnings."],
  ["info", "Information."],
]);

const STATUS_NAMES: Row[] = [
  ["status-backlog", "Backlog."],
  ["status-todo", "Todo."],
  ["status-in-progress", "In progress."],
  ["status-in-review", "In review."],
  ["status-done", "Done."],
  ["status-blocked", "Blocked."],
  ["status-cancelled", "Cancelled."],
  ["priority-urgent", "Urgent priority."],
  ["priority-high", "High priority."],
  ["priority-medium", "Medium priority."],
  ["priority-low", "Low priority."],
  ["priority-none", "No priority."],
];

const CODE_NAMES: Row[] = [
  ["code-block-background", "Code block fill."],
  ["code-block-color", "Code block text."],
  ["code-keyword", "Keywords and built-ins."],
  ["code-string", "Strings and added lines."],
  ["code-number", "Numbers and literals."],
  ["code-comment", "Comments."],
  ["code-function", "Function names."],
  ["code-type", "Types and class names."],
  ["code-tag", "Tag names."],
  ["code-property", "Properties and attributes."],
  ["code-variable", "Variables."],
  ["code-punctuation", "Punctuation and tag brackets."],
  ["code-deletion", "Deleted lines."],
];

export const EMBED_TOKENS: readonly EmbedToken[] = [
  ...SURFACES,
  ...INTENT,
  ...STATUS_NAMES.map(([name, description]) => themed("status", "color", name, description)),
  ...PALETTE_SLOTS.map(
    (slot): EmbedToken => ({
      name: `--palette-${slot}`,
      kind: "color",
      group: "palette",
      description: `Labels and custom statuses in the ${slot} slot, as the theme paints them.`,
      source: `var(--palette-${slot}, ${PALETTE_PRESETS[slot]})`,
    }),
  ),
  ...rows("chart", "color", [1, 2, 3, 4, 5].map((n): Row => [`chart-${n}`, `Chart series ${n}.`])),
  ...CODE_NAMES.map(([name, description]) => themed("code", "color", name, description)),
  { name: "--font-ui", kind: "font", group: "type", description: "Interface font stack.", source: "inherit" },
  { name: "--font-sans", kind: "font", group: "type", description: "The same as `--font-ui`.", source: "inherit" },
  { name: "--font-content", kind: "font", group: "type", description: "Font stack for rich text: descriptions, comments, documents." },
  { name: "--font-mono", kind: "font", group: "type", description: "Code font stack." },
  { name: "--text-caption", kind: "length", group: "type", description: "Caption size, after the person's font size." },
  { name: "--text-body", kind: "length", group: "type", description: "Body size, after the person's font size." },
  { name: "--radius", kind: "length", group: "shape", description: "Base corner radius." },
  {
    name: "--radius-card",
    kind: "length",
    group: "shape",
    description: "Cards and panels.",
    source: "var(--card-radius, calc(var(--radius) * 1.5))",
  },
  {
    name: "--radius-button",
    kind: "length",
    group: "shape",
    description: "Buttons.",
    source: "var(--button-radius, var(--radius))",
  },
  {
    name: "--radius-input",
    kind: "length",
    group: "shape",
    description: "Inputs.",
    source: "var(--input-radius, var(--radius))",
  },
  { name: "--color-scheme", kind: "keyword", group: "surfaces", description: "`light` or `dark`, for `color-scheme: var(--color-scheme)`." },
];

export const EMBED_TOKEN_NAMES: ReadonlySet<string> = new Set(EMBED_TOKENS.map((token) => token.name));

/**
 * Every custom property the tokens are resolved from: their own names and the
 * variables their sources read, such as `--card-radius` behind `--radius-card`.
 */
export const EMBED_TOKEN_INPUTS: readonly string[] = [
  ...new Set(
    EMBED_TOKENS.flatMap((token) => [
      token.name,
      ...`${token.source ?? ""} ${token.darkSource ?? ""}`.match(/--[\w-]+/g) ?? [],
    ]),
  ),
];

/** Longest value passed on; a font stack is the longest legitimate one. */
const MAX_VALUE_LENGTH = 512;

// Rejects anything that could close the style rule or element, escape a
// string, load a resource, or read another variable inside the frame.
const UNSAFE_VALUE = /[<>{};\\]|url\s*\(|image(?:-set)?\s*\(|src\s*\(|var\s*\(|@|\/\*/i;

/** Whether a resolved value is plain enough to hand to an embed. */
export function isPlainEmbedValue(value: string): boolean {
  return value.length > 0 && value.length <= MAX_VALUE_LENGTH && !UNSAFE_VALUE.test(value);
}

/** `:root` declarations for the tokens that are safe, in contract order. */
export function embedTokenDeclarations(tokens: Readonly<Record<string, string>>): string {
  return EMBED_TOKENS.flatMap((token) => {
    const value = tokens[token.name]?.trim();
    return value && isPlainEmbedValue(value) ? [`${token.name}:${value}`] : [];
  }).join(";");
}
