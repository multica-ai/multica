import type { ReactNode } from "react";
import Link from "next/link";

const LINK_PATTERN = /\[([^\]]+)\]\(([^)\s]+)\)/g;

export type InlineSegment =
  | { type: "text"; text: string }
  | { type: "link"; label: string; href: string };

// Splits dictionary copy on `[label](href)` so translations can place a link
// anywhere in a sentence without the component knowing the word order.
export function parseInlineLinks(text: string): InlineSegment[] {
  const segments: InlineSegment[] = [];
  let cursor = 0;
  for (const match of text.matchAll(LINK_PATTERN)) {
    const [raw, label, href] = match;
    if (match.index > cursor) {
      segments.push({ type: "text", text: text.slice(cursor, match.index) });
    }
    segments.push({ type: "link", label: label!, href: href! });
    cursor = match.index + raw.length;
  }
  if (cursor < text.length) {
    segments.push({ type: "text", text: text.slice(cursor) });
  }
  return segments;
}

export function InlineLinks({
  text,
  linkClassName = "font-medium text-[#0a0d12] underline decoration-[#0a0d12]/24 underline-offset-4 transition-colors hover:decoration-[#0a0d12]",
}: {
  text: string;
  linkClassName?: string;
}) {
  return parseInlineLinks(text).map((segment, i): ReactNode => {
    if (segment.type === "text") return segment.text;
    if (segment.href.startsWith("/")) {
      return (
        <Link key={i} href={segment.href} className={linkClassName}>
          {segment.label}
        </Link>
      );
    }
    return (
      <a
        key={i}
        href={segment.href}
        className={linkClassName}
        {...(segment.href.startsWith("http")
          ? { target: "_blank", rel: "noreferrer" }
          : {})}
      >
        {segment.label}
      </a>
    );
  });
}
