// Desktop's on-device speech engine, through the preload `desktopAPI.voice`.
// Undefined on web and on Desktop builds that predate it.
import type { VoiceModels, VoiceTranscription } from "@multica/core/voice";

export interface LocalVoiceRequest {
  modelId: string;
  language: string;
  hotwords: string[];
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
}

export function localVoiceAPI(): LocalVoiceAPI | undefined {
  if (typeof window === "undefined") return undefined;
  const api = (window as unknown as { desktopAPI?: { voice?: Partial<LocalVoiceAPI> } }).desktopAPI?.voice;
  return api?.listModels && api.onModelsChanged && api.transcribe ? (api as LocalVoiceAPI) : undefined;
}
