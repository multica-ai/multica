"use client";

import { useT } from "../../i18n";

// Shared "running" stat for the header chip and the execution-log / popover
// rows so all three read identically: live event count (primary, info blue)
// followed by the elapsed time in muted parens — e.g. "58 events (2m45s)".
// Both the count and the elapsed are live (event count via the shared
// task-messages cache, elapsed via the caller's 1s tick).
export function RunningStat({
  eventCount,
  elapsed,
}: {
  eventCount: number;
  elapsed: string;
}) {
  const { t } = useT("issues");
  return (
    <span className="flex items-center gap-1 text-xs tabular-nums">
      <span className="text-info">
        {t(($) => $.agent_live.event_count, { count: eventCount })}
      </span>
      <span className="text-muted-foreground">({elapsed})</span>
    </span>
  );
}
