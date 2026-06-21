"use client";

import { useState } from "react";
import { ChevronRight, ExternalLink } from "lucide-react";
import { Card } from "@multica/ui/components/ui/card";
import type { Session } from "./types";

export function SessionHandoff({ session }: { session: Session }) {
  const [open, setOpen] = useState(true);
  const handoff = session.handoff;
  const hasBrief =
    !!handoff &&
    (handoff.summary.trim() ||
      handoff.done.length > 0 ||
      handoff.remaining.length > 0 ||
      handoff.plan_ref);
  if (!handoff || !hasBrief) return null;

  return (
    <Card className="!py-0 !gap-0 overflow-hidden border-dashed">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 px-4 py-2 text-left text-xs font-medium text-muted-foreground hover:text-foreground"
      >
        <ChevronRight className={`h-3.5 w-3.5 transition-transform ${open ? "rotate-90" : ""}`} />
        <span>Handoff</span>
      </button>
      {open && (
        <div className="space-y-3 px-4 pb-4 text-sm">
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
