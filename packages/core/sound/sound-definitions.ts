export type SoundType = "complete" | "blocked" | "attention" | "action_required";

/**
 * Each entry is a tuple of [frequency, startTime, duration].
 * Frequency in Hz, times in seconds relative to AudioContext.currentTime.
 */
export type Tone = [number, number, number];

/**
 * Sine-wave tone sequences for each notification sound type.
 *
 * - complete:       C5-E5-G5 ascending triad — pleasant completion chime
 * - blocked:        A4 three rapid short tones — urgent, needs attention
 * - attention:      E5 single medium tone — alert without alarm
 * - action_required: A4→E5 alternating — emphasis that action is needed
 */
export const SOUND_DEFINITIONS: Record<SoundType, Tone[]> = {
  complete: [
    [523, 0, 0.15],
    [659, 0.15, 0.15],
    [784, 0.3, 0.3],
  ],
  blocked: [
    [440, 0, 0.08],
    [440, 0.12, 0.08],
    [440, 0.24, 0.12],
  ],
  attention: [[660, 0, 0.2]],
  action_required: [
    [440, 0, 0.12],
    [660, 0.15, 0.12],
  ],
};

/**
 * Priority ordering for dedup: higher value = higher priority.
 * When multiple sounds fire within the 500ms dedup window, only the
 * highest-priority sound plays.
 */
export const SOUND_PRIORITY: Record<SoundType, number> = {
  action_required: 4,
  blocked: 3,
  attention: 2,
  complete: 1,
};

export type SoundTheme = "default" | "soft" | "alert";

/**
 * Volume multiplier per theme. Applied on top of the user's volume slider.
 */
export const THEME_VOLUME_MAP: Record<SoundTheme, number> = {
  default: 1.0,
  soft: 0.5,
  alert: 1.2,
};
