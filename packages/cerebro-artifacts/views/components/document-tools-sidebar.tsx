"use client";

import * as React from "react";
import { List, PanelLeftClose, PanelLeftOpen } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { cn } from "@multica/ui/lib/utils";
import { parseOutline, countDocument } from "@multica/cerebro-artifacts/core";

/**
 * Outline (heading navigation) + word/character count for a markdown body.
 * The outline scrolls to the Nth heading element inside `contentRef`, which
 * works for both the live editor and the read-only render (both emit real
 * <h1>–<h6> tags in document order). The heading navigator hides when there
 * are fewer than 2 headings — a single- or no-heading doc needs no navigator.
 *
 * Shared by the Documents view (DocumentViewPage) and the Notes view
 * (NotesPage) so both surfaces get the identical outline + word count
 * (TECH-3637).
 *
 * FIR-1647 — the panel is now collapsible on desktop (a toggle hides it down to
 * a thin strip and restores it) and opens as a Sheet on mobile, where the aside
 * is otherwise hidden. Both surfaces inherit this because they both render this
 * one component.
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
      <div className="px-2 text-xs text-muted-foreground">
        {counts.words} words · {counts.characters} characters
      </div>
    </div>
  );
}

export function DocumentToolsSidebar({
  body,
  contentRef,
}: {
  body: string;
  contentRef: React.RefObject<HTMLDivElement | null>;
}) {
  const isMobile = useIsMobile();
  const [collapsed, setCollapsed] = React.useState(false);
  const [sheetOpen, setSheetOpen] = React.useState(false);

  // Mobile: the aside is hidden; reach the outline through a Sheet that opens
  // from a small floating button.
  if (isMobile) {
    return (
      <>
        <Button
          type="button"
          variant="outline"
          size="icon"
          onClick={() => setSheetOpen(true)}
          aria-label="Open outline"
          className="fixed bottom-4 right-4 z-30 size-10 rounded-full shadow-md lg:hidden"
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
            </div>
          </SheetContent>
        </Sheet>
      </>
    );
  }

  // Desktop: collapsible aside. Collapsed = a thin strip with just the toggle.
  return (
    <aside className={cn("hidden shrink-0 lg:block", collapsed ? "w-8" : "w-56")}>
      <div className="sticky top-4 flex flex-col gap-2">
        <button
          type="button"
          onClick={() => setCollapsed((c) => !c)}
          aria-label={collapsed ? "Expand outline" : "Collapse outline"}
          title={collapsed ? "Expand outline" : "Collapse outline"}
          className="self-end rounded p-1 text-muted-foreground hover:bg-accent/40 hover:text-foreground"
        >
          {collapsed ? (
            <PanelLeftOpen className="size-4" />
          ) : (
            <PanelLeftClose className="size-4" />
          )}
        </button>
        {!collapsed && <OutlineContent body={body} contentRef={contentRef} />}
      </div>
    </aside>
  );
}
