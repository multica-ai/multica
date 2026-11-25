/**
 * Desktop's on-device engine as the voice session's runtime: the microphone
 * recorder, the preload IPC to the speech engine, and decoding a clip to the
 * samples the engine takes.
 */
import { useEffect } from "react";
import {
  connectVoiceRuntime,
  useVoiceModelsStore,
  type VoiceDecodeOptions,
  type VoiceModel,
  type VoiceModels,
  type VoiceRuntime,
} from "@multica/core/voice";
import { localVoiceAPI } from "../platform/local-voice";
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

/** Desktop's speech models; null on web and until the engine first answers. */
export function useVoiceModels(): VoiceModels | null {
  useEffect(() => connectVoiceRuntime(desktopVoiceRuntime), []);
  return useVoiceModelsStore((s) => s.models);
}
