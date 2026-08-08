import type { PiApiType } from "@multica/core/agents";

export type PiProviderPreset = {
  id: string;
  label: string;
  provider: string;
  api: PiApiType;
  baseUrl: string;
  models: readonly string[];
  defaultModel: string;
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
  },
  {
    id: "openai",
    label: "OpenAI",
    provider: "openai",
    api: "openai-responses",
    baseUrl: "https://api.openai.com/v1",
    models: ["gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"],
    defaultModel: "gpt-5.6-sol",
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
  },
  {
    id: "xai",
    label: "xAI",
    provider: "xai",
    api: "openai-completions",
    baseUrl: "https://api.x.ai/v1",
    models: ["grok-4.5"],
    defaultModel: "grok-4.5",
  },
  {
    id: "minimax",
    label: "MiniMax",
    provider: "minimax",
    api: "openai-responses",
    baseUrl: "https://api.minimax.io/v1",
    models: ["MiniMax-M3"],
    defaultModel: "MiniMax-M3",
  },
  {
    id: "kimi",
    label: "Kimi",
    provider: "moonshot",
    api: "openai-completions",
    baseUrl: "https://api.moonshot.ai/v1",
    models: ["kimi-k3", "kimi-k2.7-code", "kimi-k2.7-code-highspeed"],
    defaultModel: "kimi-k3",
  },
  {
    id: "zai",
    label: "Z.ai",
    provider: "zai",
    api: "openai-completions",
    baseUrl: "https://api.z.ai/api/paas/v4",
    models: ["glm-5.2", "glm-5-turbo"],
    defaultModel: "glm-5.2",
  },
] as const satisfies readonly PiProviderPreset[];

export type PiProviderPresetId = (typeof PI_PROVIDER_PRESETS)[number]["id"];

export const PI_DEFAULT_PROVIDER_PRESET = PI_PROVIDER_PRESETS[0];

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
