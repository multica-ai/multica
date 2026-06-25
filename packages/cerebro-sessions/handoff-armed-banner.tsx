"use client";

import { ScrollText, X } from "lucide-react";

// FIR-1874: when a session is armed for handoff from the Send-button menu, the
// composer shows this banner as a slim bar ABOVE the field (not over it) so the
// user can see that their next message will start a fresh session carrying a
// summary of the chosen one. The handoff (close + summary) does not happen until
// Send — the X cancels it before then. (Jesper: it must sit above the input box,
// like the "Replying to X" bar, instead of covering the text.)
export function HandoffArmedBanner({
  name,
  onCancel,
}: {
  name: string;
  onCancel: () => void;
}) {
  return (
    <div className="mb-1.5 flex items-center gap-2 rounded-md border border-primary/30 bg-primary/5 px-3 py-1.5 text-xs text-primary">
      <ScrollText className="h-3.5 w-3.5 shrink-0" />
      <span className="min-w-0 flex-1 truncate">
        New session · Handoff from <span className="font-semibold">{name}</span>
      </span>
      <button
        type="button"
        onClick={onCancel}
        aria-label="Cancel handoff"
        className="shrink-0 rounded p-0.5 text-primary/70 transition-colors hover:bg-primary/10 hover:text-primary"
      >
        <X className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}
