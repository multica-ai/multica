"use client";

import { useState } from "react";
import { ChevronRight, ExternalLink } from "lucide-react";
import { Card } from "@multica/ui/components/ui/card";
import { cn } from "@multica/ui/lib/utils";
import type { HandoffBrief, Session } from "./types";

// True when a handoff brief carries anything worth showing. Shared so the
// session header can decide whether to render its "Handoff" toggle button.
export function hasHandoffBrief(handoff: HandoffBrief | null | undefined): boolean {
  return (
    !!handoff &&
    (handoff.summary.trim() !== "" ||
      handoff.done.length > 0 ||
      handoff.remaining.length > 0 ||
      !!handoff.plan_ref)
  );
}

// FIR-1787: `bare` renders the handoff flush at the top of the session's thread
// box (under the header divider) with no card chrome of its own, so the session
// title + Handoff read as one box. Outside a box it keeps its dashed card.
//
// FIR-1839 point 7: when `controlled` is set, the toggle moves OUT of this
// component and into the session header (a "Handoff" button), so the brief feels
// like part of the headline and only appears when the header button is pressed.
// In that mode this renders just the body (gated by `open`) with no toggle row.
export function SessionHandoff({
  session,
  bare = false,
  controlled = false,
  open: openProp,
}: {
  session: Session;
  bare?: boolean;
  controlled?: boolean;
  open?: boolean;
}) {
  const [openState, setOpen] = useState(true);
  const open = controlled ? !!openProp : openState;
  const handoff = session.handoff;
  if (!hasHandoffBrief(handoff)) return null;
  if (controlled && !open) return null;

  return (
    <Card
      className={cn(
        "!py-0 !gap-0 overflow-hidden border-dashed",
        bare && "rounded-none border-0 border-b border-dashed bg-transparent shadow-none",
      )}
    >
      {!controlled && (
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className="flex w-full items-center gap-2 px-4 py-2 text-left text-xs font-medium text-muted-foreground hover:text-foreground"
        >
          <ChevronRight className={`h-3.5 w-3.5 transition-transform ${open ? "rotate-90" : ""}`} />
          <span>Handoff</span>
        </button>
      )}
      {open && handoff && (
        <div className={cn("space-y-3 px-4 text-sm", controlled ? "py-3" : "pb-4")}>
          {handoff.summary && <p className="text-muted-foreground">{handoff.summary}</p>}
          {handoff.done.length > 0 && (
            <div>
              <div className="mb-1 text-xs font-medium text-muted-foreground">Done</div>
              <ul className="list-disc space-y-1 pl-5">
                {handoff.done.map((item) => <li key={item}>{item}</li>)}
              </ul>
            </div>
          )}
          {handoff.remaining.length > 0 && (
            <div>
              <div className="mb-1 text-xs font-medium text-muted-foreground">Remaining</div>
              <ul className="list-disc space-y-1 pl-5">
                {handoff.remaining.map((item) => <li key={item}>{item}</li>)}
              </ul>
            </div>
          )}
          {handoff.plan_ref && (
            <a className="inline-flex items-center gap-1 text-xs text-primary hover:underline" href={handoff.plan_ref}>
              Plan <ExternalLink className="h-3 w-3" />
            </a>
          )}
        </div>
      )}
    </Card>
  );
}
