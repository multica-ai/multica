/**
 * The on-device engine as the voice layer sees it: which models are
 * installed (kept live from Desktop) and turning a recorded clip into text.
 */
import { useSyncExternalStore } from "react";
import { useVoiceSettingsStore } from "@multica/core/voice";
import {
  localVoiceAPI,
  type LocalVoiceModel,
  type LocalVoiceModels,
  type LocalVoiceResult,
} from "../platform/local-voice";

const SAMPLE_RATE = 16000;

let snapshot: LocalVoiceModels | null = null;
let subscribed = false;
const listeners = new Set<() => void>();

function publish(next: LocalVoiceModels) {
  snapshot = next;
  listeners.forEach((listener) => listener());
}

/** Start following Desktop's model list; a no-op on web or once started. */
function follow() {
  const api = localVoiceAPI();
  if (!api || subscribed) return;
  subscribed = true;
  let changed = false;
  api.onModelsChanged((next) => {
    changed = true;
    publish(next);
  });
  void api.listModels().then(
    (next) => {
      if (!changed) publish(next);
    },
    () => {},
  );
}

function subscribe(listener: () => void) {
  follow();
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** Desktop's speech models; null on web and until the first answer. */
export function useVoiceModels(): LocalVoiceModels | null {
  return useSyncExternalStore(subscribe, () => snapshot, () => null);
}

/** The installed model to use: the chosen one, else the first installed. */
export function pickModel(models: LocalVoiceModels | null, preferred: string | null): LocalVoiceModel | null {
  const ready = models?.models.filter((model) => model.status === "ready") ?? [];
  return ready.find((model) => model.id === preferred) ?? ready[0] ?? null;
}

export function activeModel(): LocalVoiceModel | null {
  follow();
  return pickModel(snapshot, useVoiceSettingsStore.getState().modelId);
}

function request(model: LocalVoiceModel) {
  return { modelId: model.id, language: useVoiceSettingsStore.getState().language };
}

/** Load the model while the user is still talking. */
export function prepareModel(model: LocalVoiceModel) {
  localVoiceAPI()?.prepare(request(model));
}

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

export async function transcribeClip(clip: Blob, model: LocalVoiceModel): Promise<LocalVoiceResult> {
  const api = localVoiceAPI();
  if (!api) return { ok: false, reason: "engine-unavailable" };
  const samples = await decodeClip(clip);
  return api.transcribe({ ...request(model), samples });
}
