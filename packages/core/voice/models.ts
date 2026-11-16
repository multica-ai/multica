"use client";

import { create } from "zustand";
import { useVoiceSettingsStore } from "./store";

export type VoiceModelStatus = "absent" | "downloading" | "ready";

/** A speech model the on-device engine can run. */
export interface VoiceModel {
  id: string;
  name: string;
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
  receivedBytes: number;
  error?: string;
  /**
   * Transcribed by the server's speech provider while the user talks, through
   * the runtime's `stream`; nothing to download or delete on this device.
   */
  streams?: boolean;
}

export interface VoiceModels {
  /** False when this build has no speech engine for this platform. */
  engineAvailable: boolean;
  models: VoiceModel[];
}

export type VoiceTranscription =
  | { ok: true; text: string }
  | { ok: false; reason: "model-missing" | "engine-unavailable" | "failed"; error?: string };

interface VoiceModelsState {
  /** Null on web and until the engine first answers. */
  models: VoiceModels | null;
  setModels: (models: VoiceModels) => void;
}

/** The engine's model list, kept live by the connected voice runtime. */
export const useVoiceModelsStore = create<VoiceModelsState>()((set) => ({
  models: null,
  setModels: (models) => set({ models }),
}));

/**
 * The installed model to use: the chosen one, else the first installed. When
 * a chosen model is gone, an on-device model stands in before a streaming one,
 * so losing a local choice does not quietly send speech to the server.
 */
export function pickVoiceModel(models: VoiceModels | null, preferred: string | null): VoiceModel | null {
  const ready = models?.models.filter((model) => model.status === "ready") ?? [];
  const chosen = ready.find((model) => model.id === preferred);
  if (chosen) return chosen;
  if (preferred !== null) return ready.find((model) => !model.streams) ?? ready[0] ?? null;
  return ready[0] ?? null;
}

export function activeVoiceModel(): VoiceModel | null {
  return pickVoiceModel(useVoiceModelsStore.getState().models, useVoiceSettingsStore.getState().modelId);
}

/**
 * The model to transcribe a kept clip with: the chosen one, unless it only
 * streams, then the first installed model that takes clips.
 */
export function pickClipVoiceModel(models: VoiceModels | null, preferred: string | null): VoiceModel | null {
  const chosen = pickVoiceModel(models, preferred);
  if (!chosen?.streams) return chosen;
  return models?.models.find((model) => model.status === "ready" && !model.streams) ?? null;
}

export function clipVoiceModel(): VoiceModel | null {
  return pickClipVoiceModel(useVoiceModelsStore.getState().models, useVoiceSettingsStore.getState().modelId);
}
