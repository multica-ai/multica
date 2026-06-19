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

// "om 2d 4t" / "om 14m" / "om 45s" / "når som helst" — counts down to fire time.
export function formatCountdown(fireAt: string, now: number): string {
  const diff = new Date(fireAt).getTime() - now;
  if (diff <= 0 || Number.isNaN(diff)) return "når som helst";
  const s = Math.floor(diff / 1000);
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (d > 0) return `om ${d}d ${h}t`;
  if (h > 0) return `om ${h}t ${m}m`;
  if (m > 0) return `om ${m}m`;
  return `om ${sec}s`;
}

// "i 2t 14m" — how long a condition (status / CI) wakeup has been waiting.
export function formatWaiting(createdAt: string, now: number): string {
  const s = Math.max(0, Math.floor((now - new Date(createdAt).getTime()) / 1000));
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return `i ${d}d ${h}t`;
  if (h > 0) return `i ${h}t ${m}m`;
  if (m > 0) return `i ${m}m`;
  return `i ${s}s`;
}

// Headline label for a wakeup, independent of the live counter.
export function wakeupLabel(w: Wakeup): string {
  if (w.trigger_type === "issue_status" && w.watch_status)
    return `Venter på status: ${w.watch_status}`;
  if (w.trigger_type === "github_ci") return "Venter på CI";
  return "Planlagt kørsel";
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
