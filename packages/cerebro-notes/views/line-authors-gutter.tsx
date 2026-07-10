"use client";

// FIR-2810: the line-authors gutter — Apple Notes-style per-line attribution.
// Renders a narrow column to the left of the note body showing, for every
// rendered line, who wrote it (member code) and who last edited it. The data
// is the server's per-line attribution (see lineattr.go); the pairing with
// rendered blocks is content-based (see line-attr-map.ts) so a line the
// mapping cannot confidently place simply shows nothing.

import * as React from "react";
import { cn } from "@multica/ui/lib/utils";
import { memberCode } from "../core/member-code";
import type { NoteLineAttr } from "../core/types";
import { attrsForBlockTexts } from "./line-attr-map";

// The rendered blocks that correspond to markdown lines. Scoped to the
// .rich-text-editor root that both ContentEditor and ReadonlyContent render.
const BLOCK_SELECTOR = "p, h1, h2, h3, h4, h5, h6, pre";

export interface GutterMember {
  name: string;
}

interface GutterRow {
  top: number;
  createdCode: string;
  editedCode: string | null;
  title: string;
  time: string;
}

// shortTime renders a compact timestamp: clock time today, day + month within
// the current year, and day + month + year otherwise.
function shortTime(iso: string, now: Date): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const sameDay = d.toDateString() === now.toDateString();
  if (sameDay) {
    return d.toLocaleTimeString(undefined, {
      hour: "2-digit",
      minute: "2-digit",
    });
  }
  const sameYear = d.getFullYear() === now.getFullYear();
  return d.toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
    ...(sameYear ? {} : { year: "numeric" }),
  });
}

export function LineAuthorsGutter({
  contentRef,
  body,
  attrs,
  membersById,
  version,
  className,
}: {
  // The positioned wrapper that contains the rendered note body. Rows are
  // absolutely positioned relative to it.
  contentRef: React.RefObject<HTMLElement | null>;
  // The saved markdown body the attrs align with.
  body: string;
  attrs: NoteLineAttr[];
  membersById: Map<string, GutterMember>;
  // Bump to force a re-measure (the editor re-rendered its content).
  version: number;
  className?: string;
}) {
  const [rows, setRows] = React.useState<GutterRow[]>([]);

  const measure = React.useCallback(() => {
    const wrapper = contentRef.current;
    if (!wrapper) {
      setRows([]);
      return;
    }
    const root = wrapper.querySelector<HTMLElement>(".rich-text-editor");
    if (!root) {
      setRows([]);
      return;
    }
    const blocks = Array.from(root.querySelectorAll<HTMLElement>(BLOCK_SELECTOR))
      // A <p> inside <pre> (or similar nesting) would double-count its parent.
      .filter((el) => !el.parentElement?.closest("pre"));
    const mapped = attrsForBlockTexts(
      body,
      attrs,
      blocks.map((el) => el.textContent ?? ""),
    );
    const wrapperTop = wrapper.getBoundingClientRect().top;
    const now = new Date();
    const next: GutterRow[] = [];
    for (let i = 0; i < blocks.length; i++) {
      const block = blocks[i];
      const attr = mapped[i];
      if (!block || !attr || !attr.created_by) continue;
      const creator = membersById.get(attr.created_by);
      if (!creator) continue;
      const createdCode = memberCode(creator.name);
      const editor =
        attr.updated_by && attr.updated_by !== attr.created_by
          ? membersById.get(attr.updated_by)
          : undefined;
      const editedCode = editor ? memberCode(editor.name) : null;
      const timeIso = attr.updated_at || attr.created_at;
      const titleParts = [`Written by ${creator.name}`];
      if (editor) titleParts.push(`edited by ${editor.name}`);
      next.push({
        top: block.getBoundingClientRect().top - wrapperTop,
        createdCode,
        editedCode,
        title: titleParts.join(", "),
        time: shortTime(timeIso, now),
      });
    }
    setRows(next);
  }, [contentRef, body, attrs, membersById]);

  React.useLayoutEffect(() => {
    measure();
    const wrapper = contentRef.current;
    if (!wrapper || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => measure());
    ro.observe(wrapper);
    return () => ro.disconnect();
    // `version` intentionally re-runs the measurement after editor re-renders.
  }, [measure, version, contentRef]);

  return (
    <div
      aria-hidden
      className={cn(
        "pointer-events-none absolute inset-y-0 left-0 w-24 select-none",
        className,
      )}
    >
      {rows.map((row, i) => (
        <div
          key={i}
          title={row.title}
          className="pointer-events-auto absolute left-0 flex w-full flex-col items-end pr-3 text-right leading-tight"
          style={{ top: row.top }}
        >
          <span className="text-[11px] font-semibold text-muted-foreground">
            {row.createdCode}
            {row.editedCode && (
              <span className="font-normal"> · {row.editedCode}</span>
            )}
          </span>
          <span className="text-[10px] text-muted-foreground/70">
            {row.time}
          </span>
        </div>
      ))}
    </div>
  );
}
