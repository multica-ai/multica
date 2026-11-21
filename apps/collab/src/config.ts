/** Each live doc lives in this process's memory, so the service runs as one
 *  replica: a second one would hold its own copy of the same doc. Scaling out
 *  needs Hocuspocus's Redis extension or routing by doc id first. */
export interface CollabConfig {
  port: number;
  /** Shared with the Go API: signs editor tickets and authenticates the
   *  internal calls in both directions. */
  secret: string;
  /** Base URL of the Go API's internal collaboration endpoints. */
  apiUrl: string;
  cdnDomain: string;
  /** Quiet period before a changed document is written back. Every store
   *  rewrites the doc, its version and a revision in Postgres. Agents' reads
   *  and every API write flush the live doc first, so they never wait on it. */
  storeDebounceMs: number;
  /** Longest a continuously edited document goes unsaved. */
  storeMaxDebounceMs: number;
}

export function loadConfig(env: NodeJS.ProcessEnv = process.env): CollabConfig {
  const secret = env.COLLAB_SECRET?.trim() ?? "";
  const apiUrl = env.MULTICA_API_URL?.trim().replace(/\/+$/, "") ?? "";
  if (!secret) throw new Error("COLLAB_SECRET is required");
  if (!apiUrl) throw new Error("MULTICA_API_URL is required");
  return {
    port: Number(env.COLLAB_PORT || 8095),
    secret,
    apiUrl,
    cdnDomain: env.MULTICA_CDN_DOMAIN?.trim() ?? "",
    storeDebounceMs: Number(env.COLLAB_STORE_DEBOUNCE_MS || 5000),
    storeMaxDebounceMs: Number(env.COLLAB_STORE_MAX_DEBOUNCE_MS || 30000),
  };
}
