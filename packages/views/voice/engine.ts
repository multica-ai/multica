/**
 * The voice session's runtime: the microphone recorder plus a speech engine.
 * Desktop transcribes on the device through the preload IPC. A browser has no
 * local engine, so the web app streams to the speech provider the server
 * configured, with a short-lived token the server mints.
 */
import { useEffect } from "react";
import { api } from "@multica/core/api";
import { useConfigStore } from "@multica/core/config";
import {
  connectVoiceRuntime,
  useVoiceModelsStore,
  type VoiceDecodeOptions,
  type VoiceModel,
  type VoiceModels,
  type VoiceRuntime,
} from "@multica/core/voice";
import { localVoiceAPI } from "../platform/local-voice";
import { DeepgramStream } from "./deepgram-stream";
import { Recording } from "./recorder";

const SAMPLE_RATE = 16000;

/** Mono samples at 16 kHz; the decoder resamples to the context's rate. */
async function decodeClip(clip: Blob): Promise<Float32Array> {
  const context = new OfflineAudioContext(1, 1, SAMPLE_RATE);
  const audio = await context.decodeAudioData(await clip.arrayBuffer());
  if (audio.numberOfChannels === 1) return audio.getChannelData(0);
  const mixed = new Float32Array(audio.length);
  for (let channel = 0; channel < audio.numberOfChannels; channel++) {
    const data = audio.getChannelData(channel);
    for (let i = 0; i < data.length; i++) mixed[i]! += data[i]! / audio.numberOfChannels;
  }
  return mixed;
}

function desktopVoiceRuntime(): VoiceRuntime | null {
  const api = localVoiceAPI();
  if (!api) return null;
  const history = api.history;
  const request = (model: VoiceModel, { language, hotwords }: VoiceDecodeOptions) => ({
    modelId: model.id,
    language,
    hotwords,
  });
  return {
    record: (onMeter, onLimit) => new Recording(onMeter, onLimit),
    prepare: (model, options) => api.prepare(request(model, options)),
    transcribe: async (clip, model, options) =>
      api.transcribe({ ...request(model, options), samples: await decodeClip(clip) }),
    history: history && {
      save: async (clip, keepDays) => {
        const entry = await history.save({
          audio: new Uint8Array(await clip.blob.arrayBuffer()),
          mimeType: clip.blob.type,
          durationMs: clip.durationMs,
          keepDays,
        });
        return entry?.id ?? null;
      },
      update: (id, status, text) => void history.update(id, { status, text }).catch(() => {}),
    },
    watchModels: (onChange) => {
      let changed = false;
      api.onModelsChanged((models) => {
        changed = true;
        onChange(models);
      });
      void api.listModels().then(
        (models) => {
          if (!changed) onChange(models);
        },
        () => {},
      );
    },
  };
}

/**
 * The server engine's one model: always ready, nothing to download. Flux
 * hears English and decides its own language handling, so the app offers no
 * language choice for it.
 */
export const SERVER_VOICE_MODEL: VoiceModel = {
  id: "server",
  name: "Server",
  author: "",
  licenseUrl: "",
  languages: ["en"],
  languageHint: false,
  sizeBytes: 0,
  status: "ready",
  receivedBytes: 0,
};

/** Streams to the server's speech provider: each finished sentence is typed as it ends. */
export function serverVoiceRuntime(): VoiceRuntime {
  return {
    record: (onMeter, onLimit) => new Recording(onMeter, onLimit),
    prepare: () => {},
    // Streams never send a clip; only history re-transcribes one, and the web keeps none.
    transcribe: async () => ({ ok: false, reason: "engine-unavailable" }),
    stream: (handlers, onMeter, onLimit) =>
      new DeepgramStream(handlers, onMeter, onLimit, (signal) => api.createVoiceSession(signal)),
    watchModels: (onChange) => onChange({ engineAvailable: true, models: [SERVER_VOICE_MODEL] }),
  };
}

/**
 * The speech models of this app's engine; null until one is connected and
 * answers. Desktop keeps its on-device engine; the web app uses the server's
 * once the config reports it.
 */
export function useVoiceModels(): VoiceModels | null {
  const serverTranscription = useConfigStore((s) => s.voiceTranscriptionEnabled);
  useEffect(() => {
    // The session connects one runtime for the life of the page, so the web
    // app waits for the config instead of connecting "none" first.
    if (localVoiceAPI()) connectVoiceRuntime(desktopVoiceRuntime);
    else if (serverTranscription) connectVoiceRuntime(serverVoiceRuntime);
  }, [serverTranscription]);
  return useVoiceModelsStore((s) => s.models);
}

/** Whether this app can transcribe at all: Desktop's engine or the server's. */
export function useVoiceInputAvailable(): boolean {
  const serverTranscription = useConfigStore((s) => s.voiceTranscriptionEnabled);
  return localVoiceAPI() !== undefined || serverTranscription;
}

/** Recordings go to the server rather than an engine on this device. */
export function useServerVoiceEngine(): boolean {
  const serverTranscription = useConfigStore((s) => s.voiceTranscriptionEnabled);
  return localVoiceAPI() === undefined && serverTranscription;
}
