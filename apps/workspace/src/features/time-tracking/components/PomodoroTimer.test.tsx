import React from "react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, act, screen } from "@testing-library/react";
import { PomodoroTimer } from "./PomodoroTimer";
import type { PomodoroSettings } from "../hooks/use-pomodoro-settings";

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
const mockPomodoroSettings = vi.hoisted(() => {
  const settings: PomodoroSettings = {
    work_minutes: 25,
    short_break_minutes: 5,
    long_break_minutes: 15,
    long_break_after: 4,
    auto_start_break: false,
    auto_start_work: false,
    white_noise: "none" as const,
    sound_enabled: false,
    tick_enabled: false,
    volume: 0.8,
  };
  return { settings };
});
const mockSoundSystem = vi.hoisted(() => ({
  playWorkComplete: vi.fn(),
  playBreakComplete: vi.fn(),
  playStartTick: vi.fn(),
  playTick: vi.fn(),
  startWhiteNoise: vi.fn(),
  stopWhiteNoise: vi.fn(),
  updateWhiteNoiseVolume: vi.fn(),
}));

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
  usePomodoroSettings: () => mockPomodoroSettings,
}));

vi.mock("../hooks/use-sound-system", () => ({
  useSoundSystem: () => mockSoundSystem,
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
    mockPomodoroSettings.settings = {
      work_minutes: 25,
      short_break_minutes: 5,
      long_break_minutes: 15,
      long_break_after: 4,
      auto_start_break: false,
      auto_start_work: false,
      white_noise: "none",
      sound_enabled: false,
      tick_enabled: false,
      volume: 0.8,
    };
    mockSoundSystem.playWorkComplete.mockClear();
    mockSoundSystem.playBreakComplete.mockClear();
    mockSoundSystem.playStartTick.mockClear();
    mockSoundSystem.playTick.mockClear();
    mockSoundSystem.startWhiteNoise.mockClear();
    mockSoundSystem.stopWhiteNoise.mockClear();
    mockSoundSystem.updateWhiteNoiseVolume.mockClear();
  });

  it("starts ambient sound when pomodoro is running and the setting is enabled", () => {
    mockPomodoroSettings.settings = {
      work_minutes: 25,
      short_break_minutes: 5,
      long_break_minutes: 15,
      long_break_after: 4,
      auto_start_break: false,
      auto_start_work: false,
      white_noise: "rain",
      sound_enabled: true,
      tick_enabled: false,
      volume: 0.8,
    };

    render(<PomodoroTimer />);

    expect(mockSoundSystem.startWhiteNoise).toHaveBeenCalledWith("rain");
    expect(mockSoundSystem.updateWhiteNoiseVolume).toHaveBeenCalledWith(0.8);
  });

  it("stops ambient sound when sound is disabled during a running pomodoro", () => {
    mockPomodoroSettings.settings = {
      work_minutes: 25,
      short_break_minutes: 5,
      long_break_minutes: 15,
      long_break_after: 4,
      auto_start_break: false,
      auto_start_work: false,
      white_noise: "rain",
      sound_enabled: true,
      tick_enabled: false,
      volume: 0.8,
    };

    const { rerender } = render(<PomodoroTimer />);

    expect(mockSoundSystem.startWhiteNoise).toHaveBeenCalledWith("rain");

    mockPomodoroSettings.settings = {
      ...mockPomodoroSettings.settings,
      sound_enabled: false,
    };

    rerender(<PomodoroTimer />);

    expect(mockSoundSystem.stopWhiteNoise).toHaveBeenCalled();
  });

  it("plays a tick sound on each second while running when enabled", async () => {
    mockPomodoroSettings.settings = {
      work_minutes: 25,
      short_break_minutes: 5,
      long_break_minutes: 15,
      long_break_after: 4,
      auto_start_break: false,
      auto_start_work: false,
      white_noise: "none",
      sound_enabled: true,
      tick_enabled: true,
      volume: 0.8,
    };

    render(<PomodoroTimer />);

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    expect(mockSoundSystem.playTick).toHaveBeenCalledTimes(1);
  });

  it("does not play a tick sound when ticking is disabled", async () => {
    mockPomodoroSettings.settings = {
      work_minutes: 25,
      short_break_minutes: 5,
      long_break_minutes: 15,
      long_break_after: 4,
      auto_start_break: false,
      auto_start_work: false,
      white_noise: "none",
      sound_enabled: true,
      tick_enabled: false,
      volume: 0.8,
    };

    render(<PomodoroTimer />);

    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    expect(mockSoundSystem.playTick).not.toHaveBeenCalled();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("fires completeMutation exactly once even if both inline Skip and toast Skip are triggered", async () => {
    render(<PomodoroTimer />);

    // Advance the countdown one second at a time so each state update lands before the next tick.
    // t=1s: remaining 2 -> 1
    await act(async () => { vi.advanceTimersByTime(1000); });
    // t=2s: remaining 1 -> 0, then the completion branch schedules setTimeout(0).
    await act(async () => { vi.advanceTimersByTime(1000); });
    // Flush the scheduled setTimeout(0) so the completion flow becomes observable.
    await act(async () => { vi.advanceTimersByTime(0); });

    // The inline Skip button should now be visible.
    const skipButton = screen.getByRole("button", { name: /skip/i });

    // Click inline Skip to trigger one completion flow.
    await act(async () => {
      skipButton.click();
    });

    // The second independent toast path should be blocked by the guard.
    await act(async () => {
      capturedToastAction?.();
    });

    // Only one mutation call should happen regardless of which path fired first.
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
