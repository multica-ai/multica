/** A host-owned dictation UI pastes into the focused editor. Audio, credentials
 * and transcript transport never cross this adapter. Success means the toggle
 * was dispatched, not that recording or transcription succeeded. */
export type DictationResult =
  | { ok: true; shortcut: string }
  | {
      ok: false;
      reason:
        | "not_configured"
        | "app_not_running"
        | "not_focused"
        | "busy"
        | "cleanup_failed"
        | "unavailable";
    };

export interface DictationAdapter {
  toggle: () => Promise<DictationResult>;
}
