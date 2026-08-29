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
});
