"use client";

import * as React from "react";
import { List } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { parseOutline, countDocument } from "@multica/cerebro-artifacts/core";

/**
 * Outline (heading navigation) + word/character count + References for a
 * markdown body. The outline scrolls to the Nth heading element inside
 * `contentRef`, which works for both the live editor and the read-only render
 * (both emit real <h1>–<h6> tags in document order). The heading navigator
 * hides when there are fewer than 2 headings — a single- or no-heading doc
 * needs no navigator.
 *
 * Shared by the Documents view (DocumentViewPage) and the Notes view
 * (NotesPage) so both surfaces get the identical panel (TECH-3637).
 *
 * FIR-4028 slice 7 — the desktop branch no longer holds a 224 px column. It is
 * an overlay panel layered over the content, opened by hovering the right edge
 * of the writing pane or by ⌘⇧O (which pins it until pressed again or Escape).
 * The mobile Sheet is unchanged. `references` is a slot rather than a direct
 * import because NoteReferences lives in cerebro-notes, which already depends
 * on this package — the same reason DocumentViewPage takes renderComments.
 */
function OutlineContent({
  body,
  contentRef,
  onNavigate,
}: {
  body: string;
  contentRef: React.RefObject<HTMLDivElement | null>;
  onNavigate?: () => void;
}) {
  const outline = React.useMemo(() => parseOutline(body), [body]);
  const counts = React.useMemo(() => countDocument(body), [body]);

  const scrollToHeading = React.useCallback(
    (index: number) => {
      const root = contentRef.current;
      if (root) {
        const headings = root.querySelectorAll("h1, h2, h3, h4, h5, h6");
        const el = headings.item(index);
        if (el) el.scrollIntoView({ behavior: "smooth", block: "start" });
      }
      onNavigate?.();
    },
    [contentRef, onNavigate],
  );

  return (
    <div className="flex flex-col gap-3">
      {outline.length >= 2 && (
        <nav aria-label="Document outline" className="flex flex-col gap-1">
          <div className="px-2 text-xs font-medium text-muted-foreground">
            Outline
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
      {/* A PDF or uploaded file has no markdown body — it opens this panel only
          for its References, so the count would read "0 words". */}
      {body.trim().length > 0 && (
        <div className="px-2 text-xs text-muted-foreground">
          {counts.words} words · {counts.characters} characters
        </div>
      )}
    </div>
  );
}

function ReferencesSection({ children }: { children: React.ReactNode }) {
  // NoteReferences renders nothing when a note has no references; empty:hidden
  // keeps the divider from showing above an empty section.
  return <div className="mt-4 border-t pt-3 empty:hidden">{children}</div>;
}

export function DocumentToolsSidebar({
  body,
  contentRef,
  references,
}: {
  body: string;
  contentRef: React.RefObject<HTMLDivElement | null>;
  // Rendered as its own section below the outline, on both surfaces.
  references?: React.ReactNode;
}) {
  const isMobile = useIsMobile();
  const [sheetOpen, setSheetOpen] = React.useState(false);
  // Two independent reasons the panel can be open: the pointer is on the edge
  // or inside the panel (transient), or ⌘⇧O pinned it (survives the pointer
  // leaving). Escape only clears the pin.
  const [hovering, setHovering] = React.useState(false);
  const [pinned, setPinned] = React.useState(false);

  React.useEffect(() => {
    if (isMobile) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (
        (event.metaKey || event.ctrlKey) &&
        event.shiftKey &&
        event.key.toLowerCase() === "o"
      ) {
        event.preventDefault();
        setHovering(false);
        setPinned((p) => !p);
        return;
      }
      if (event.key === "Escape") setPinned(false);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [isMobile]);

  // Mobile: the panel is hidden; reach the outline and References through a
  // Sheet that opens from a small floating button.
  if (isMobile) {
    return (
      <>
        <Button
          type="button"
          variant="outline"
          size="icon"
          onClick={() => setSheetOpen(true)}
          aria-label="Open outline"
          // The phone formatting row is fixed to the bottom edge and spans the
          // full width; it publishes the height it claims so this button steps
          // over it instead of landing on top of it.
          style={{ bottom: "calc(1rem + var(--cerebro-editor-dock-height, 0px))" }}
          className="fixed right-4 z-30 size-10 rounded-full shadow-md lg:hidden"
        >
          <List className="size-4" />
        </Button>
        <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
          <SheetContent
            side="right"
            className="w-[88vw] max-w-sm overflow-y-auto"
          >
            <SheetHeader>
              <SheetTitle>Outline</SheetTitle>
            </SheetHeader>
            <div className="mt-3">
              <OutlineContent
                body={body}
                contentRef={contentRef}
                onNavigate={() => setSheetOpen(false)}
              />
              {references && <ReferencesSection>{references}</ReferencesSection>}
            </div>
          </SheetContent>
        </Sheet>
      </>
    );
  }

  // Desktop: a zero-width anchor at the right edge of the writing pane. The
  // panel is absolute inside it, so it overlays the content and never sits on
  // top of a neighbouring rail (the comments panel renders after this one).
  const open = pinned || hovering;
  return (
    <div className="relative shrink-0">
      <div
        data-testid="document-tools-edge"
        aria-hidden="true"
        onMouseEnter={() => setHovering(true)}
        className="absolute inset-y-0 right-0 w-2"
      />
      {open && (
        <div
          data-testid="document-tools-panel"
          role="complementary"
          aria-label="Document tools"
          onMouseEnter={() => setHovering(true)}
          onMouseLeave={() => setHovering(false)}
          className="absolute right-0 top-0 z-30 max-h-[70vh] w-56 overflow-y-auto rounded-md border bg-popover p-3 shadow-lg"
        >
          <OutlineContent body={body} contentRef={contentRef} />
          {references && <ReferencesSection>{references}</ReferencesSection>}
        </div>
      )}
    </div>
  );
}
