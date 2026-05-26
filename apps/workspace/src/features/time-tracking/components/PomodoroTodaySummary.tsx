import { computeTodayProgress } from "../utils/pomodoro-dashboard";

interface PomodoroTodaySummaryProps {
  done: number;
  target: number;
  streak: number;
}

export function PomodoroTodaySummary({ done, target, streak }: PomodoroTodaySummaryProps) {
  const { remaining, percent } = computeTodayProgress(done, target);

  return (
    <section aria-label="Today" className="rounded-xl border bg-card p-4 sm:p-5">
      <div className="space-y-4">
        <div>
          <h2 className="text-sm font-medium text-foreground">Today</h2>
          <p className="text-xs text-muted-foreground">
            Stay oriented without leaving the current session.
          </p>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <div className="rounded-lg bg-muted/40 p-3">
            <p className="text-xs text-muted-foreground">Focus target</p>
            <p className="text-base font-medium">{target} pomodoros</p>
          </div>
          <div className="rounded-lg bg-muted/40 p-3">
            <p className="text-xs text-muted-foreground">Done today</p>
            <p className="text-base font-medium">{done}</p>
          </div>
          <div className="rounded-lg bg-muted/40 p-3">
            <p className="text-xs text-muted-foreground">Remaining</p>
            <p className="text-base font-medium">{remaining}</p>
          </div>
          <div className="rounded-lg bg-muted/40 p-3">
            <p className="text-xs text-muted-foreground">Streak</p>
            <p className="text-base font-medium">{streak} days</p>
          </div>
        </div>
        <div className="space-y-2">
          <div className="flex items-center justify-between text-xs text-muted-foreground">
            <span>Progress</span>
            <span>{percent}%</span>
          </div>
          <div className="h-2 rounded-full bg-muted">
            <div className="h-2 rounded-full bg-foreground" style={{ width: `${percent}%` }} />
          </div>
        </div>
      </div>
    </section>
  );
}
