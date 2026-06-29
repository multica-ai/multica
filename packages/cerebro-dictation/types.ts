/**
 * Public type contract for cerebro-dictation. Kept separate from the hook
 * implementation so consumers (slice 2 UI, slice 3 API client) can depend
 * on the types without pulling React in.
 */

export type DictationStatus =
  | "idle"
  | "requesting-permission"
  | "recording"
  | "transcribing"
  | "error";

export type DictationErrorKind =
  | "unsupported"
  | "permission-denied"
  | "no-microphone"
  | "recording-failed"
  | "transcription-failed"
  // The speech engine is still cold-booting (FIR-2048). Distinct from
  // transcription-failed so the UI can show a calm "warming up" cue instead of a
  // hard error — the recording is kept and can be re-sent in a moment.
  | "warming-up"
  | "streaming-failed"
  | "aborted";

export interface DictationError {
  kind: DictationErrorKind;
  message: string;
  cause?: unknown;
}

/**
 * Slice 3 implements this against the real /api/cerebro/dictation/transcribe
 * endpoint. Until then, callers inject a stub or mock.
 *
 * The signal is wired to `cancel()` on the hook — implementations should
 * cooperate by passing it to `fetch` so an in-flight upload aborts cleanly.
 */
export type Transcriber = (
  audio: Blob,
  signal: AbortSignal,
) => Promise<string>;

export interface DictationPartialEvent {
  type: "partial";
  text: string;
  stable_until?: number;
}

export interface DictationFinalEvent {
  type: "final";
  text: string;
  started_at?: number;
  ended_at?: number;
}

export interface DictationBackendErrorEvent {
  type: "error";
  code: string;
  message: string;
}

export type DictationStreamEvent =
  | DictationPartialEvent
  | DictationFinalEvent
  | DictationBackendErrorEvent;

export interface StreamingTranscriberSession {
  sendAudio: (audio: Blob) => void;
  endUtterance: () => void;
  cancel: () => void;
  finished: Promise<void>;
}

export interface StreamingTranscriberOptions {
  mimeType?: string;
  signal: AbortSignal;
  onPartial?: (text: string, event: DictationPartialEvent) => void;
  onFinal?: (text: string, event: DictationFinalEvent) => void;
  onError?: (error: Error, event?: DictationBackendErrorEvent) => void;
}

export type StreamingTranscriber = (
  options: StreamingTranscriberOptions,
) => StreamingTranscriberSession;

export interface UseDictationOptions {
  transcribe?: Transcriber;
  streamTranscribe?: StreamingTranscriber;
  /**
   * Audio MIME type passed to MediaRecorder. When omitted, the hook picks
   * the first entry of {@link DEFAULT_MIME_TYPE_PREFERENCE} that the
   * runtime reports as supported. Override only when the backend needs a
   * specific codec.
   */
  mimeType?: string;
  /** Auto-stop after this many ms of recording. Default: 60_000 (60s). */
  maxDurationMs?: number;
  /** Called once a transcription resolves successfully. */
  onTranscribed?: (text: string) => void;
  /** Called as streaming dictation emits partial text. */
  onPartial?: (text: string) => void;
  /** Called whenever the hook captures a non-recoverable error. */
  onError?: (error: DictationError) => void;
}

export interface UseDictationReturn {
  status: DictationStatus;
  error: DictationError | null;
  /** Last successful transcription. Cleared at the start of a new recording. */
  lastTranscript: string | null;
  /** Whether the runtime supports MediaRecorder + getUserMedia at all. */
  isSupported: boolean;
  /**
   * The live microphone stream while recording, exposed so the UI can render
   * a real-time waveform from it. `null` whenever we are not recording.
   */
  mediaStream: MediaStream | null;
  /** Begin recording. No-op when already recording / transcribing. */
  start: () => Promise<void>;
  /** Stop recording and run the transcriber on what was captured. */
  stop: () => Promise<void>;
  /**
   * Abort the current recording or in-flight transcription without
   * producing a result. Safe to call from any state.
   */
  cancel: () => void;
  /**
   * True when the last transcription failed but the recorded clip was kept, so
   * the user can retry without speaking again (FIR-2048). Set on a warming-up
   * cold start or a transient transcription failure; cleared on success, on a
   * new recording, and on cancel.
   */
  canResend: boolean;
  /**
   * Re-run transcription on the kept recording (see {@link canResend}). No-op
   * when there is no kept clip or a recording/transcription is already in
   * flight. Used by the "Re-send" button the UI shows after a failure.
   */
  resend: () => Promise<void>;
  /**
   * Progress (0..1) of the engine cold-boot while we auto-retry the kept clip,
   * or `null` when no warm-up retry cycle is running (FIR-2048). It is a
   * wall-clock estimate against a typical ~30s boot — held just below 1 until
   * the transcript actually lands. The UI shows it as a climbing "warming up"
   * cue so a cold start reads as working, not stuck, and the transcript runs
   * itself the moment the engine is ready (no manual Re-send needed).
   */
  warmupProgress: number | null;
}
