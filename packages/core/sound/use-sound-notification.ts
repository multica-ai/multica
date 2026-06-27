"use client";

import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { soundManager } from "./sound-manager";
import { SOUND_PRIORITY, type SoundType } from "./sound-definitions";
import { notificationPreferenceKeys } from "../notification-preferences/queries";
import type { NotificationPreferences } from "../types/notification-preference";

/**
 * Dedup window in ms. Notifications arriving within this window are merged:
 * only the highest-priority sound plays.
 */
const DEDUP_WINDOW_MS = 500;

/**
 * Maps WS notification event types to the sound type that should play and
 * the preference key that gates it. Events not listed here are ignored.
 */
const EVENT_SOUND_MAP: Record<string, { sound: SoundType; prefKey: keyof NotificationPreferences }> = {
  "notification:issue_done": { sound: "complete", prefKey: "sound_issue_done" },
  "notification:parent_chain_done": { sound: "complete", prefKey: "sound_issue_done" },
  "notification:issue_blocked": { sound: "blocked", prefKey: "sound_blocked" },
  "notification:child_blocked": { sound: "blocked", prefKey: "sound_child_blocked" },
  "notification:in_review": { sound: "action_required", prefKey: "sound_in_review" },
  "notification:mention_decision": { sound: "action_required", prefKey: "sound_mention_decision" },
  "notification:task_failed": { sound: "attention", prefKey: "sound_task_failed" },
  "notification:stage_closed": { sound: "complete", prefKey: "sound_child_blocked" },
};

/**
 * Reads the raw notification preferences from React Query's cache for the
 * given workspace. Returns null when the cache is cold (no data loaded yet).
 */
function getCachedPreferences(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
): NotificationPreferences | null {
  const data = qc.getQueryData<{ preferences: NotificationPreferences }>(
    notificationPreferenceKeys.all(wsId),
  );
  return data?.preferences ?? null;
}

/**
 * React hook that wires SoundManager initialisation and provides a
 * `handleNotificationSound` callback for the realtime sync layer.
 *
 * Usage (in use-realtime-sync or similar):
 *   const { handleNotificationSound } = useSoundNotification(wsId);
 *   // then in WS event handler:
 *   handleNotificationSound("notification:issue_done");
 */
export function useSoundNotification(wsId: string) {
  const qc = useQueryClient();

  // Dedup state: last-fire timestamp + highest-priority sound in window
  const dedupRef = useRef<{ timer: ReturnType<typeof setTimeout> | null; pending: SoundType | null }>({
    timer: null,
    pending: null,
  });

  // Init SoundManager on mount
  useEffect(() => {
    soundManager.init();
  }, []);

  /**
   * Handle a notification event for sound playback. Applies:
   * 1. Preference check (global on/off + per-event toggle)
   * 2. 500ms dedup window (only highest-priority sound wins)
   * 3. Volume & theme from preferences
   *
   * This is the DEFENSIVE path — the server already checked preferences
   * before pushing. If local cache disagrees (user just changed settings),
   * the local preference wins and the sound is silently dropped.
   */
  const handleNotificationSound = (eventType: string) => {
    const mapping = EVENT_SOUND_MAP[eventType];
    if (!mapping) return;

    const prefs = getCachedPreferences(qc, wsId);
    if (!prefs) return; // cache cold, skip

    // Global sound switch
    if (prefs.sound_enabled === "muted") return;

    // Per-event toggle
    if (prefs[mapping.prefKey] === "muted") return;

    const volume = typeof prefs.sound_volume === "number" ? prefs.sound_volume : 70;
    const theme = (typeof prefs.sound_theme === "string" ? prefs.sound_theme : "default") as "default" | "soft" | "alert";

    const sound = mapping.sound;

    // Dedup: if a timer is already pending, keep the higher-priority sound
    if (dedupRef.current.pending !== null) {
      const currentPri = SOUND_PRIORITY[dedupRef.current.pending];
      const newPri = SOUND_PRIORITY[sound];
      if (newPri <= currentPri) return; // lower or equal priority, drop
      // Higher priority — replace pending
      dedupRef.current.pending = sound;
      return;
    }

    // No pending dedup — fire immediately and start window
    dedupRef.current.pending = sound;
    dedupRef.current.timer = setTimeout(() => {
      const finalSound = dedupRef.current.pending;
      dedupRef.current.pending = null;
      dedupRef.current.timer = null;
      if (finalSound) {
        soundManager.play(finalSound, volume, theme);
      }
    }, DEDUP_WINDOW_MS);
  };

  return { handleNotificationSound };
}
