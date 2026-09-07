import type { PiApiType } from "@multica/core/agents";

export type PiProviderPreset = {
  id: string;
  label: string;
  provider: string;
  api: PiApiType;
  baseUrl: string;
  models: readonly string[];
  defaultModel: string;
  /**
   * Where this provider issues API keys. The most common reason setup stalls
   * is not the form — it is "I don't have a key and don't know where to get
   * one", so every preset must be able to answer that in one click.
   */
  consoleUrl: string;
};

export const PI_PROVIDER_PRESETS = [
  {
    id: "deepseek",
    label: "DeepSeek",
    provider: "deepseek",
    api: "openai-completions",
    baseUrl: "https://api.deepseek.com",
    models: ["deepseek-v4-flash", "deepseek-v4-pro"],
    defaultModel: "deepseek-v4-flash",
    consoleUrl: "https://platform.deepseek.com/api_keys",
  },
  {
    id: "openai",
    label: "OpenAI",
    provider: "openai",
    api: "openai-responses",
    baseUrl: "https://api.openai.com/v1",
    models: ["gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"],
    defaultModel: "gpt-5.6-sol",
    consoleUrl: "https://platform.openai.com/api-keys",
  },
  {
    id: "anthropic",
    label: "Anthropic",
    provider: "anthropic",
    api: "anthropic-messages",
    baseUrl: "https://api.anthropic.com",
    models: [
      "claude-sonnet-5",
      "claude-fable-5",
      "claude-opus-5",
      "claude-haiku-4-5-20251001",
    ],
    defaultModel: "claude-sonnet-5",
    consoleUrl: "https://console.anthropic.com/settings/keys",
  },
  {
    id: "google",
    label: "Google Gemini",
    provider: "google",
    api: "google-generative-ai",
    baseUrl: "https://generativelanguage.googleapis.com/v1beta",
    models: [
      "gemini-3.6-flash",
      "gemini-3.5-flash",
      "gemini-3.5-flash-lite",
      "gemini-3.1-flash-lite",
      "gemini-3.1-pro-preview",
      "gemini-3-flash-preview",
    ],
    defaultModel: "gemini-3.6-flash",
    consoleUrl: "https://aistudio.google.com/apikey",
  },
  {
    id: "xai",
    label: "xAI",
    provider: "xai",
    api: "openai-completions",
    baseUrl: "https://api.x.ai/v1",
    models: ["grok-4.5"],
    defaultModel: "grok-4.5",
    consoleUrl: "https://console.x.ai",
  },
  {
    id: "minimax",
    label: "MiniMax",
    provider: "minimax",
    api: "openai-responses",
    baseUrl: "https://api.minimax.io/v1",
    models: ["MiniMax-M3"],
    defaultModel: "MiniMax-M3",
    consoleUrl: "https://platform.minimax.io",
  },
  {
    id: "kimi",
    label: "Kimi",
    provider: "moonshot",
    api: "openai-completions",
    baseUrl: "https://api.moonshot.ai/v1",
    models: ["kimi-k3", "kimi-k2.7-code", "kimi-k2.7-code-highspeed"],
    defaultModel: "kimi-k3",
    consoleUrl: "https://platform.moonshot.ai/console/api-keys",
  },
  {
    id: "zai",
    label: "Z.ai",
    provider: "zai",
    api: "openai-completions",
    baseUrl: "https://api.z.ai/api/paas/v4",
    models: ["glm-5.2", "glm-5-turbo"],
    defaultModel: "glm-5.2",
    consoleUrl: "https://z.ai",
  },
] as const satisfies readonly PiProviderPreset[];

export type PiProviderPresetId = (typeof PI_PROVIDER_PRESETS)[number]["id"];

export const PI_DEFAULT_PROVIDER_PRESET = PI_PROVIDER_PRESETS[0];

/**
 * Providers to surface first, per locale. Setup asks the user for exactly one
 * thing, so the list has to lead with providers that audience can actually
 * sign up for and pay — a Chinese user scrolling past four providers they
 * cannot reach is a step we lost for no reason.
 *
 * Unlisted presets keep their declaration order behind the leaders, so adding
 * a preset never silently disappears from a locale.
 */
const PRESET_PRIORITY_BY_LANGUAGE: Record<string, readonly string[]> = {
  zh: ["deepseek", "kimi", "zai", "minimax"],
  ja: ["openai", "anthropic", "google"],
  ko: ["openai", "anthropic", "google"],
  en: ["openai", "anthropic", "deepseek", "google"],
};

const DEFAULT_PRESET_PRIORITY = PRESET_PRIORITY_BY_LANGUAGE.en!;

/**
 * How many presets to show before "more providers". Kept small on purpose:
 * a grid of eight logos reads as a decision, a grid of four reads as a
 * shortcut.
 */
export const PI_PRIMARY_PRESET_COUNT = 4;

function languageOf(locale: string | undefined): string {
  return (locale ?? "").trim().toLowerCase().split(/[-_]/)[0] ?? "";
}

export function orderPresetsForLocale(
  locale: string | undefined,
): readonly PiProviderPreset[] {
  // Widen away from the `as const` tuple so the reordered result is a plain
  // preset list rather than a union of eight literal shapes.
  const all: readonly PiProviderPreset[] = PI_PROVIDER_PRESETS;
  const priority =
    PRESET_PRIORITY_BY_LANGUAGE[languageOf(locale)] ?? DEFAULT_PRESET_PRIORITY;
  const leading: PiProviderPreset[] = [];
  for (const id of priority) {
    const match = all.find((preset) => preset.id === id);
    if (match) leading.push(match);
  }
  const leadingIds = new Set(leading.map((preset) => preset.id));
  return [...leading, ...all.filter((preset) => !leadingIds.has(preset.id))];
}

export function defaultPresetForLocale(
  locale: string | undefined,
): PiProviderPreset {
  return orderPresetsForLocale(locale)[0] ?? PI_DEFAULT_PROVIDER_PRESET;
}

export function getPiProviderPreset(
  id: string,
): PiProviderPreset | undefined {
  return PI_PROVIDER_PRESETS.find((preset) => preset.id === id);
}

export function findPiProviderPreset(config: {
  provider?: string;
  api?: PiApiType;
  baseUrl?: string;
}): PiProviderPreset | undefined {
  const provider = config.provider?.trim().toLowerCase();
  const api = config.api;
  const baseUrl = normalizeBaseUrl(config.baseUrl);
  return PI_PROVIDER_PRESETS.find(
    (preset) =>
      preset.provider.toLowerCase() === provider &&
      preset.api === api &&
      normalizeBaseUrl(preset.baseUrl) === baseUrl,
  );
}

export function piProviderCatalogModels(provider: string): readonly string[] {
  const normalized = provider.trim().toLowerCase();
  return (
    PI_PROVIDER_PRESETS.find(
      (preset) => preset.provider.toLowerCase() === normalized,
    )?.models ?? []
  );
}

function normalizeBaseUrl(value: string | undefined): string {
  return value?.trim().replace(/\/+$/, "") ?? "";
}
