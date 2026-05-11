import { useCallback, useState } from "react";

export interface PomodoroSettings {
  work_minutes: number;
  short_break_minutes: number;
  long_break_minutes: number;
  /** Number of pomodoros before a long break. */
  long_break_after: number;
  auto_start_break: boolean;
  auto_start_work: boolean;
  sound_enabled: boolean;
  /** Volume level between 0.0 and 1.0. */
  volume: number;
  white_noise: "none" | "brown" | "rain" | "cafe" | "pink";
}

const DEFAULT_SETTINGS: PomodoroSettings = {
  work_minutes: 25,
  short_break_minutes: 5,
  long_break_minutes: 15,
  long_break_after: 4,
  auto_start_break: false,
  auto_start_work: false,
  sound_enabled: true,
  volume: 0.8,
  white_noise: "none",
};

const STORAGE_KEY = "pomodoro-settings";

function loadSettings(): PomodoroSettings {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return DEFAULT_SETTINGS;
    return { ...DEFAULT_SETTINGS, ...JSON.parse(raw) };
  } catch {
    return DEFAULT_SETTINGS;
  }
}

export function usePomodoroSettings() {
  const [settings, setSettings] = useState<PomodoroSettings>(loadSettings);

  const updateSettings = useCallback((partial: Partial<PomodoroSettings>) => {
    setSettings((prev) => {
      const next = { ...prev, ...partial };
      try {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
      } catch {
        // ignore storage errors
      }
      return next;
    });
  }, []);

  return { settings, updateSettings };
}
