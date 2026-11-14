"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";
import { VOICE_HISTORY_RETENTIONS, type VoiceHistoryRetention } from "./history";

/**
 * How a press of the mic (or the voice shortcut) starts a recording.
 * - auto: a tap toggles dictation, holding talks until release
 * - toggle: every press toggles
 * - hold: record only while held
 */
export type VoiceActivation = "auto" | "toggle" | "hold";

/**
 * Languages a model can be told to expect, for models that take a hint;
 * "auto" lets the model detect.
 */
export const VOICE_LANGUAGES = ["auto", "zh", "en", "ja", "ko", "yue"] as const;
export type VoiceLanguage = (typeof VOICE_LANGUAGES)[number];

export const MAX_VOCABULARY_WORDS = 40;
export const MAX_VOCABULARY_WORD_LENGTH = 40;

export interface VoiceSettings {
  activation: VoiceActivation;
  language: VoiceLanguage;
  /** Input device id; null follows the system default. */
  deviceId: string | null;
  removeFillers: boolean;
  /** Words to spell the way the user writes them. */
  vocabulary: string[];
  /** The local speech model to use; null picks the first one installed. */
  modelId: string | null;
  /** How long recordings and transcripts stay on this device. */
  keepHistory: VoiceHistoryRetention;
}

export const DEFAULT_VOICE_SETTINGS: VoiceSettings = {
  activation: "auto",
  language: "auto",
  deviceId: null,
  removeFillers: true,
  vocabulary: [],
  modelId: null,
  keepHistory: "30d",
};

interface VoiceSettingsState extends VoiceSettings {
  update: (patch: Partial<VoiceSettings>) => void;
  addWord: (word: string) => void;
  removeWord: (word: string) => void;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** Keep what is valid from storage and fall back to defaults for the rest. */
export function sanitizeVoiceSettings(value: unknown): VoiceSettings {
  const raw = isRecord(value) ? value : {};
  const activation = ["auto", "toggle", "hold"].includes(raw.activation as string)
    ? (raw.activation as VoiceActivation)
    : DEFAULT_VOICE_SETTINGS.activation;
  const language = (VOICE_LANGUAGES as readonly string[]).includes(raw.language as string)
    ? (raw.language as VoiceLanguage)
    : DEFAULT_VOICE_SETTINGS.language;
  const deviceId = typeof raw.deviceId === "string" && raw.deviceId ? raw.deviceId : null;
  const removeFillers =
    typeof raw.removeFillers === "boolean" ? raw.removeFillers : DEFAULT_VOICE_SETTINGS.removeFillers;
  const vocabulary = Array.isArray(raw.vocabulary)
    ? normalizeVocabulary(raw.vocabulary.filter((w): w is string => typeof w === "string"))
    : [];
  const modelId = typeof raw.modelId === "string" && raw.modelId ? raw.modelId : null;
  const keepHistory = (VOICE_HISTORY_RETENTIONS as readonly string[]).includes(raw.keepHistory as string)
    ? (raw.keepHistory as VoiceHistoryRetention)
    : DEFAULT_VOICE_SETTINGS.keepHistory;
  return { activation, language, deviceId, removeFillers, vocabulary, modelId, keepHistory };
}

function normalizeVocabulary(words: readonly string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const word of words) {
    const trimmed = word.trim().slice(0, MAX_VOCABULARY_WORD_LENGTH);
    const key = trimmed.toLowerCase();
    if (!trimmed || seen.has(key)) continue;
    seen.add(key);
    out.push(trimmed);
    if (out.length >= MAX_VOCABULARY_WORDS) break;
  }
  return out;
}

export const useVoiceSettingsStore = create<VoiceSettingsState>()(
  persist(
    (set) => ({
      ...DEFAULT_VOICE_SETTINGS,
      update: (patch) => set((state) => sanitizeVoiceSettings({ ...pickSettings(state), ...patch })),
      addWord: (word) =>
        set((state) => ({ vocabulary: normalizeVocabulary([...state.vocabulary, word]) })),
      removeWord: (word) =>
        set((state) => ({ vocabulary: state.vocabulary.filter((w) => w !== word) })),
    }),
    {
      name: "multica_voice_input",
      storage: createJSONStorage(() => defaultStorage),
      version: 1,
      partialize: (state): VoiceSettings => pickSettings(state),
      merge: (persisted, current): VoiceSettingsState => ({ ...current, ...sanitizeVoiceSettings(persisted) }),
    },
  ),
);

function pickSettings(state: VoiceSettings): VoiceSettings {
  return {
    activation: state.activation,
    language: state.language,
    deviceId: state.deviceId,
    removeFillers: state.removeFillers,
    vocabulary: state.vocabulary,
    modelId: state.modelId,
    keepHistory: state.keepHistory,
  };
}
