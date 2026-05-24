import { describe, it, expect } from "vitest";
import { computeStreak, localDateKey } from "./pomodoro-dashboard";
import type { TimeEntry } from "@/shared/types";

// Minimal TimeEntry factory — only fields consumed by computeStreak matter.
function makeEntry(startTime: string): TimeEntry {
  return {
    id: startTime,
    workspace_id: "ws-1",
    user_id: "user-1",
    issue_id: null,
    description: null,
    start_time: startTime,
    stop_time: null,
    duration_seconds: 1500,
    type: "pomodoro",
    labels: [],
    created_at: startTime,
    updated_at: startTime,
  };
}

/** Build an ISO string for N days ago at noon local time. */
function daysAgo(n: number): string {
  const d = new Date();
  d.setDate(d.getDate() - n);
  d.setHours(12, 0, 0, 0);
  return d.toISOString();
}

describe("computeStreak", () => {
  it("returns 0 for an empty entry list", () => {
    expect(computeStreak([])).toBe(0);
  });

  it("returns 1 when only today has an entry", () => {
    expect(computeStreak([makeEntry(daysAgo(0))])).toBe(1);
  });

  it("counts consecutive days ending today", () => {
    const entries = [daysAgo(0), daysAgo(1), daysAgo(2)].map(makeEntry);
    expect(computeStreak(entries)).toBe(3);
  });

  it("stops counting at a gap in days", () => {
    // yesterday and 3 days ago — gap on 2 days ago breaks the streak.
    const entries = [daysAgo(1), daysAgo(3)].map(makeEntry);
    expect(computeStreak(entries)).toBe(1);
  });

  it("counts multiple entries on the same day as one day in the streak", () => {
    const entries = [daysAgo(0), daysAgo(0), daysAgo(1)].map(makeEntry);
    expect(computeStreak(entries)).toBe(2);
  });

  // ── Bug regression: streak must NOT drop to 0 at midnight ─────────────────
  it("preserves yesterday's streak at midnight when today has no entries yet", () => {
    // Simulate: only yesterday and the day before have entries (today = nothing yet).
    // Expected streak: 2 (yesterday counts, day-before-yesterday counts).
    const entries = [daysAgo(1), daysAgo(2)].map(makeEntry);
    expect(computeStreak(entries)).toBe(2);
  });

  it("returns 1 when only yesterday has an entry and today has none", () => {
    expect(computeStreak([makeEntry(daysAgo(1))])).toBe(1);
  });

  it("still returns 0 when even yesterday has no entry", () => {
    // Only 3 days ago — not consecutive with yesterday/today.
    expect(computeStreak([makeEntry(daysAgo(3))])).toBe(0);
  });

  it("shows today's streak when today has entries, ignoring the midnight edge", () => {
    // Today + yesterday: streak should be 2.
    const entries = [daysAgo(0), daysAgo(1)].map(makeEntry);
    expect(computeStreak(entries)).toBe(2);
  });
});

describe("localDateKey", () => {
  it("produces YYYY-MM-DD format", () => {
    expect(localDateKey("2024-03-15T10:30:00.000Z")).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  });
});
