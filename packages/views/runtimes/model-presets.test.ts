import { describe, expect, it } from "vitest";
import {
  PI_PROVIDER_PRESETS,
  defaultPresetForLocale,
  findPiProviderPreset,
  orderPresetsForLocale,
  piProviderCatalogModels,
} from "./model-presets";

describe("Pi provider presets", () => {
  it("pins provider API types and official base URLs", () => {
    expect(
      PI_PROVIDER_PRESETS.map(({ id, provider, api, baseUrl }) => ({
        id,
        provider,
        api,
        baseUrl,
      })),
    ).toEqual([
      {
        id: "deepseek",
        provider: "deepseek",
        api: "openai-completions",
        baseUrl: "https://api.deepseek.com",
      },
      {
        id: "openai",
        provider: "openai",
        api: "openai-responses",
        baseUrl: "https://api.openai.com/v1",
      },
      {
        id: "anthropic",
        provider: "anthropic",
        api: "anthropic-messages",
        baseUrl: "https://api.anthropic.com",
      },
      {
        id: "google",
        provider: "google",
        api: "google-generative-ai",
        baseUrl: "https://generativelanguage.googleapis.com/v1beta",
      },
      {
        id: "xai",
        provider: "xai",
        api: "openai-completions",
        baseUrl: "https://api.x.ai/v1",
      },
      {
        id: "minimax",
        provider: "minimax",
        api: "openai-responses",
        baseUrl: "https://api.minimax.io/v1",
      },
      {
        id: "kimi",
        provider: "moonshot",
        api: "openai-completions",
        baseUrl: "https://api.moonshot.ai/v1",
      },
      {
        id: "zai",
        provider: "zai",
        api: "openai-completions",
        baseUrl: "https://api.z.ai/api/paas/v4",
      },
    ]);
  });

  it("uses current public model ids without older generations", () => {
    expect(
      Object.fromEntries(
        PI_PROVIDER_PRESETS.map((preset) => [preset.id, preset.models]),
      ),
    ).toEqual({
      deepseek: ["deepseek-v4-flash", "deepseek-v4-pro"],
      openai: ["gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"],
      anthropic: [
        "claude-sonnet-5",
        "claude-fable-5",
        "claude-opus-5",
        "claude-haiku-4-5-20251001",
      ],
      google: [
        "gemini-3.6-flash",
        "gemini-3.5-flash",
        "gemini-3.5-flash-lite",
        "gemini-3.1-flash-lite",
        "gemini-3.1-pro-preview",
        "gemini-3-flash-preview",
      ],
      xai: ["grok-4.5"],
      minimax: ["MiniMax-M3"],
      kimi: ["kimi-k3", "kimi-k2.7-code", "kimi-k2.7-code-highspeed"],
      zai: ["glm-5.2", "glm-5-turbo"],
    });
  });

  it("keeps every default model in its provider model list", () => {
    for (const preset of PI_PROVIDER_PRESETS) {
      expect(preset.models).toContain(preset.defaultModel);
    }
  });

  it("looks up presets and catalog models with normalized provider data", () => {
    expect(
      findPiProviderPreset({
        provider: "xai",
        api: "openai-completions",
        baseUrl: "https://api.x.ai/v1/",
      })?.id,
    ).toBe("xai");
    expect(piProviderCatalogModels("Moonshot")).toContain("kimi-k3");
  });
});

describe("orderPresetsForLocale", () => {
  it("leads with providers a Chinese user can actually sign up for", () => {
    const ordered = orderPresetsForLocale("zh-Hans");
    expect(ordered.slice(0, 4).map((p) => p.id)).toEqual([
      "deepseek",
      "kimi",
      "zai",
      "minimax",
    ]);
  });

  it("leads with the global providers for English", () => {
    const ordered = orderPresetsForLocale("en");
    expect(ordered[0]?.id).toBe("openai");
  });

  it("falls back to the English order for an unknown locale", () => {
    expect(orderPresetsForLocale("xx-YY").map((p) => p.id)).toEqual(
      orderPresetsForLocale("en").map((p) => p.id),
    );
    expect(orderPresetsForLocale(undefined)[0]?.id).toBe("openai");
  });

  it("never drops a preset from any locale", () => {
    for (const locale of ["zh-Hans", "en", "ja", "ko", "xx"]) {
      const ordered = orderPresetsForLocale(locale);
      expect(ordered).toHaveLength(PI_PROVIDER_PRESETS.length);
      expect(new Set(ordered.map((p) => p.id)).size).toBe(
        PI_PROVIDER_PRESETS.length,
      );
    }
  });

  it("matches the language regardless of region or case", () => {
    expect(orderPresetsForLocale("ZH_CN")[0]?.id).toBe("deepseek");
  });

  it("gives every preset a key console link", () => {
    for (const preset of PI_PROVIDER_PRESETS) {
      expect(preset.consoleUrl).toMatch(/^https:\/\//);
    }
  });

  it("defaults to the locale's first provider", () => {
    expect(defaultPresetForLocale("zh-Hans").id).toBe("deepseek");
    expect(defaultPresetForLocale("en").id).toBe("openai");
  });
});
