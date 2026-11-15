/**
 * On-device voice input: main downloads speech models into user data and
 * transcribes in a utility process, so audio never leaves the machine. The
 * renderer records, decodes to 16 kHz mono samples and asks for text.
 */

export const VOICE_MODELS_LIST_CHANNEL = "voice:models:list";
export const VOICE_MODELS_CHANGED_CHANNEL = "voice:models:changed";
export const VOICE_MODEL_DOWNLOAD_CHANNEL = "voice:models:download";
export const VOICE_MODEL_CANCEL_CHANNEL = "voice:models:cancel";
export const VOICE_MODEL_DELETE_CHANNEL = "voice:models:delete";
export const VOICE_PREPARE_CHANNEL = "voice:prepare";
export const VOICE_TRANSCRIBE_CHANNEL = "voice:transcribe";
export const VOICE_HISTORY_LIST_CHANNEL = "voice:history:list";
export const VOICE_HISTORY_CHANGED_CHANNEL = "voice:history:changed";
export const VOICE_HISTORY_SAVE_CHANNEL = "voice:history:save";
export const VOICE_HISTORY_UPDATE_CHANNEL = "voice:history:update";
export const VOICE_HISTORY_AUDIO_CHANNEL = "voice:history:audio";
export const VOICE_HISTORY_REMOVE_CHANNEL = "voice:history:remove";
export const VOICE_HISTORY_RETENTION_CHANNEL = "voice:history:retention";
export const VOICE_HISTORY_CLEAR_CHANNEL = "voice:history:clear";

/** The sample rate every model expects. */
export const VOICE_SAMPLE_RATE = 16000;
/** Matches the renderer's two-minute recording cap, with headroom. */
export const MAX_VOICE_SAMPLES = VOICE_SAMPLE_RATE * 150;

export type VoiceModelStatus = "absent" | "downloading" | "ready";

export interface VoiceModelInfo {
  id: string;
  name: string;
  /** Who trained the model, for attribution. */
  author: string;
  licenseUrl: string;
  /** BCP 47 codes the model transcribes. */
  languages: string[];
  /** The model takes a spoken-language hint; others always detect. */
  languageHint: boolean;
  /** What the model is best at, shown as a badge. */
  highlight?: "accurate" | "fast";
  sizeBytes: number;
  status: VoiceModelStatus;
  /** Bytes on disk so far while downloading. */
  receivedBytes: number;
  /** Why the last download failed. */
  error?: string;
}

export interface VoiceModelsSnapshot {
  /** False when this build has no speech engine for this platform. */
  engineAvailable: boolean;
  models: VoiceModelInfo[];
}

export interface VoiceModelRequest {
  modelId: string;
  /** A VOICE_LANGUAGES value; ignored by models without a language hint. */
  language: string;
  /** Names and terms to favour; ignored by models without hotword support. */
  hotwords?: string[];
}

export interface VoiceTranscribeRequest extends VoiceModelRequest {
  /** Mono PCM at VOICE_SAMPLE_RATE, in [-1, 1]. */
  samples: Float32Array;
}

export type VoiceTranscribeResult =
  | { ok: true; text: string }
  | { ok: false; reason: "model-missing" | "engine-unavailable" | "failed"; error?: string };

/**
 * Dictation history: every recording is kept on this device with its text,
 * so a failed or interrupted transcription can be run again from the audio.
 * `transcribing` left over from a previous run reads as `interrupted`.
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

export interface VoiceHistorySaveRequest {
  /** The recording as captured (compressed, e.g. webm/opus). */
  audio: Uint8Array;
  mimeType: string;
  durationMs: number;
  /** Prune entries older than this many days; null keeps everything. */
  keepDays: number | null;
}

export interface VoiceHistoryUpdate {
  status: VoiceHistoryStatus;
  text?: string;
}

export interface VoiceHistoryAudio {
  audio: Uint8Array;
  mimeType: string;
}
