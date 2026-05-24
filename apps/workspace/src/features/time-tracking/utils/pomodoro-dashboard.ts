import type { TimeEntry } from "@/shared/types";

export function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  return h > 0 ? `${h}h ${m}m` : `${m}m`;
}

export function localDateKey(isoString: string): string {
  const d = new Date(isoString);
  const y = d.getFullYear();
  const mo = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${mo}-${day}`;
}

export function formatDayLabel(dateStr: string): string {
  const today = localDateKey(new Date().toISOString());
  const yesterday = localDateKey(new Date(Date.now() - 86_400_000).toISOString());
  if (dateStr === today) return "Today";
  if (dateStr === yesterday) return "Yesterday";
  return new Date(`${dateStr}T12:00:00`).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });
}

export function groupByDay(entries: TimeEntry[]): [string, TimeEntry[]][] {
  const map = new Map<string, TimeEntry[]>();
  for (const entry of entries) {
    const key = localDateKey(entry.start_time);
    const bucket = map.get(key) ?? [];
    bucket.push(entry);
    map.set(key, bucket);
  }
  return [...map.entries()].sort((a, b) => b[0].localeCompare(a[0]));
}

export function computeStreak(entries: TimeEntry[]): number {
  if (entries.length === 0) return 0;
  const days = new Set(entries.map((entry) => localDateKey(entry.start_time)));
  // If today has no entry yet (e.g. right after midnight), start counting from
  // yesterday so a streak earned through yesterday is not zeroed out before the
  // user completes their first pomodoro of the new day.
  const today = localDateKey(new Date().toISOString());
  const startOffset = days.has(today) ? 0 : 1;
  let streak = 0;
  const now = new Date();
  for (let i = startOffset; i < 365; i += 1) {
    const day = new Date(now);
    day.setDate(day.getDate() - i);
    if (!days.has(localDateKey(day.toISOString()))) break;
    streak += 1;
  }
  return streak;
}

/**
 * Counts entries whose start_time falls on today in the browser's local timezone.
 * Using local midnight keeps this consistent with groupByDay/localDateKey so the
 * Today summary and Recent sessions grouping always agree.
 */
export function countTodayEntries(entries: TimeEntry[]): number {
  const today = localDateKey(new Date().toISOString());
  return entries.filter((e) => localDateKey(e.start_time) === today).length;
}

export function computeTodayProgress(done: number, target: number) {
  const safeTarget = Math.max(target, 1);
  const remaining = Math.max(safeTarget - done, 0);
  const percent = Math.min(Math.round((done / safeTarget) * 100), 100);
  return { remaining, percent };
}

export function sliceRecentGroups<T>(groups: [string, T[]][], expanded: boolean) {
  return expanded ? groups : groups.slice(0, 2);
}
