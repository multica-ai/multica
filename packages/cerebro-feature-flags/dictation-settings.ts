// FIR-1797 — user-configurable dictation tuning: a personal glossary that
// biases the speech-to-text model toward your own terms (brand/product names,
// jargon) and an opt-in cleanup pass that polishes punctuation and structure
// (the "Wispr Flow"-style finish). Persisted in the server-synced
// user.preferences blob (like the Secretary criteria), so the choice follows
// the user across devices. The settings UI lives in this package (rendered
// under the Dictation toggle in the Cerebro features tab); the values are read
// by @multica/cerebro-dictation when it builds the transcribe request.
"use client";

import { useCallback } from "react";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";

export const DICTATION_SETTINGS_KEY = "cerebro_dictation_settings";

export interface DictationSettings {
  /**
   * Free-text glossary of domain terms (one per line or comma separated). Sent
   * to the model as a decoding hint so proper nouns are spelled correctly.
   */
  glossary: string;
  /**
   * When true, the raw transcript is run through an LLM cleanup pass that fixes
   * punctuation, capitalisation and paragraph breaks without changing words.
   */
  cleanup: boolean;
}

export const DEFAULT_DICTATION_SETTINGS: DictationSettings = {
  glossary: "",
  // On by default — the cleanup pass is the "Wispr Flow"-style finish everyone
  // wants; users can switch it off. It is still a no-op server-side until the
  // inference key is configured.
  cleanup: true,
};

export function readDictationSettings(value: unknown): DictationSettings {
  if (!value || typeof value !== "object") return DEFAULT_DICTATION_SETTINGS;
  const v = value as Partial<Record<keyof DictationSettings, unknown>>;
  return {
    glossary: typeof v.glossary === "string" ? v.glossary : DEFAULT_DICTATION_SETTINGS.glossary,
    cleanup: typeof v.cleanup === "boolean" ? v.cleanup : DEFAULT_DICTATION_SETTINGS.cleanup,
  };
}

/**
 * Normalise the free-text glossary into the comma-separated form the inference
 * service expects, collapsing newlines and trimming blanks. Empty in → empty
 * out (no bias sent).
 */
export function glossaryToHint(glossary: string): string {
  return glossary
    .split(/[\n,]+/)
    .map((term) => term.trim())
    .filter(Boolean)
    .join(", ");
}

export function useDictationSettings(): DictationSettings {
  const user = useAuthStore((s) => s.user);
  return readDictationSettings(user?.preferences?.[DICTATION_SETTINGS_KEY]);
}

export function useSetDictationSettings(): (
  patch: Partial<DictationSettings>,
) => Promise<void> {
  const setUser = useAuthStore((s) => s.setUser);
  return useCallback(
    async (patch) => {
      const current = readDictationSettings(
        useAuthStore.getState().user?.preferences?.[DICTATION_SETTINGS_KEY],
      );
      const next: DictationSettings = { ...current, ...patch };
      const updated = await api.updateMyPreferences({ [DICTATION_SETTINGS_KEY]: next });
      setUser(updated);
    },
    [setUser],
  );
}
