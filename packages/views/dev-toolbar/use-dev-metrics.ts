"use client";

import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  getDevRecording,
  installDevRecorder,
  recordDevNavigationDuration,
  recordDevRoute,
  recordDevSyncStatus,
  subscribeDevRecording,
  type DevRecording,
} from "@multica/core/diagnostics";
import { blockedRatio, JANK_WINDOW_MS, navDuration } from "./metrics";

/** Start the recorder and feed it what only the platform knows: route and sync. */
export function useDevRecorder(route: string, sync: string): DevRecording {
  const queryClient = useQueryClient();
  useEffect(() => installDevRecorder(queryClient), [queryClient]);
  useEffect(() => recordDevRoute(route), [route]);
  useEffect(() => recordDevSyncStatus(sync), [sync]);
  return useSyncExternalStore(subscribeDevRecording, getDevRecording, getDevRecording);
}

/** Wall clock, re-read every `intervalMs` so trailing windows keep moving. */
export function useNow(intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(timer);
  }, [intervalMs]);
  return now;
}

/** Share of the last 10s spent in long tasks; null where `longtask` is unsupported. */
export function useMainThreadBlocked(recording: DevRecording, now: number): number | null {
  const [supported] = useState(
    () => typeof PerformanceObserver !== "undefined" && Boolean(PerformanceObserver.supportedEntryTypes?.includes("longtask")),
  );
  if (!supported) return null;
  const tasks = recording.longTasks
    .filter((task) => task.at + task.durationMs > now - JANK_WINDOW_MS)
    .map((task) => ({ start: task.at, end: task.at + task.durationMs }));
  return blockedRatio(tasks, now);
}

/**
 * How long the last route change took, from the pointer or key press that
 * caused it to the first frame after the new route rendered.
 */
export function useNavDuration(route: string): number | null {
  const [duration, setDuration] = useState<number | null>(null);
  const lastInputAt = useRef<number | null>(null);
  const previousRoute = useRef(route);

  useEffect(() => {
    const record = (event: Event) => {
      lastInputAt.current = event.timeStamp;
    };
    window.addEventListener("pointerdown", record, true);
    window.addEventListener("keydown", record, true);
    return () => {
      window.removeEventListener("pointerdown", record, true);
      window.removeEventListener("keydown", record, true);
    };
  }, []);

  useEffect(() => {
    if (previousRoute.current === route) return;
    previousRoute.current = route;
    const inputAt = lastInputAt.current;
    lastInputAt.current = null;
    const frame = requestAnimationFrame(() => {
      const elapsed = navDuration(inputAt, performance.now());
      if (elapsed === null) return;
      setDuration(elapsed);
      recordDevNavigationDuration(route, elapsed);
    });
    return () => cancelAnimationFrame(frame);
  }, [route]);

  return duration;
}
