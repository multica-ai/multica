"use client";

// CEREBRO-PATCH(issue-context-trigger): TECH-2969 — click/tap the issue title in the
// top bar to open a top sheet with the full title and the first lines of the
// description (scrollable). Lets a user catch up on context without scrolling
// the whole comment timeline to find what the issue is about.

import * as React from "react";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { cn } from "@multica/ui/lib/utils";

export interface IssueContextTriggerProps {
  /** Full issue title — shown in the sheet header. */
  fullTitle: string;
  /** Issue description (markdown source); shown as a scrollable preview. */
  description?: string | null;
  /** Short identifier (e.g. "TECH-2969"); shown above the title in the sheet header. */
  identifier?: string | null;
  /** Extra classes for the trigger button (it lays out in the breadcrumb flex flow). */
  className?: string;
  /** Whatever was previously rendered inline as the title (kept identical for visual parity). */
  children: React.ReactNode;
}

export function IssueContextTrigger({
  fullTitle,
  description,
  identifier,
  className,
  children,
}: IssueContextTriggerProps) {
  const [open, setOpen] = React.useState(false);
  const hasDescription =
    typeof description === "string" && description.trim().length > 0;

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label="Vis fuld kontekst for opgaven"
        className={cn(
          "flex min-w-0 cursor-pointer items-center gap-1.5 rounded-sm text-left transition-colors hover:bg-muted/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1",
          className,
        )}
      >
        {children}
      </button>
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent
          side="top"
          className="max-h-[80vh] gap-0 rounded-b-lg pr-12"
        >
          <SheetHeader className="gap-1.5 pb-3">
            {identifier ? (
              <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                {identifier}
              </div>
            ) : null}
            <SheetTitle className="text-base leading-snug sm:text-lg">
              {fullTitle}
            </SheetTitle>
          </SheetHeader>
          <div className="border-t px-4 py-3">
            {hasDescription ? (
              <div
                className="max-h-[8rem] overflow-y-auto whitespace-pre-wrap break-words text-sm leading-relaxed text-foreground/90"
                data-testid="issue-context-description"
              >
                {description}
              </div>
            ) : (
              <div className="text-sm italic text-muted-foreground">
                Ingen beskrivelse endnu.
              </div>
            )}
          </div>
        </SheetContent>
      </Sheet>
    </>
  );
}
