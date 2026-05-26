import { useMemo, useState } from "react";
import { Timer } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { usePomodoroHistoryQuery } from "../hooks/use-pomodoro-history";
import { PomodoroTimer } from "../components/PomodoroTimer";
import { PomodoroTodaySummary } from "../components/PomodoroTodaySummary";
import { PomodoroRecentSessions } from "../components/PomodoroRecentSessions";
import { computeStreak, countTodayEntries } from "../utils/pomodoro-dashboard";

const TODAY_TARGET = 6;

// Fetch enough entries to cover a full year of streak computation for active users.
// A user doing ~10 sessions per day for 365 days produces up to 3 650 entries.
// Server defaults to limit=50 which would silently truncate active users and
// under-count streaks. 3 650 is a safe ceiling without unbounded pagination.
const HISTORY_LIMIT = 3650;

export function PomodoroPage() {
  const { data, isLoading } = usePomodoroHistoryQuery({ limit: HISTORY_LIMIT });
  const entries = data?.entries ?? [];

  // Compute today's count from entries using local midnight so it matches the
  // PomodoroRecentSessions "Today" group (both use localDateKey).
  // stats.today_count is UTC-midnight-based and can disagree with local grouping.
  const todayDone = useMemo(() => countTodayEntries(entries), [entries]);
  const streak = useMemo(() => computeStreak(entries), [entries]);
  const [historyExpanded, setHistoryExpanded] = useState(false);

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <header className="border-b px-6 py-4">
        <div className="mx-auto flex w-full max-w-5xl items-center gap-2">
          <Timer className="size-4 text-muted-foreground" />
          <div>
            <h1 className="text-base font-medium text-foreground">Pomodoro</h1>
            <p className="text-xs text-muted-foreground">
              Focus mode first. History stays below.
            </p>
          </div>
        </div>
      </header>

      <div className="flex-1 overflow-y-auto px-6 py-6">
        <div className="mx-auto flex w-full max-w-5xl flex-col gap-6">
          <div className="grid gap-6 lg:grid-cols-[minmax(0,1.4fr)_minmax(280px,0.8fr)]">
            <PomodoroTimer variant="page" />
            {isLoading ? (
              <section aria-label="Today" className="rounded-xl border bg-card p-4 sm:p-5">
                <Skeleton className="h-24 w-full rounded-lg" />
              </section>
            ) : (
              <PomodoroTodaySummary
                done={todayDone}
                target={TODAY_TARGET}
                streak={streak}
              />
            )}
          </div>

          {isLoading ? (
            <section aria-label="Recent sessions" className="rounded-xl border bg-card p-4 sm:p-5">
              <Skeleton className="h-32 w-full rounded-lg" />
            </section>
          ) : (
            <PomodoroRecentSessions
              entries={entries}
              expanded={historyExpanded}
              onToggleExpanded={() => setHistoryExpanded((v) => !v)}
            />
          )}
        </div>
      </div>
    </div>
  );
}
