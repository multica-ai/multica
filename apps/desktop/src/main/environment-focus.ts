export type FocusEnvironmentResult =
  | { ok: true }
  | { ok: false; reason: "gone" | "unsupported" | "unavailable" | "failed"; error?: string };

const FOCUS_TIMEOUT_MS = 15_000;
const REASONS = new Set(["gone", "unsupported", "unavailable", "failed"]);

/**
 * Asks the daemon on `port` to bring the desktop app it detected under `key`
 * forward. Node's fetch sends no Origin header, which is how the daemon tells
 * this call apart from a web page's.
 */
export async function focusEnvironmentAt(
  port: number,
  key: string,
  fetchImpl: typeof fetch = fetch,
): Promise<FocusEnvironmentResult> {
  let res: Response;
  try {
    res = await fetchImpl(`http://127.0.0.1:${port}/environments/focus`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ key }),
      signal: AbortSignal.timeout(FOCUS_TIMEOUT_MS),
    });
  } catch {
    return { ok: false, reason: "unavailable" };
  }
  if (res.ok) return { ok: true };
  let body: { reason?: unknown; error?: unknown } | null = null;
  try {
    body = (await res.json()) as { reason?: unknown; error?: unknown };
  } catch {
    body = null;
  }
  // A daemon older than this endpoint answers its mux's plain-text 404.
  if (!body || typeof body.reason !== "string") return { ok: false, reason: "unsupported" };
  const error = typeof body.error === "string" ? body.error : undefined;
  if (!REASONS.has(body.reason)) return { ok: false, reason: "failed", error };
  return { ok: false, reason: body.reason as "gone" | "unsupported" | "unavailable" | "failed", error };
}
