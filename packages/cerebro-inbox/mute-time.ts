/**
 * Compute the timestamp the inbox-mute action should mute until — the next
 * occurrence of 08:00 in the user's local timezone.
 *
 * If we are at exactly 08:00, the next occurrence is tomorrow's 08:00.
 */
export function nextLocalEightAm(now: Date = new Date()): Date {
  const target = new Date(now);
  target.setHours(8, 0, 0, 0);
  if (target.getTime() <= now.getTime()) {
    target.setDate(target.getDate() + 1);
  }
  return target;
}

/** Whether an inbox item is currently muted (muted_until is in the future). */
export function isMuted(mutedUntil: string | null | undefined, now: Date = new Date()): boolean {
  if (!mutedUntil) return false;
  return new Date(mutedUntil).getTime() > now.getTime();
}
