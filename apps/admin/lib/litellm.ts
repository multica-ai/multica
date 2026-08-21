import "server-only";
import { parseWithFallback } from "./schema";
import {
  LiteLlmKeyListSchema,
  LiteLlmTeamListSchema,
  type LiteLlmKey,
  type LiteLlmTeam,
} from "./litellm-schema";

/**
 * LiteLLM admin API client. Modeled directly on
 * ~/WORK/ai-spend-dashboard/app/lib/litellm.ts: same env vars, same
 * Authorization/User-Agent headers, same `litellmConfigured()` guard so
 * callers can degrade to an empty/"not linked" state instead of crashing
 * when the proxy isn't reachable or configured (this app ships without it
 * wired in most local dev setups).
 *
 * Auth: Authorization: Bearer ${LLMPROXY_MASTER_KEY} (master key or a key
 * with the get_spend_routes permission). Host: ${LLMPROXY_BASE_URL}, e.g.
 * https://llmproxy.g2.com/v1.
 */

// LLMPROXY_BASE_URL carries a `/v1` suffix for OpenAI-compatible routes
// (chat/completions etc.), but the admin API (/key/*, /team/*) used here is
// mounted at the proxy root, not under /v1 — strip it so apiGet() doesn't
// 404 every call.
const BASE = (process.env.LLMPROXY_BASE_URL || "").replace(/\/$/, "").replace(/\/v1$/, "");
const KEY = process.env.LLMPROXY_MASTER_KEY || "";

export function litellmConfigured(): boolean {
  return Boolean(BASE && KEY);
}

async function apiGet(path: string, params: Record<string, string> = {}): Promise<unknown> {
  const qs = new URLSearchParams(params).toString();
  const url = qs ? `${BASE}${path}?${qs}` : `${BASE}${path}`;
  const r = await fetch(url, {
    headers: {
      Authorization: `Bearer ${KEY}`,
      "User-Agent": "multica-admin/1.0",
    },
    // Key/team aliases and cost data change slowly; a short revalidate
    // window keeps the detail panel snappy without hammering the proxy.
    next: { revalidate: 300 },
  });
  if (!r.ok) throw new Error(`LiteLLM ${path} -> ${r.status}`);
  return r.json();
}

// This LiteLLM deployment caps /key/list's `size` at 100 (larger values 422).
const KEY_PAGE_SIZE = 100;
const MAX_KEY_PAGES = 200; // hard backstop — 20k keys is far beyond any real proxy

/**
 * Fetches all LiteLLM keys, paginating on `metadata.total_pages` (same
 * convention as ai-spend-dashboard's fetchRows). Best-effort end to end:
 * a malformed page is dropped via parseWithFallback (never fabricated), and
 * a network/HTTP failure on any page — including the first — degrades to
 * whatever pages were already collected (or `[]`) instead of throwing, since
 * callers (attachLiteLlmToList / findKeyForSlug) must be able to render
 * "Not linked" rather than 500 the whole list when the proxy is flaky.
 *
 * Filtered server-side to agentfarm- keys via key_alias + substring_matching,
 * which LiteLLM only honors for proxy_admin-role callers (LLMPROXY_MASTER_KEY
 * is the proxy master key) — a narrower key would silently get zero results.
 */
export async function listLiteLlmKeys(): Promise<LiteLlmKey[]> {
  if (!litellmConfigured()) return [];
  const keys: LiteLlmKey[] = [];
  for (let page = 1; page <= MAX_KEY_PAGES; page++) {
    let raw: unknown;
    try {
      raw = await apiGet("/key/list", {
        page: String(page),
        size: String(KEY_PAGE_SIZE),
        key_alias: "agentfarm-",
        substring_matching: "true",
        // Without this, /key/list returns `keys` as an array of hashed
        // token strings rather than objects — key_alias/metadata would be
        // unreadable and every parseWithFallback would drop the page.
        return_full_object: "true",
      });
    } catch (error) {
      console.error(`[admin] LiteLLM /key/list page ${page} failed`, error);
      break;
    }
    const parsed = parseWithFallback(raw, LiteLlmKeyListSchema, { keys: [] }, {
      endpoint: "/key/list",
    });
    keys.push(...parsed.keys);
    const totalPages = parsed.metadata?.total_pages ?? 1;
    if (page >= totalPages) break;
  }
  return keys;
}

// This LiteLLM deployment caps list endpoints' `size` at 100 (larger values 422).
const TEAM_PAGE_SIZE = 100;
const MAX_TEAM_PAGES = 200; // hard backstop, mirrors MAX_KEY_PAGES

/**
 * Fetches all teams so `team_id` (the only team identifier a key actually
 * carries — see lib/litellm-match.ts) can be resolved to `team_alias`, the
 * org's real human-assigned team name (e.g. "Digital Acquisition"). Same
 * best-effort/degrade-to-partial semantics as listLiteLlmKeys.
 */
export async function listLiteLlmTeams(): Promise<LiteLlmTeam[]> {
  if (!litellmConfigured()) return [];
  const teams: LiteLlmTeam[] = [];
  for (let page = 1; page <= MAX_TEAM_PAGES; page++) {
    let raw: unknown;
    try {
      raw = await apiGet("/team/list", {
        page: String(page),
        size: String(TEAM_PAGE_SIZE),
      });
    } catch (error) {
      console.error(`[admin] LiteLLM /team/list page ${page} failed`, error);
      break;
    }
    const parsed = parseWithFallback(raw, LiteLlmTeamListSchema, [], {
      endpoint: "/team/list",
    });
    const pageTeams = Array.isArray(parsed) ? parsed : parsed.teams;
    if (pageTeams.length === 0) break;
    teams.push(...pageTeams);
    if (pageTeams.length < TEAM_PAGE_SIZE) break;
  }
  return teams;
}
