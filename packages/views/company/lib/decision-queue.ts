import { useEffect, useState } from "react";
import { isProposalSnoozed, nextSnoozeEnd, type Proposal } from "@multica/core/company";

export interface DecisionQueue {
  /** The "Needs your decision" list: pending proposals not put aside. */
  needsDecision: Proposal[];
  /** Pending proposals put aside with "decide later" until a date still ahead. */
  decidedLater: Proposal[];
  /** Proposals the boss sent back for changes, which the squad has not resubmitted. */
  waiting: Proposal[];
}

/**
 * Splits proposals into the queue, the ones put aside and the ones waiting on
 * the squad, at `nowMs`. A proposal in any other state is left out: nothing is
 * waiting on the boss for it.
 */
export function splitDecisionQueue(proposals: readonly Proposal[], nowMs: number): DecisionQueue {
  const needsDecision: Proposal[] = [];
  const decidedLater: Proposal[] = [];
  const waiting: Proposal[] = [];
  for (const proposal of proposals) {
    if (proposal.state === "revising") waiting.push(proposal);
    else if (proposal.state !== "pending") continue;
    else (isProposalSnoozed(proposal, nowMs) ? decidedLater : needsDecision).push(proposal);
  }
  return { needsDecision, decidedLater, waiting };
}

// setTimeout stops at about 24.8 days and a snooze can run for 30, so the timer
// is re-armed when it fires early.
const MAX_TIMER_MS = 2_147_483_647;

/**
 * The decision queue, kept current while the page is open: a snoozed proposal
 * returns to the queue when its date passes, without a refetch.
 */
export function useDecisionQueue(proposals: readonly Proposal[]): DecisionQueue {
  // Bumped when a snooze is due, so the page re-renders and the split reads the clock again.
  const [wake, setWake] = useState(0);

  // Split on every render: the lists are small, and this keeps the clock fresh
  // whenever the proposals change.
  const queue = splitDecisionQueue(proposals, Date.now());

  useEffect(() => {
    const now = Date.now();
    const end = nextSnoozeEnd(proposals, now);
    if (end === null) return;
    const timer = setTimeout(() => setWake((n) => n + 1), Math.min(end - now, MAX_TIMER_MS));
    return () => clearTimeout(timer);
  }, [proposals, wake]);

  return queue;
}
