import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import type { TimeEntry } from "@/shared/types";
import { formatDuration, getElapsedSeconds, LiveDuration } from "./LiveDuration";

// ── formatDuration ────────────────────────────────────────────────────────────

describe("formatDuration", () => {
  it("formats seconds-only durations as m:ss", () => {
    expect(formatDuration(0)).toBe("0:00");
    expect(formatDuration(5)).toBe("0:05");
    expect(formatDuration(59)).toBe("0:59");
  });

  it("formats minute durations as m:ss", () => {
    expect(formatDuration(60)).toBe("1:00");
    expect(formatDuration(90)).toBe("1:30");
    expect(formatDuration(3599)).toBe("59:59");
  });

  it("formats hour durations as h:mm:ss", () => {
    expect(formatDuration(3600)).toBe("1:00:00");
    expect(formatDuration(3661)).toBe("1:01:01");
    expect(formatDuration(7322)).toBe("2:02:02");
  });

  it("does not add leading zeros to hours or minutes", () => {
    expect(formatDuration(3600)).toBe("1:00:00");
    expect(formatDuration(36000)).toBe("10:00:00");
  });
});

// ── getElapsedSeconds ─────────────────────────────────────────────────────────

describe("getElapsedSeconds", () => {
  beforeEach(() => {
    // Fix Date.now to 1_000_000_000_000 ms (unix second = 1_000_000_000).
    vi.spyOn(Date, "now").mockReturnValue(1_000_000_000_000);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("returns stored duration_seconds for stopped entries", () => {
    const entry = { duration_seconds: 3661 } as TimeEntry;
    expect(getElapsedSeconds(entry)).toBe(3661);
  });

  it("computes elapsed from Toggl convention for running entries", () => {
    // duration_seconds = -start_time.Unix() where start was 100 seconds ago.
    const startUnix = 1_000_000_000 - 100;
    const entry = { duration_seconds: -startUnix } as TimeEntry;
    // elapsed = floor(Date.now()/1000) + (-startUnix) = 1_000_000_000 - startUnix = 100
    expect(getElapsedSeconds(entry)).toBe(100);
  });

  it("floors elapsed to 0 if clock skew would produce negative value", () => {
    // Simulates a future start_time (should not happen but must not crash).
    const futureUnix = 1_000_000_000 + 500;
    const entry = { duration_seconds: -futureUnix } as TimeEntry;
    expect(getElapsedSeconds(entry)).toBe(0);
  });
});

// ── LiveDuration ──────────────────────────────────────────────────────────────

describe("LiveDuration", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.spyOn(Date, "now").mockReturnValue(1_000_000_000_000);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  function makeEntry(overrides: Partial<TimeEntry>): TimeEntry {
    return {
      id: "test-id",
      workspace_id: "ws",
      user_id: "user",
      issue_id: null,
      description: null,
      start_time: new Date().toISOString(),
      stop_time: null,
      duration_seconds: 0,
      type: "manual",
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
      ...overrides,
    };
  }

  it("renders the stored duration for a stopped entry", () => {
    const entry = makeEntry({ duration_seconds: 3661 });
    render(<LiveDuration entry={entry} />);
    expect(screen.getByText("1:01:01")).toBeInTheDocument();
  });

  it("renders 0:00 for a zero-duration stopped entry", () => {
    const entry = makeEntry({ duration_seconds: 0 });
    render(<LiveDuration entry={entry} />);
    expect(screen.getByText("0:00")).toBeInTheDocument();
  });

  it("renders elapsed time for a running entry (Toggl convention)", () => {
    // 60 seconds ago.
    const startUnix = 1_000_000_000 - 60;
    const entry = makeEntry({ duration_seconds: -startUnix });
    render(<LiveDuration entry={entry} />);
    expect(screen.getByText("1:00")).toBeInTheDocument();
  });

  it("ticks every second for a running entry", () => {
    const startUnix = 1_000_000_000 - 60;
    const entry = makeEntry({ duration_seconds: -startUnix });
    render(<LiveDuration entry={entry} />);

    expect(screen.getByText("1:00")).toBeInTheDocument();

    // Advance 5 seconds.
    vi.spyOn(Date, "now").mockReturnValue(1_000_000_000_000 + 5_000);
    act(() => { vi.advanceTimersByTime(5_000); });
    expect(screen.getByText("1:05")).toBeInTheDocument();
  });

  it("does not tick for a stopped entry", () => {
    const entry = makeEntry({ duration_seconds: 120 });
    render(<LiveDuration entry={entry} />);

    expect(screen.getByText("2:00")).toBeInTheDocument();

    // Even after time advances, the display must not change.
    vi.spyOn(Date, "now").mockReturnValue(1_000_000_000_000 + 10_000);
    act(() => { vi.advanceTimersByTime(10_000); });
    expect(screen.getByText("2:00")).toBeInTheDocument();
  });
});
