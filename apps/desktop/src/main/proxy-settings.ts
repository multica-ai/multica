import { normalizeProxyUrl } from "../shared/runtime-config";

const PROXY_ENV_KEYS = ["HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"] as const;

export function createProxyEnvironment(env: NodeJS.ProcessEnv) {
  const original = Object.fromEntries(PROXY_ENV_KEYS.map((key) => [key, env[key]]));
  return {
    set(proxyUrl: string | null, noProxy: string | null) {
      if (!proxyUrl) {
        for (const key of PROXY_ENV_KEYS) {
          if (original[key] === undefined) delete env[key];
          else env[key] = original[key];
        }
        return;
      }
      const bypass = [...new Set([
        "localhost", "127.0.0.1", "::1",
        ...[original.NO_PROXY, original.no_proxy, noProxy].flatMap((value) =>
          (value ?? "").split(",").map((entry) => entry.trim()).filter(Boolean)),
      ])].join(",");
      env.HTTP_PROXY = env.HTTPS_PROXY = env.http_proxy = env.https_proxy = proxyUrl;
      env.NO_PROXY = env.no_proxy = bypass;
    },
  };
}

export function maskProxyUrl(value: string | null): string | null {
  if (!value) return null;
  const url = new URL(value);
  if (url.password) url.password = "********";
  return url.toString();
}

export function resolveProxyUrl(value: unknown, current: string | null): string | null {
  if (value === null || value === "") return null;
  if (typeof value !== "string") throw new Error("Invalid proxy URL");
  // The renderer only receives this exact redacted value, never the password.
  if (current && value === maskProxyUrl(current)) return current;
  return normalizeProxyUrl(value);
}

type ProxySettings = { proxyUrl: string | null; noProxy: string | null };
type Result = { ok: boolean; error?: string };

export async function changeProxySettings(
  next: ProxySettings,
  dependencies: {
    current: ProxySettings;
    confirm: (target: string) => Promise<boolean>;
    persist: (next: ProxySettings) => Promise<Result>;
    apply: (next: ProxySettings) => Promise<void>;
    restart: () => Promise<{ success: boolean; error?: string }>;
  },
): Promise<Result & { cancelled?: boolean }> {
  if (next.proxyUrl === dependencies.current.proxyUrl && next.noProxy === dependencies.current.noProxy) {
    return { ok: true };
  }
  const target = next.proxyUrl ? new URL(next.proxyUrl).host : "System proxy settings";
  if (!await dependencies.confirm(target)) return { ok: false, cancelled: true };
  const result = await dependencies.persist(next);
  if (!result.ok) return result;
  await dependencies.apply(next);
  const restart = await dependencies.restart();
  if (!restart.success) {
    return { ok: false, error: "Proxy saved, but the daemon could not restart. Restart it from Runtime settings to apply the change." };
  }
  return { ok: true };
}