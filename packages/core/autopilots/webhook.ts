import type { AutopilotTrigger } from "../types";

/**
 * Compose a usable absolute webhook URL for a webhook trigger.
 *
 * Resolution order:
 *  1. trigger.webhook_url — present only when MULTICA_PUBLIC_URL is set on the
 *     server. This is the authoritative form when available.
 *  2. apiBaseUrl + webhook_path — desktop apps and self-host setups where the
 *     server didn't mint an absolute URL but the client knows its API origin.
 *  3. currentOrigin + webhook_path — browser fallback when getBaseUrl() is
 *     empty (e.g. same-origin Next.js dev).
 *
 * Returns null when the trigger has no token / path yet (a new trigger that
 * hasn't been written back to the cache, or a non-webhook trigger).
 */
export function buildAutopilotWebhookUrl(params: {
  trigger: Pick<AutopilotTrigger, "kind" | "webhook_token" | "webhook_path" | "webhook_url">;
  apiBaseUrl?: string;
  currentOrigin?: string;
}): string | null {
  const { trigger, apiBaseUrl, currentOrigin } = params;

  if (trigger.kind !== "webhook") return null;

  if (typeof trigger.webhook_url === "string" && trigger.webhook_url) {
    return trigger.webhook_url;
  }

  const path =
    (typeof trigger.webhook_path === "string" && trigger.webhook_path) ||
    (trigger.webhook_token ? `/api/webhooks/autopilots/${trigger.webhook_token}` : null);
  if (!path) return null;

  const base = stripTrailingSlash(apiBaseUrl) || stripTrailingSlash(currentOrigin);
  if (!base) return path; // last resort — relative path will still work in-browser
  return base + path;
}

function stripTrailingSlash(s: string | undefined): string {
  if (!s) return "";
  return s.endsWith("/") ? s.slice(0, -1) : s;
}
