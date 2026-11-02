import type { Proposal } from "./proposal-schema";

/** The earliest and latest dates a "decide later" date may name. */
export interface SnoozeDateBounds {
  /** Tomorrow, UTC (YYYY-MM-DD). Today's midnight UTC is already past. */
  min: string;
  /** Thirty days after today, UTC (YYYY-MM-DD). The server's limit is 30 days from now. */
  max: string;
}

const DAY_MS = 24 * 60 * 60 * 1000;

/**
 * The picker's bounds. A snooze date is a bare YYYY-MM-DD that the server reads
 * as midnight UTC, so the bounds are UTC calendar days too: tomorrow is always
 * later than now, and thirty days out is always within the server's limit.
 */
export function snoozeDateBounds(now: Date): SnoozeDateBounds {
  const today = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());
  return { min: utcDay(today + DAY_MS), max: utcDay(today + 30 * DAY_MS) };
}

/** True while the proposal is snoozed and the snooze has not yet ended at `nowMs`. */
export function isProposalSnoozed(proposal: Pick<Proposal, "snoozed_until">, nowMs: number): boolean {
  if (!proposal.snoozed_until) return false;
  const until = Date.parse(proposal.snoozed_until);
  return !Number.isNaN(until) && until > nowMs;
}

/** The earliest snooze end after `nowMs`, or null when no snooze is still running. */
export function nextSnoozeEnd(proposals: readonly Pick<Proposal, "snoozed_until">[], nowMs: number): number | null {
  let next: number | null = null;
  for (const proposal of proposals) {
    if (!isProposalSnoozed(proposal, nowMs)) continue;
    const until = Date.parse(proposal.snoozed_until as string);
    if (next === null || until < next) next = until;
  }
  return next;
}

function utcDay(ms: number): string {
  return new Date(ms).toISOString().slice(0, 10);
}
