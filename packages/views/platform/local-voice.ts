// Desktop's on-device speech engine, through the preload `desktopAPI.voice`.
// Undefined on web and on Desktop builds that predate it.

export type LocalVoiceModelStatus = "absent" | "downloading" | "ready";

export interface LocalVoiceModel {
  id: string;
  name: string;
  author: string;
  licenseUrl: string;
  /** BCP 47 codes the model transcribes. */
  languages: string[];
  /** The model takes a spoken-language hint; others always detect. */
  languageHint: boolean;
  sizeBytes: number;
  status: LocalVoiceModelStatus;
  receivedBytes: number;
  error?: string;
}

export interface LocalVoiceModels {
  /** False when this build has no speech engine for this platform. */
  engineAvailable: boolean;
  models: LocalVoiceModel[];
}

export interface LocalVoiceRequest {
  modelId: string;
  language: string;
}

export type LocalVoiceResult =
  | { ok: true; text: string }
  | { ok: false; reason: "model-missing" | "engine-unavailable" | "failed"; error?: string };

export interface LocalVoiceAPI {
  listModels: () => Promise<LocalVoiceModels>;
  onModelsChanged: (callback: (snapshot: LocalVoiceModels) => void) => () => void;
  downloadModel: (id: string) => Promise<void>;
  cancelDownload: (id: string) => Promise<void>;
  deleteModel: (id: string) => Promise<void>;
  prepare: (request: LocalVoiceRequest) => void;
  /** `samples` is mono PCM at 16 kHz. */
  transcribe: (request: LocalVoiceRequest & { samples: Float32Array }) => Promise<LocalVoiceResult>;
}

export function localVoiceAPI(): LocalVoiceAPI | undefined {
  if (typeof window === "undefined") return undefined;
  const api = (window as unknown as { desktopAPI?: { voice?: Partial<LocalVoiceAPI> } }).desktopAPI?.voice;
  return api?.listModels && api.onModelsChanged && api.transcribe ? (api as LocalVoiceAPI) : undefined;
}
