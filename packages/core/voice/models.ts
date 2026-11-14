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

/** The installed model to use: the chosen one, else the first installed. */
export function pickVoiceModel(models: VoiceModels | null, preferred: string | null): VoiceModel | null {
  const ready = models?.models.filter((model) => model.status === "ready") ?? [];
  return ready.find((model) => model.id === preferred) ?? ready[0] ?? null;
}

export function activeVoiceModel(): VoiceModel | null {
  return pickVoiceModel(useVoiceModelsStore.getState().models, useVoiceSettingsStore.getState().modelId);
}
