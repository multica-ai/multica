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
