// Desktop's on-device speech engine, through the preload `desktopAPI.voice`.
// Undefined on web and on Desktop builds that predate it.
import type { VoiceHistoryEntry, VoiceHistoryStatus, VoiceModels, VoiceTranscription } from "@multica/core/voice";

export interface LocalVoiceRequest {
  modelId: string;
  language: string;
  hotwords: string[];
}

export interface LocalVoiceHistoryAPI {
  /** Newest first. */
  list: () => Promise<VoiceHistoryEntry[]>;
  onChanged: (callback: () => void) => () => void;
  save: (request: {
    audio: Uint8Array;
    mimeType: string;
    durationMs: number;
    keepDays: number | null;
  }) => Promise<VoiceHistoryEntry | null>;
  update: (id: string, patch: { status: VoiceHistoryStatus; text?: string }) => Promise<void>;
  audio: (id: string) => Promise<{ audio: Uint8Array<ArrayBuffer>; mimeType: string } | null>;
  remove: (id: string) => Promise<void>;
  /** Remember how many days to keep (0 keeps none, null forever) and drop older entries now. */
  setRetention: (keepDays: number | null) => Promise<void>;
  /** Delete every entry, including one still being transcribed. */
  clear: () => Promise<void>;
}

export interface LocalVoiceAPI {
  listModels: () => Promise<VoiceModels>;
  onModelsChanged: (callback: (snapshot: VoiceModels) => void) => () => void;
  downloadModel: (id: string) => Promise<void>;
  cancelDownload: (id: string) => Promise<void>;
  deleteModel: (id: string) => Promise<void>;
  prepare: (request: LocalVoiceRequest) => void;
  /** `samples` is mono PCM at 16 kHz. */
  transcribe: (request: LocalVoiceRequest & { samples: Float32Array }) => Promise<VoiceTranscription>;
  /** Absent on Desktop builds that predate history. */
  history?: LocalVoiceHistoryAPI;
}

export function localVoiceAPI(): LocalVoiceAPI | undefined {
  if (typeof window === "undefined") return undefined;
  const api = (window as unknown as { desktopAPI?: { voice?: Partial<LocalVoiceAPI> } }).desktopAPI?.voice;
  return api?.listModels && api.onModelsChanged && api.transcribe ? (api as LocalVoiceAPI) : undefined;
}
