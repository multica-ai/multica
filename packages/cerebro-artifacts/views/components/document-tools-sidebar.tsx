"use client";

import * as React from "react";
import { parseOutline, countDocument } from "@multica/cerebro-artifacts/core";

/**
 * Outline (heading navigation) + word/character count for a markdown body.
 * The outline scrolls to the Nth heading element inside `contentRef`, which
 * works for both the live editor and the read-only render (both emit real
 * <h1>–<h6> tags in document order). The heading navigator hides when there
 * are fewer than 2 headings — a single- or no-heading doc needs no navigator.
 *
 * Shared by the Documents view (DocumentViewPage) and the Notes view
 * (NotesPage) so both surfaces get the identical "Oversigt" + word count
 * (TECH-3637).
 */
export function DocumentToolsSidebar({
  body,
  contentRef,
}: {
  body: string;
  contentRef: React.RefObject<HTMLDivElement | null>;
}) {
  const outline = React.useMemo(() => parseOutline(body), [body]);
  const counts = React.useMemo(() => countDocument(body), [body]);

  const scrollToHeading = React.useCallback(
    (index: number) => {
      const root = contentRef.current;
      if (!root) return;
      const headings = root.querySelectorAll("h1, h2, h3, h4, h5, h6");
      const el = headings.item(index);
      if (el) el.scrollIntoView({ behavior: "smooth", block: "start" });
    },
    [contentRef],
  );

  return (
    <aside className="hidden w-56 shrink-0 lg:block">
      <div className="sticky top-4 flex flex-col gap-3">
        {outline.length >= 2 && (
          <nav aria-label="Dokument-oversigt" className="flex flex-col gap-1">
            <div className="px-2 text-xs font-medium text-muted-foreground">
              Oversigt
            </div>
            <ul className="flex flex-col">
              {outline.map((h) => (
                <li key={h.index}>
                  <button
                    type="button"
                    onClick={() => scrollToHeading(h.index)}
                    className="w-full truncate rounded px-2 py-1 text-left text-xs text-muted-foreground hover:bg-accent/40 hover:text-foreground"
                    style={{ paddingLeft: `${0.5 + (h.level - 1) * 0.6}rem` }}
                    title={h.text}
                  >
                    {h.text}
                  </button>
                </li>
              ))}
            </ul>
          </nav>
        )}
        <div className="px-2 text-xs text-muted-foreground">
          {counts.words} ord · {counts.characters} tegn
        </div>
      </div>
    </aside>
  );
}
