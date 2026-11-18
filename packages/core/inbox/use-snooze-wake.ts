import { useEffect, useState } from "react";
import { focusManager, useQuery, useQueryClient } from "@tanstack/react-query";
import { snoozedInboxOptions } from "./queries";
import { onInboxInvalidate, onInboxSummaryInvalidate } from "./ws-updaters";

// setTimeout overflows past 2^31 - 1 ms (~24.8 days); a longer wait re-arms.
const MAX_TIMER_MS = 24 * 60 * 60 * 1000;
// The server compares against its own clock; give it a moment past the time.
const WAKE_SLACK_MS = 1500;

/**
 * Brings a snoozed issue back when its time comes (MUL-9333). Nothing on the
 * server announces it, so this refreshes the inbox lists and the unread
 * summary at the earliest snooze time, and again when the page is shown after
 * that time passed unseen (a sleeping laptop holds timers back).
 *
 * Mount once per workspace, where it stays mounted with the badge.
 */
export function useInboxSnoozeWake(wsId: string | null | undefined) {
  const qc = useQueryClient();
  const { data: snoozed } = useQuery({ ...snoozedInboxOptions(wsId ?? ""), enabled: !!wsId });
  const [tick, setTick] = useState(0);

  const nextWake = snoozed?.reduce<number | null>((min, entry) => {
    const at = new Date(entry.snoozed_until).getTime();
    if (Number.isNaN(at)) return min;
    return min === null || at < min ? at : min;
  }, null) ?? null;

  useEffect(() => {
    if (!wsId || nextWake === null) return;
    const wake = () => {
      void onInboxInvalidate(qc, wsId);
      void onInboxSummaryInvalidate(qc);
    };
    const delay = nextWake - Date.now() + WAKE_SLACK_MS;
    if (delay <= 0) {
      wake();
      return;
    }
    const timer = setTimeout(delay > MAX_TIMER_MS ? () => setTick((n) => n + 1) : wake, Math.min(delay, MAX_TIMER_MS));
    const unsubscribe = focusManager.subscribe((focused) => {
      if (focused && Date.now() >= nextWake) wake();
    });
    return () => {
      clearTimeout(timer);
      unsubscribe();
    };
  }, [qc, wsId, nextWake, tick]);
}
