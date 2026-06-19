// CEREBRO: FIR-1521 — shared formatting + sorting for pending wakeups, used by
// both the top-of-issue banner (CerebroWakeupBar) and the stacked activity-dot
// popover (CerebroIssueWakeupPip) so the countdown/label wording stays identical
// across surfaces. Pure functions only — no React, no API.

export interface Wakeup {
  id: string;
  trigger_type: string;
  fire_at?: string;
  watch_status?: string;
  prompt: string;
  created_at: string;
}

// "in 2d 4h" / "in 1h 14m" / "in 36m 12s" / "in 45s" / "any moment now" — counts
// down to fire time. Under an hour it ticks every second so the user can see the
// wakeup is live (Jesper feedback FIR-1521); further out, the two top units suffice.
// English copy throughout — the app UI is English, no Danish strings (Jesper FIR-1521).
export function formatCountdown(fireAt: string, now: number): string {
  const diff = new Date(fireAt).getTime() - now;
  if (diff <= 0 || Number.isNaN(diff)) return "any moment now";
  const s = Math.floor(diff / 1000);
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (d > 0) return `in ${d}d ${h}h`;
  if (h > 0) return `in ${h}h ${m}m`;
  if (m > 0) return `in ${m}m ${sec}s`;
  return `in ${sec}s`;
}

// "for 2h 14m" — how long a condition (status / CI) wakeup has been waiting.
export function formatWaiting(createdAt: string, now: number): string {
  const s = Math.max(0, Math.floor((now - new Date(createdAt).getTime()) / 1000));
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return `for ${d}d ${h}h`;
  if (h > 0) return `for ${h}h ${m}m`;
  if (m > 0) return `for ${m}m`;
  return `for ${s}s`;
}

// Headline label for a wakeup, independent of the live counter.
export function wakeupLabel(w: Wakeup): string {
  if (w.trigger_type === "issue_status" && w.watch_status)
    return `Waiting for status: ${w.watch_status}`;
  if (w.trigger_type === "github_ci") return "Waiting for CI";
  return "Scheduled run";
}

// The ticking counter shown after the label: a countdown for time triggers,
// elapsed-waiting for condition triggers.
export function wakeupCounter(w: Wakeup, now: number): string {
  if (w.trigger_type === "time" && w.fire_at) return formatCountdown(w.fire_at, now);
  return formatWaiting(w.created_at, now);
}

// Sort soonest-first: a concrete fire time wins over condition triggers, which
// fall back to oldest-waiting-first.
export function sortWakeups(wakeups: Wakeup[]): Wakeup[] {
  return [...wakeups].sort((a, b) => {
    const at = a.trigger_type === "time" && a.fire_at ? new Date(a.fire_at).getTime() : Infinity;
    const bt = b.trigger_type === "time" && b.fire_at ? new Date(b.fire_at).getTime() : Infinity;
    if (at !== bt) return at - bt;
    return new Date(a.created_at).getTime() - new Date(b.created_at).getTime();
  });
}
