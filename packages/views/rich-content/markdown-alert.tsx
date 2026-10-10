"use client";

/**
 * GitHub alerts: a top-level blockquote whose first line is `[!NOTE]`, `[!TIP]`,
 * `[!IMPORTANT]`, `[!WARNING]` or `[!CAUTION]` renders as a callout.
 *
 * Detection runs as a rehype step AFTER rehype-sanitize, so the callout's
 * markup never has to pass the sanitize schema and the schema stays as narrow
 * as it was. The step only tags the blockquote with a type from the fixed list
 * below; every class name and icon is a constant chosen by the renderer, never
 * copied from the input.
 *
 * Matching follows github.com: the marker is case-insensitive and must sit
 * alone on the quote's first line, the quote must sit at the top level, and a
 * marker with nothing under it stays a plain quote. The difference is that the
 * source of the quote's first paragraph must start with the marker, written
 * with literal brackets or backslash-escaped ones (`\[!NOTE\]`, which is how
 * the rich-text editor stores it). That check is what keeps quoted-reply
 * snapshots inert: they entity-encode brackets (`&#91;!NOTE&#93;`, see
 * composeAnnotatedReply), which decode to the same text node. It also leaves a
 * marker inside a raw HTML paragraph (`<p>[!NOTE]</p>`) as plain text.
 */

import type { ComponentPropsWithoutRef, ReactNode } from "react";
import type { Element, ElementContent, Root } from "hast";
import type { ExtraProps } from "react-markdown";
import {
  Info,
  Lightbulb,
  MessageSquareWarning,
  OctagonAlert,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

const ALERT_TYPES = ["note", "tip", "important", "warning", "caution"] as const;

type MarkdownAlertType = (typeof ALERT_TYPES)[number];

function isAlertType(value: unknown): value is MarkdownAlertType {
  return ALERT_TYPES.some((type) => type === value);
}

// The decoded first text node of the quote: the marker and nothing else.
const MARKER_TEXT = /^\[!([a-z]+)\][ \t]*$/i;
// The same marker in the source, which is what tells `\[` apart from `&#91;`:
// both decode to the same text node.
const MARKER_SOURCE = /^\\?\[!([a-z]+)\\?\]/i;

function isBlank(node: ElementContent): boolean {
  return node.type === "text" && node.value.trim() === "";
}

function markAlert(quote: Element, source: string): void {
  const index = quote.children.findIndex((child) => !isBlank(child));
  const paragraph = quote.children[index];
  if (paragraph?.type !== "element" || paragraph.tagName !== "p") return;

  const [marker, lineBreak] = paragraph.children;
  if (marker?.type !== "text") return;
  const type = MARKER_TEXT.exec(marker.value)?.[1]?.toLowerCase();
  if (!isAlertType(type)) return;
  // Alone on its line: remark-breaks turns the line end into a <br>, so
  // anything else after the marker means it shares the line with other text.
  if (lineBreak && !(lineBreak.type === "element" && lineBreak.tagName === "br")) return;

  // No position means the source cannot be checked; stay a plain quote.
  const start = paragraph.position?.start.offset;
  const end = paragraph.position?.end.offset;
  if (start == null || end == null) return;
  if (MARKER_SOURCE.exec(source.slice(start, end))?.[1]?.toLowerCase() !== type) return;

  // The marker line's own newline follows the <br> as a leading "\n".
  const rest = paragraph.children.slice(lineBreak ? 2 : 1);
  const head = rest[0];
  if (head?.type === "text") rest[0] = { ...head, value: head.value.replace(/^\n/, "") };
  const body = [...rest, ...quote.children.slice(index + 1)];
  if (body.every(isBlank)) return;

  if (rest.every(isBlank)) quote.children.splice(index, 1);
  else paragraph.children = rest;
  quote.properties = { ...quote.properties, dataAlert: type };
}

/** Rehype plugin. Must run after rehype-sanitize; see the module comment. */
export function rehypeMarkdownAlerts() {
  return (tree: Root, file: { value: unknown }) => {
    // micromark skips a leading BOM before it counts offsets; skip it here too
    // so node positions index this string.
    const source = String(file.value ?? "").replace(/^\uFEFF/, "");
    for (const node of tree.children) {
      if (node.type === "element" && node.tagName === "blockquote") markAlert(node, source);
    }
  };
}

// GitHub's hues, from the palette rather than the semantic tokens: there is no
// violet token, and --warning is too light for title text in light mode. Title
// text sits one shade darker than the bar in light mode so it keeps AA
// contrast, the same scheme as the pull-request state pills.
const ALERT_STYLE: Record<MarkdownAlertType, { icon: LucideIcon; bar: string; title: string }> = {
  note: {
    icon: Info,
    bar: "border-blue-600 dark:border-blue-400",
    title: "text-blue-700 dark:text-blue-400",
  },
  tip: {
    icon: Lightbulb,
    bar: "border-emerald-600 dark:border-emerald-400",
    title: "text-emerald-700 dark:text-emerald-400",
  },
  important: {
    icon: MessageSquareWarning,
    bar: "border-violet-600 dark:border-violet-400",
    title: "text-violet-700 dark:text-violet-400",
  },
  warning: {
    icon: TriangleAlert,
    bar: "border-amber-600 dark:border-amber-400",
    title: "text-amber-700 dark:text-amber-400",
  },
  caution: {
    icon: OctagonAlert,
    bar: "border-rose-600 dark:border-rose-400",
    title: "text-rose-700 dark:text-rose-400",
  },
};

function MarkdownAlert({ type, children }: { type: MarkdownAlertType; children?: ReactNode }) {
  const { t } = useT("editor");
  const { icon: Icon, bar, title } = ALERT_STYLE[type];
  return (
    <div className={cn("markdown-alert", bar)} data-alert={type}>
      <p className={cn("markdown-alert-title", title)}>
        <Icon className="size-4 shrink-0" aria-hidden="true" />
        {t(($) => $.alert[type])}
      </p>
      {children}
    </div>
  );
}

type RichBlockquoteProps = ComponentPropsWithoutRef<"blockquote"> & ExtraProps;

/** `blockquote` renderer: a callout when rehypeMarkdownAlerts tagged it. */
export function RichBlockquote({ node, children, ...props }: RichBlockquoteProps) {
  const type = node?.properties?.dataAlert;
  if (!isAlertType(type)) return <blockquote {...props}>{children}</blockquote>;
  return <MarkdownAlert type={type}>{children}</MarkdownAlert>;
}
