"use client";

// FIR-4073 — the text half of a failed/paused alert row, shared by both.
//
// Two lines: what happened, and whose run it was. The whole block is the
// trigger that opens the run log, so the tap target is the full width of the
// row instead of a 22px icon.
//
// Overflow is the entire point of the class list. The old row let the headline
// keep its natural width (`shrink-0`) inside a flex parent with no
// `overflow-hidden`, so a long failure reason painted straight over the icon
// and the buttons next to it and swallowed their taps — that is the "cannot be
// opened" report. Here the block is `min-w-0 flex-1` and every line truncates,
// so the actions on the right always survive.

import { Loader2, ScrollText } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";

interface RunSummaryProps {
  /** What happened, in the row's own colour (destructive for failed, muted for paused). */
  headline: string;
  headlineClassName?: string;
  /** "Sara · 21:12 · attempt 2 of 3 · sara-mac" — see formatRunIdentity. */
  identity: string;
  onOpen?: () => void;
  loading?: boolean;
}

export function RunSummary({
  headline,
  headlineClassName,
  identity,
  onOpen,
  loading = false,
}: RunSummaryProps) {
  const body = (
    <>
      <span className={cn("w-full truncate font-medium", headlineClassName)}>{headline}</span>
      {identity ? (
        <span className="flex w-full min-w-0 items-center gap-1 text-[11px] text-muted-foreground">
          {onOpen ? (
            loading ? (
              <Loader2 aria-hidden="true" className="h-3 w-3 shrink-0 animate-spin" />
            ) : (
              <ScrollText aria-hidden="true" className="h-3 w-3 shrink-0" />
            )
          ) : null}
          <span className="truncate">{identity}</span>
        </span>
      ) : null}
    </>
  );

  if (!onOpen) {
    return (
      <div className="flex min-w-0 flex-1 flex-col items-start gap-0.5 text-xs">{body}</div>
    );
  }

  return (
    <button
      type="button"
      onClick={onOpen}
      disabled={loading}
      title="Open run log"
      aria-label={`Open run log — ${headline}`}
      className="-mx-1 flex min-w-0 flex-1 flex-col items-start gap-0.5 rounded px-1 py-0.5 text-left text-xs transition-colors hover:bg-accent/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring disabled:opacity-70"
    >
      {body}
    </button>
  );
}
