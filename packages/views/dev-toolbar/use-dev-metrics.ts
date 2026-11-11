"use client";

import { useEffect, useRef, useState } from "react";
import { blockedRatio, JANK_WINDOW_MS, navDuration, type TimeSpan } from "./metrics";

const SAMPLE_INTERVAL_MS = 1_000;

interface ChromiumMemory {
  usedJSHeapSize: number;
}

function readHeapBytes(): number | null {
  const memory = (performance as Performance & { memory?: ChromiumMemory }).memory;
  return memory ? memory.usedJSHeapSize : null;
}

/** JS heap in use; null on engines without `performance.memory` (non-Chromium). */
export function useHeapBytes(): number | null {
  const [bytes, setBytes] = useState<number | null>(null);
  useEffect(() => {
    setBytes(readHeapBytes());
    const timer = setInterval(() => setBytes(readHeapBytes()), SAMPLE_INTERVAL_MS * 2);
    return () => clearInterval(timer);
  }, []);
  return bytes;
}

/** Share of the last 10s spent in long tasks; null where `longtask` is unsupported. */
export function useMainThreadBlocked(): number | null {
  const [ratio, setRatio] = useState<number | null>(null);
  useEffect(() => {
    if (!PerformanceObserver.supportedEntryTypes?.includes("longtask")) return;
    let tasks: TimeSpan[] = [];
    const observer = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        tasks.push({ start: entry.startTime, end: entry.startTime + entry.duration });
      }
    });
    observer.observe({ type: "longtask" });
    const sample = () => {
      const now = performance.now();
      tasks = tasks.filter((task) => task.end > now - JANK_WINDOW_MS);
      setRatio(blockedRatio(tasks, now));
    };
    sample();
    const timer = setInterval(sample, SAMPLE_INTERVAL_MS);
    return () => {
      observer.disconnect();
      clearInterval(timer);
    };
  }, []);
  return ratio;
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
      if (elapsed !== null) setDuration(elapsed);
    });
    return () => cancelAnimationFrame(frame);
  }, [route]);

  return duration;
}
