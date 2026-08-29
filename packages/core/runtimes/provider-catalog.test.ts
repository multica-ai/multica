// @vitest-environment node

import { describe, expect, it } from "vitest";

import {
  REPLACEMENT_RUNTIME_PROVIDERS,
  replacementRuntimeProvider,
} from "./provider-catalog";

describe("replacement runtime provider catalog", () => {
  it("covers every requested subscription and API runtime", () => {
    expect(REPLACEMENT_RUNTIME_PROVIDERS.map((provider) => provider.id)).toEqual([
      "codex",
      "claude",
      "antigravity",
      "cursor",
      "grok",
      "opencode",
      "opencode-api",
      "opencode-zen",
      "opencode-go",
      "openrouter",
      "vercel-ai-gateway",
      "ollama",
      "lmstudio",
      "nvidia-nim",
    ]);
  });

  it("keeps credential names in metadata without accepting credential values", () => {
    const openrouter = replacementRuntimeProvider("openrouter");
    expect(openrouter?.apiKeyEnv).toBe("OPENROUTER_API_KEY");
    expect(openrouter).not.toHaveProperty("apiKey");
  });

  it("distinguishes daemon API providers from subscription CLIs", () => {
    expect(replacementRuntimeProvider("codex")).toMatchObject({
      setup: "subscription",
      execution: "cli",
    });
    expect(replacementRuntimeProvider("ollama")).toMatchObject({
      setup: "local",
      execution: "openai-compatible",
      defaultBaseUrl: "http://127.0.0.1:11434/v1",
    });
  });

  it("does not advertise MCP for API execution providers", () => {
    const apiProviders = REPLACEMENT_RUNTIME_PROVIDERS.filter(
      (provider) => provider.execution === "openai-compatible",
    );
    expect(apiProviders.map((provider) => provider.id)).toEqual([
      "opencode-api",
      "opencode-zen",
      "opencode-go",
      "openrouter",
      "vercel-ai-gateway",
      "ollama",
      "lmstudio",
      "nvidia-nim",
    ]);
    for (const provider of apiProviders) {
      expect(provider.capabilities).not.toContain("mcp");
    }
  });

  it("preserves native CLI capability declarations", () => {
    expect(
      REPLACEMENT_RUNTIME_PROVIDERS.filter(
        (provider) => provider.execution === "cli",
      ).map((provider) => [provider.id, provider.capabilities]),
    ).toEqual([
      ["codex", ["streaming", "model-selection", "mcp"]],
      ["claude", ["streaming", "model-selection", "mcp"]],
      ["antigravity", ["streaming", "model-selection"]],
      ["cursor", ["streaming", "model-selection", "mcp"]],
      ["grok", ["streaming", "model-selection", "mcp"]],
      ["opencode", ["streaming", "model-selection", "mcp"]],
    ]);
  });
});
