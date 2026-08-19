"use client";

import { useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import type { ActivityEvent } from "@/lib/types";

const DOT_CLASSES: Record<ActivityEvent["type"], string> = {
  success: "bg-success",
  error: "bg-destructive",
  default: "bg-muted-foreground",
};

// Plan §2.2B: "Recent 10 events ... 'View all' link". The API already
// returns up to 50 real rows (lib/queries.ts's getRecentActivity) — "View
// all" just expands the slice client-side, no extra request and no
// fabricated data beyond what was already fetched.
const COLLAPSED_COUNT = 10;

function formatTimestamp(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

export function ActivityTimeline({ events }: { events: ActivityEvent[] }) {
  const [expanded, setExpanded] = useState(false);
  const visible = expanded ? events : events.slice(0, COLLAPSED_COUNT);

  return (
    <section>
      <h3 className="mb-3 text-label font-medium text-muted-foreground uppercase tracking-wide">
        Recent activity
      </h3>
      {events.length === 0 ? (
        <p className="text-body text-muted-foreground">No recent activity reported.</p>
      ) : (
        <>
          <ol className="space-y-3">
            {visible.map((event, i) => (
              <li key={i} className="flex items-start gap-2.5">
                <span className={cn("mt-1.5 size-1.5 shrink-0 rounded-full", DOT_CLASSES[event.type])} aria-hidden />
                <div>
                  <p className="text-body text-foreground">{event.text}</p>
                  <p className="text-caption text-muted-foreground">{formatTimestamp(event.at)}</p>
                </div>
              </li>
            ))}
          </ol>
          {!expanded && events.length > COLLAPSED_COUNT && (
            <button
              type="button"
              onClick={() => setExpanded(true)}
              className="mt-3 text-label font-medium text-primary hover:underline"
            >
              View all ({events.length})
            </button>
          )}
        </>
      )}
    </section>
  );
}
