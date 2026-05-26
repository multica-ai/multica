import React from "react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, act, screen } from "@testing-library/react";
import { PomodoroTimer } from "./PomodoroTimer";

// ── Sonner mock: capture toast action so the test can trigger the "toast Skip" ──
let capturedToastAction: (() => void) | undefined;

vi.mock("sonner", () => ({
  toast: Object.assign(
    (_msg: string, opts?: { action?: { onClick?: () => void } }) => {
      capturedToastAction = opts?.action?.onClick;
    },
    { info: vi.fn(), error: vi.fn(), success: vi.fn() },
  ),
}));

// ── completeMutation spy ──────────────────────────────────────────────────────
const mockCompleteMutate = vi.fn();

// Stable session object — a new object reference on each render would cause
// useEffect([session]) to reset completingRef on every render cycle, which
// breaks the countdown guard being tested here.
const FAKE_NOW = "2024-06-01T10:00:00.000Z";
const SESSION = {
  id: "session-1",
  status: "running",
  phase: "work",
  // 2-second phase so fake-timer advance is cheap.
  phase_duration_seconds: 2,
  elapsed_seconds: 0,
  // started_at matches vi.setSystemTime(FAKE_NOW) so runningFor starts at 0.
  started_at: FAKE_NOW,
  pomodoro_count: 1,
};

vi.mock("../hooks/use-pomodoro", () => ({
  usePomodoroQuery: () => ({ data: SESSION, isLoading: false }),
  useStartPomodoroMutation: () => ({ mutate: vi.fn(), isPending: false }),
  usePausePomodoroMutation: () => ({ mutate: vi.fn(), isPending: false }),
  useCompletePomodoroMutation: () => ({
    mutate: mockCompleteMutate,
    isPending: false,
  }),
  useResetPomodoroMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));

vi.mock("../hooks/use-pomodoro-settings", () => ({
  usePomodoroSettings: () => ({
    settings: {
      work_minutes: 25,
      short_break_minutes: 5,
      long_break_minutes: 15,
      long_break_after: 4,
      auto_start_break: false,
      auto_start_work: false,
      white_noise: "none",
      sound_enabled: false,
    },
  }),
}));

vi.mock("../hooks/use-sound-system", () => ({
  useSoundSystem: () => ({
    playWorkComplete: vi.fn(),
    playBreakComplete: vi.fn(),
    playStartTick: vi.fn(),
    startWhiteNoise: vi.fn(),
    stopWhiteNoise: vi.fn(),
  }),
}));

vi.mock("../hooks/use-time-tracking", () => ({
  useTimeEntryLabelsQuery: () => ({ data: [] }),
  useTimeEntryLabelMutations: () => ({ createTimeEntryLabel: vi.fn() }),
}));

// ── Tests ─────────────────────────────────────────────────────────────────────
describe("PomodoroTimer – double-completion guard (Bug 1)", () => {
  beforeEach(() => {
    // Pin Date.now() to FAKE_NOW so calcRemaining returns exactly 2 s.
    vi.useFakeTimers();
    vi.setSystemTime(new Date(FAKE_NOW));
    capturedToastAction = undefined;
    mockCompleteMutate.mockClear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("fires completeMutation exactly once even if both inline Skip and toast Skip are triggered", async () => {
    render(<PomodoroTimer />);

    // Step through the countdown one second at a time so React commits each
    // setRemaining state update before the next tick reads `prev`.
    // t=1s: remaining 2 → 1
    await act(async () => { vi.advanceTimersByTime(1000); });
    // t=2s: remaining 1 → 0 → completingRef=true, setTimeout(0) scheduled
    await act(async () => { vi.advanceTimersByTime(1000); });
    // Flush the scheduled setTimeout(0) which calls setCompletionFlow + toast.
    await act(async () => { vi.advanceTimersByTime(0); });

    // Inline "Skip" button should now be visible because completionFlow was set.
    const skipButton = screen.getByRole("button", { name: /skip/i });

    // Click inline Skip — this calls fireComplete once.
    await act(async () => {
      skipButton.click();
    });

    // The toast action (a second independent path) should be blocked by the guard.
    await act(async () => {
      capturedToastAction?.();
    });

    // Only one mutation call, regardless of which path fired first.
    expect(mockCompleteMutate).toHaveBeenCalledTimes(1);
  });

  it("Escape inside note input backs out to completion-flow actions, not abandon the flow", async () => {
    const { getByPlaceholderText, getByRole, queryByPlaceholderText } = render(<PomodoroTimer />);

    // Advance time to completion so completionFlow is set.
    await act(async () => { vi.advanceTimersByTime(1000); });
    await act(async () => { vi.advanceTimersByTime(1000); });
    await act(async () => { vi.advanceTimersByTime(0); });

    // Click "Add Note" to enter note-input mode.
    const addNoteButton = getByRole("button", { name: /add note/i });
    await act(async () => { addNoteButton.click(); });

    // Note input should be visible.
    const noteInput = getByPlaceholderText("Add a note…");
    expect(noteInput).toBeInTheDocument();

    // Press Escape — should back out of note-input mode.
    await act(async () => {
      noteInput.dispatchEvent(
        new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
      );
    });

    // Note input should be gone — backed out of note-input substate.
    expect(queryByPlaceholderText("Add a note…")).not.toBeInTheDocument();

    // Completion-flow action buttons (Skip) must still be visible —
    // the entire flow must NOT have been abandoned.
    expect(getByRole("button", { name: /skip/i })).toBeInTheDocument();
  });
});
