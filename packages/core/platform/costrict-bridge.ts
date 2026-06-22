// CoStrict embedding bridge.
//
// multica can run standalone (web / desktop) or embedded as an iframe inside
// the costrict-web platform. When embedded, the parent injects
// `window.desktopAPI.coStrictToken` for auth and loads multica with
// `?embedded=opencode`. The parent also listens for `multica:navigate`
// postMessages so the embedded app can drive parent-window navigation.
//
// This module centralizes the embed check and the navigate contract so the
// rest of the app never pokes at `window.parent` / query params directly.

interface CoStrictWindow {
  desktopAPI?: { coStrictToken?: string };
}

/**
 * True when multica is running embedded inside costrict-web. Detected via the
 * injected coStrict token (the reliable auth signal) OR the `embedded` query
 * param the parent appends to the iframe src. Either alone is sufficient.
 */
export function isEmbeddedInCostrict(): boolean {
  if (typeof window === "undefined") return false;
  const w = window as unknown as CoStrictWindow;
  if (w.desktopAPI?.coStrictToken) return true;
  try {
    const embedded = new URLSearchParams(window.location.search).get("embedded");
    if (embedded) return true;
  } catch {
    // location/search unavailable — fall through to false.
  }
  // Embedded iframes have a distinct parent frame.
  return window.parent !== window;
}

/** Message multica posts to the costrict-web parent to open a csc session. */
export interface CostrictNavigateSessionMessage {
  type: "multica:navigate";
  target: "session";
  sessionId: string;
  /**
   * Working directory the session ran in, when known. Best-effort only — the
   * parent opens the session by id and uses workDir merely as a hint to pick
   * the landing workspace, so an empty value is fine.
   */
  workDir?: string;
}

/**
 * Ask the costrict-web parent to open a csc conversation session. The parent
 * opens the session by `sessionId` (landing in the owning or default
 * workspace) — `workDir` is an optional hint. No-op when not embedded or when
 * `sessionId` is missing.
 */
export function postCostrictNavigateToSession(args: {
  sessionId: string;
  workDir?: string;
}): void {
  if (typeof window === "undefined") return;
  if (!args.sessionId) return;
  if (window.parent === window) return;
  const message: CostrictNavigateSessionMessage = {
    type: "multica:navigate",
    target: "session",
    sessionId: args.sessionId,
    ...(args.workDir ? { workDir: args.workDir } : {}),
  };
  // Target origin "*" mirrors the existing parent contract; the parent
  // validates event.origin on its side.
  window.parent.postMessage(message, "*");
}
