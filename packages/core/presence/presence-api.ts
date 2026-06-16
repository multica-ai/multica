// CEREBRO-PATCH(member-presence-core): moved here from
// cerebro-inbox-slack-block so member presence (online/offline) can be read by
// any layer — including @multica/views (which the slack-block package depends
// on, so it cannot live there without a dependency cycle). The slack-block
// keeps working via a thin re-export. See TECH-3583.
//
// REST client for the cerebro presence + typing endpoints. These endpoints
// are not on the typed shared ApiClient, so we go through the same low-level
// primitives the shared client exposes publicly: `api.getBaseUrl()` and
// `api.getToken()`. Auth/workspace/CSRF headers are assembled here to mirror
// the shared client's `authHeaders()` exactly (Authorization bearer,
// X-Workspace-Slug, X-CSRF-Token) so requests authenticate identically.
//
// Parse-don't-cast: presence responses run through a lenient zod schema via
// `parseWithFallback`, never a bare cast — see CLAUDE.md "API Response
// Compatibility".
import { z } from "zod";
import { api, parseWithFallback } from "../api";
import { getCurrentSlug } from "../platform";

// Lenient schema: `online_user_ids` is the contract field. Kept permissive so
// an unexpected extra field never fails the parse and white-screens callers.
const presenceResponseSchema = z.object({
  online_user_ids: z.array(z.string()),
});

export interface PresenceResponse {
  online_user_ids: string[];
}

const PRESENCE_FALLBACK: PresenceResponse = { online_user_ids: [] };

/** Reads the CSRF token the server set as a cookie. Mirrors the shared
 *  ApiClient's private `readCsrfToken()`; SSR-guarded. */
function readCsrfToken(): string | null {
  if (typeof document === "undefined") return null;
  const match = document.cookie
    .split("; ")
    .find((c) => c.startsWith("multica_csrf="));
  return match ? (match.split("=")[1] ?? null) : null;
}

/** Builds the standard auth headers the shared client sends on every request. */
function authHeaders(): Record<string, string> {
  const headers: Record<string, string> = {};
  const token = api.getToken();
  if (token) headers["Authorization"] = `Bearer ${token}`;
  const slug = getCurrentSlug();
  if (slug) headers["X-Workspace-Slug"] = slug;
  const csrf = readCsrfToken();
  if (csrf) headers["X-CSRF-Token"] = csrf;
  return headers;
}

function url(path: string): string {
  return `${api.getBaseUrl().replace(/\/$/, "")}${path}`;
}

/** GET /api/cerebro/presence — initial online snapshot. */
export async function fetchPresenceSnapshot(): Promise<PresenceResponse> {
  const res = await fetch(url("/api/cerebro/presence"), {
    method: "GET",
    headers: authHeaders(),
    credentials: "include",
  });
  if (!res.ok) return PRESENCE_FALLBACK;
  const data: unknown = await res.json().catch(() => null);
  return parseWithFallback(data, presenceResponseSchema, PRESENCE_FALLBACK, {
    endpoint: "GET /api/cerebro/presence",
  });
}

/** POST /api/cerebro/presence/ping — keep self "online"; returns the snapshot. */
export async function pingPresence(): Promise<PresenceResponse> {
  const res = await fetch(url("/api/cerebro/presence/ping"), {
    method: "POST",
    headers: { ...authHeaders(), "Content-Type": "application/json" },
    credentials: "include",
  });
  if (!res.ok) return PRESENCE_FALLBACK;
  const data: unknown = await res.json().catch(() => null);
  return parseWithFallback(data, presenceResponseSchema, PRESENCE_FALLBACK, {
    endpoint: "POST /api/cerebro/presence/ping",
  });
}

/** POST /api/cerebro/channels/{channelId}/typing — signal the user is typing.
 *  Server replies 204; we ignore the body. Failures are swallowed — a missed
 *  typing ping is never worth surfacing to the user. */
export async function postTyping(channelId: string): Promise<void> {
  try {
    await fetch(url(`/api/cerebro/channels/${channelId}/typing`), {
      method: "POST",
      headers: { ...authHeaders(), "Content-Type": "application/json" },
      credentials: "include",
    });
  } catch {
    // Best-effort: typing presence is non-critical.
  }
}
