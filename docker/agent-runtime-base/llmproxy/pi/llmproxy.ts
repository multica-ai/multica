// Pi extension — auto-discovered from ~/.pi/agent/extensions/ at startup.
// Pi has no *_BASE_URL env var; pi.registerProvider() is the only override
// (see earendil-works/pi-mono's custom-provider.md).
//
// Anthropic uses the proxy root (SDK appends /v1/messages); OpenAI uses
// /v1.

import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

const LLM_PROXY = "https://llmproxy.g2.com";

export default function (pi: ExtensionAPI) {
  pi.registerProvider("anthropic", {
    baseUrl: LLM_PROXY,
  });

  pi.registerProvider("openai", {
    baseUrl: `${LLM_PROXY}/v1`,
  });
}
