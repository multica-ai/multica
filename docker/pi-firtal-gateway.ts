import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

export default function registerFirtalGateway(pi: ExtensionAPI) {
  const registryUrl = process.env.FIRTAL_REGISTRY_URL?.trim().replace(/\/$/, "");
  const registryKey = process.env.FIRTAL_REGISTRY_KEY?.trim();
  if (!registryUrl || !registryKey) return;

  const model = process.env.FIRTAL_REGISTRY_MODEL?.trim() || "claude-sonnet-5";
  pi.registerProvider("firtal-gateway", {
    name: "Firtal AI Gateway",
    baseUrl: `${registryUrl}/api/ai/proxy/v1`,
    apiKey: "$FIRTAL_REGISTRY_KEY",
    authHeader: true,
    headers: {
      "X-Skill": "multica-pi-runtime-fallback",
      "X-Tags": "multica,cerebro,pi-fallback",
    },
    api: "openai-completions",
    models: [
      {
        id: model,
        name: `Firtal AI Gateway (${model})`,
        reasoning: true,
        input: ["text", "image"],
        cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
        contextWindow: 200000,
        maxTokens: 16384,
      },
    ],
  });
}
