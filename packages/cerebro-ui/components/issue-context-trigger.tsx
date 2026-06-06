"use client";

// CEREBRO-PATCH(issue-context-trigger): TECH-2969 — click/tap the issue title in the
// top bar to open a top sheet with the full title and the first lines of the
// description (scrollable). Lets a user catch up on context without scrolling
// the whole comment timeline to find what the issue is about.

import * as React from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";
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
  /**
   * Ref to the column container the panel should be constrained to.
   * The container must have `position: relative` applied.
   */
  containerRef?: React.RefObject<HTMLElement | null>;
}

export function IssueContextTrigger({
  fullTitle,
  description,
  identifier,
  className,
  children,
  containerRef,
}: IssueContextTriggerProps) {
  const [open, setOpen] = React.useState(false);
  const hasDescription =
    typeof description === "string" && description.trim().length > 0;

  React.useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [open]);

  const panel = open ? (
    <>
      {/* click-outside backdrop — fills the container */}
      <div
        className="absolute inset-0 z-40"
        aria-hidden
        onClick={() => setOpen(false)}
      />
      {/* the panel itself */}
      <div
        role="dialog"
        aria-modal="false"
        aria-label="Opgavekontekst"
        className="absolute inset-x-0 top-0 z-50 border-b bg-background shadow-md animate-in slide-in-from-top-2 duration-150"
      >
        <div className="flex items-start justify-between gap-2 px-4 pt-4 pb-2">
          <div className="flex flex-col gap-1 min-w-0">
            {identifier ? (
              <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                {identifier}
              </div>
            ) : null}
            <div className="text-base font-semibold leading-snug sm:text-lg">
              {fullTitle}
            </div>
          </div>
          <button
            type="button"
            onClick={() => setOpen(false)}
            aria-label="Luk"
            className="mt-0.5 shrink-0 rounded-sm p-1 text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
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
      </div>
    </>
  ) : null;

  const container = containerRef?.current ?? null;

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-label="Vis fuld kontekst for opgaven"
        aria-expanded={open}
        className={cn(
          "flex min-w-0 cursor-pointer items-center gap-1.5 rounded-sm text-left transition-colors hover:bg-muted/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1",
          className,
        )}
      >
        {children}
      </button>
      {panel && container ? createPortal(panel, container) : null}
    </>
  );
}
