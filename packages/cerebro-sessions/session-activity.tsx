"use client";

import { useState } from "react";
import { ChevronRight } from "lucide-react";
import type { ReactNode } from "react";

// A whisper-quiet fold for a session's activity events (FIR-1769 P2).
// Collapsed by default so comments stay prominent; expanding reveals the
// activity rows rendered by the host (passed as children, so the existing
// ActivityBlock styling is reused rather than duplicated).
export function SessionActivity({
  count,
  children,
}: {
  count: number;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  if (count <= 0) return null;
  return (
    <div className="flex flex-col gap-3">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1.5 px-4 text-xs text-muted-foreground transition-colors hover:text-foreground"
      >
        <ChevronRight className={`h-3 w-3 shrink-0 transition-transform ${open ? "rotate-90" : ""}`} />
        <span>Activity ({count})</span>
      </button>
      {open ? children : null}
    </div>
  );
}
