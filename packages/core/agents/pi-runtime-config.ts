// Pi-specific runtime_config schema. Secrets intentionally do not belong here:
// the API key is stored in the audited custom_env endpoint under this fixed key
// and models.json references it by name.

// Deliberately does not use the reserved MULTICA_ prefix: the daemon rejects
// custom_env keys in that namespace before spawning provider processes.
export const PI_API_KEY_ENV = "PI_MULTICA_API_KEY";

export const PI_API_TYPES = [
  "openai-completions",
  "openai-responses",
  "anthropic-messages",
  "google-generative-ai",
] as const;

export type PiApiType = (typeof PI_API_TYPES)[number];

export interface PiRuntimeConfig {
  provider?: string;
  api?: PiApiType;
  baseUrl?: string;
  model?: string;
}

export function parsePiRuntimeConfig(raw: unknown): PiRuntimeConfig {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return {};
  const root = raw as Record<string, unknown>;
  const out: PiRuntimeConfig = {};
  if (typeof root.provider === "string") out.provider = root.provider;
  if (
    typeof root.api === "string" &&
    PI_API_TYPES.includes(root.api as PiApiType)
  ) {
    out.api = root.api as PiApiType;
  }
  if (typeof root.base_url === "string") out.baseUrl = root.base_url;
  if (typeof root.model === "string") out.model = root.model;
  return out;
}

export function serializePiRuntimeConfig(
  config: PiRuntimeConfig,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (config.provider) out.provider = config.provider.trim();
  if (config.api) out.api = config.api;
  if (config.baseUrl) out.base_url = config.baseUrl.trim();
  if (config.model) out.model = config.model.trim();
  return out;
}

export function piRuntimeConfigEquals(
  a: PiRuntimeConfig,
  b: PiRuntimeConfig,
): boolean {
  return (
    (a.provider ?? "") === (b.provider ?? "") &&
    (a.api ?? "") === (b.api ?? "") &&
    (a.baseUrl ?? "") === (b.baseUrl ?? "") &&
    (a.model ?? "") === (b.model ?? "")
  );
}

export function validatePiRuntimeConfig(config: PiRuntimeConfig): string | null {
  const values = [config.provider, config.api, config.baseUrl, config.model];
  const filled = values.filter((value) => value?.trim()).length;
  if (filled === 0) return null;
  if (filled !== values.length) return "incomplete";
  if (!/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/.test(config.provider!)) {
    return "provider";
  }

  let url: URL;
  try {
    url = new URL(config.baseUrl!);
  } catch {
    return "base_url";
  }
  if (url.username || url.password) return "base_url";
  if (url.protocol !== "https:" && url.protocol !== "http:") {
    return "base_url";
  }
  if (url.protocol === "http:" && !isLoopbackHost(url.hostname)) {
    return "base_url";
  }
  return null;
}

function isLoopbackHost(host: string): boolean {
  const normalized = host.replace(/^\[|\]$/g, "").toLowerCase();
  if (normalized === "localhost" || normalized === "::1") return true;
  const octets = normalized.split(".");
  return (
    octets.length === 4 &&
    octets[0] === "127" &&
    octets.every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255)
  );
}
