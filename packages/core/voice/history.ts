/**
 * Dictation history: recordings kept on this device with their transcripts,
 * so one that failed or was interrupted can be transcribed again.
 */

export type VoiceHistoryStatus = "transcribing" | "done" | "failed" | "interrupted";

export interface VoiceHistoryEntry {
  id: string;
  /** Epoch milliseconds. */
  createdAt: number;
  durationMs: number;
  status: VoiceHistoryStatus;
  /** The transcript as inserted; empty until one succeeds. */
  text: string;
}

export const VOICE_HISTORY_RETENTIONS = ["off", "1d", "7d", "30d", "forever"] as const;
export type VoiceHistoryRetention = (typeof VOICE_HISTORY_RETENTIONS)[number];

/** Days to keep entries for; 0 keeps none, null keeps them all. */
export function voiceHistoryDays(retention: VoiceHistoryRetention): number | null {
  switch (retention) {
    case "off":
      return 0;
    case "1d":
      return 1;
    case "7d":
      return 7;
    case "30d":
      return 30;
    case "forever":
      return null;
  }
}
