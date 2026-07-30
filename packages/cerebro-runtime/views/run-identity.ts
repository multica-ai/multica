// FIR-4073 — the "which agent, on which run" line under a failed/paused row.
//
// The alert row used to say only WHAT broke ("The model provider rejected the
// request"), which is useless on an issue that several agents work: you cannot
// tell whose run it was, nor which of the retries you are looking at. Every
// field needed is already on the wire (see DeadFailedRun) — this is pure
// presentation, no new endpoint.
//
// Pure function on purpose: `now` is injected so the test does not depend on
// the machine clock, exactly like pausedRunLabel.

/** Fields the identity line reads. Kept structural so both rows can pass a DeadFailedRun. */
export interface RunIdentityInput {
  agentName?: string;
  /** RFC3339 from the API. Absent while the run is still being written off. */
  completedAt?: string;
  attempt?: number;
  maxAttempts?: number;
  runtimeName?: string;
}

/**
 * "Sara · 21:12 · attempt 2 of 3 · sara-mac"
 *
 * Parts drop out when unknown rather than rendering "Unknown" noise, so the
 * line degrades to whatever we actually know. The order is deliberate: WHO
 * first (that is the question being asked), then WHEN, then WHICH attempt,
 * then WHERE it ran.
 */
export function formatRunIdentity(run: RunIdentityInput, now: Date): string {
  const parts: string[] = [];
  if (run.agentName) parts.push(run.agentName);

  const when = formatRunTime(run.completedAt, now);
  if (when) parts.push(when);

  // A single-attempt run has no "which retry" question to answer, so saying
  // "attempt 1 of 1" is pure noise. Only a real retry series earns the words.
  if (
    typeof run.attempt === "number" &&
    typeof run.maxAttempts === "number" &&
    run.maxAttempts > 1 &&
    run.attempt > 0
  ) {
    parts.push(`attempt ${run.attempt} of ${run.maxAttempts}`);
  }

  if (run.runtimeName) parts.push(run.runtimeName);
  return parts.join(" · ");
}

/**
 * Clock time for a run that failed today, date + clock for an older one. A
 * bare "21:12" on a three-day-old failure reads as "just now" and is the kind
 * of small lie that makes people distrust the whole bar.
 */
export function formatRunTime(completedAt: string | undefined, now: Date): string {
  if (!completedAt) return "";
  const at = new Date(completedAt);
  if (Number.isNaN(at.getTime())) return "";
  const time = at.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  if (isSameDay(at, now)) return time;
  const day = at.toLocaleDateString(undefined, { day: "numeric", month: "short" });
  return `${day} ${time}`;
}

function isSameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}
