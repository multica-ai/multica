import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

// The gateway decides which models this key may call, so the provider
// registers whatever GET /v1/models returns instead of a hard-coded list that
// drifts from the grants. Mirrors DiscoverFirtalGatewayModels in
// server/pkg/agent/firtal_gateway.go.
const MODELS_PATH = "/api/ai/proxy/v1/models";
const DISCOVERY_TIMEOUT_MS = 5000;

// ai_proxy_models stores neither a context window nor an output limit, so every
// discovered model gets these conservative defaults. Too small only makes Pi
// compact earlier; it never sends a request the backend rejects.
const DEFAULT_CONTEXT_WINDOW = 200000;
const DEFAULT_MAX_TOKENS = 16384;

// piGatewayFallbackModel in server/pkg/agent/cerebro_pi_fallback.go retries a
// failed turn on `firtal-gateway/<this model>`, so it stays registered even
// when discovery answers with nothing.
const DEFAULT_FALLBACK_MODEL = "claude-sonnet-5";

interface GatewayCost {
  input_per_1k?: number;
  output_per_1k?: number;
  cached_input_per_1k?: number;
}

interface GatewayModel {
  id?: string;
  firtal_display_name?: string;
  firtal_capability?: string;
  firtal_supports_vision?: boolean;
  firtal_cost?: GatewayCost;
}

// The gateway quotes USD per 1K tokens; Pi model descriptors are per 1M. There
// is no cache-write column — the gateway coster derives that from the input
// rate — so Pi is told 0 rather than a guess.
function perMillionCost(cost: GatewayCost | undefined) {
  return {
    input: (cost?.input_per_1k ?? 0) * 1000,
    output: (cost?.output_per_1k ?? 0) * 1000,
    cacheRead: (cost?.cached_input_per_1k ?? 0) * 1000,
    cacheWrite: 0,
  };
}

function describeModel(id: string, entry: GatewayModel = {}) {
  const displayName = entry.firtal_display_name?.trim();
  const input: ("text" | "image")[] =
    entry.firtal_supports_vision === false ? ["text"] : ["text", "image"];
  return {
    id,
    name: displayName ? `Firtal AI Gateway (${displayName})` : `Firtal AI Gateway (${id})`,
    reasoning: true,
    input,
    cost: perMillionCost(entry.firtal_cost),
    contextWindow: DEFAULT_CONTEXT_WINDOW,
    maxTokens: DEFAULT_MAX_TOKENS,
  };
}

export function toProviderModels(payload: unknown, fallbackModel: string) {
  const data = (payload as { data?: unknown } | undefined)?.data;
  const entries: GatewayModel[] = Array.isArray(data) ? (data as GatewayModel[]) : [];

  const models = entries
    // Embeddings and other non-chat capabilities cannot serve a Pi turn.
    .filter((entry) => entry.firtal_capability === undefined || entry.firtal_capability === "chat")
    .map((entry) => ({ id: entry.id?.trim() ?? "", entry }))
    .filter(({ id }) => id !== "")
    .map(({ id, entry }) => describeModel(id, entry));

  if (!models.some((model) => model.id === fallbackModel)) {
    models.unshift(describeModel(fallbackModel));
  }
  return models;
}

async function fetchModels(registryUrl: string, registryKey: string): Promise<unknown> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), DISCOVERY_TIMEOUT_MS);
  try {
    const response = await fetch(`${registryUrl}${MODELS_PATH}`, {
      headers: { authorization: `Bearer ${registryKey}`, accept: "application/json" },
      signal: controller.signal,
    });
    if (!response.ok) return undefined;
    return await response.json();
  } catch {
    // A gateway that is down or slow must not stop Pi from starting — the
    // fallback model on its own is still a working provider.
    return undefined;
  } finally {
    clearTimeout(timer);
  }
}

export default async function registerFirtalGateway(pi: ExtensionAPI) {
  const registryUrl = process.env.FIRTAL_REGISTRY_URL?.trim().replace(/\/$/, "");
  const registryKey = process.env.FIRTAL_REGISTRY_KEY?.trim();
  if (!registryUrl || !registryKey) return;

  const fallbackModel = process.env.FIRTAL_REGISTRY_MODEL?.trim() || DEFAULT_FALLBACK_MODEL;
  const payload = await fetchModels(registryUrl, registryKey);

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
    models: toProviderModels(payload, fallbackModel),
  });
}
