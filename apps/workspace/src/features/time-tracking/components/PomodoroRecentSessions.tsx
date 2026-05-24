import type { TimeEntry } from "@/shared/types";
import {
  formatDayLabel,
  formatDuration,
  groupByDay,
  sliceRecentGroups,
} from "../utils/pomodoro-dashboard";

interface PomodoroRecentSessionsProps {
  entries: TimeEntry[];
  expanded: boolean;
  onToggleExpanded: () => void;
}

function formatTime(isoString: string): string {
  return new Date(isoString).toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

export function PomodoroRecentSessions({
  entries,
  expanded,
  onToggleExpanded,
}: PomodoroRecentSessionsProps) {
  const grouped = sliceRecentGroups(groupByDay(entries), expanded);

  return (
    <section aria-label="Recent sessions" className="rounded-xl border bg-card p-4 sm:p-5">
      <div className="space-y-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h2 className="text-sm font-medium text-foreground">Recent sessions</h2>
            <p className="text-xs text-muted-foreground">
              History stays available, but below the active session.
            </p>
          </div>
          <button
            type="button"
            className="text-xs text-muted-foreground hover:text-foreground"
            onClick={onToggleExpanded}
          >
            {expanded ? "Collapse history" : "Expand full history"}
          </button>
        </div>
        <div className="space-y-4">
          {grouped.map(([dateKey, dayEntries]) => (
            <div key={dateKey}>
              <p className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                {formatDayLabel(dateKey)}
              </p>
              <div className="divide-y rounded-lg border">
                {dayEntries.map((entry) => (
                  <div key={entry.id} className="flex items-center gap-3 px-4 py-3 text-sm">
                    <span className="shrink-0">🍅</span>
                    <span className="shrink-0 font-medium tabular-nums">
                      {formatDuration(entry.duration_seconds)}
                    </span>
                    <span className="shrink-0 text-muted-foreground">
                      {formatTime(entry.start_time)}
                    </span>
                    <span className="min-w-0 truncate text-muted-foreground">
                      {entry.description ?? "Pomodoro session"}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
