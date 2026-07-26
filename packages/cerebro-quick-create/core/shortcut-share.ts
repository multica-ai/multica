export interface ShortcutShareDraft {
  title: string;
  text: string;
  url: string;
  workspaceId: string;
  projectId: string;
  autoSubmit: boolean;
}

const MAX_TITLE_LENGTH = 500;
const MAX_TEXT_LENGTH = 6_000;
const MAX_URL_LENGTH = 2_000;

/**
 * Parses an iOS Shortcut payload from the URL fragment.
 *
 * The fragment never reaches Cloudflare or the web server. Multica reads it
 * locally after the normal signed-in page opens, then creates the issue through
 * the same authenticated API as the in-app create dialog.
 */
export function parseShortcutShareHash(hash: string): ShortcutShareDraft | null {
  const raw = hash.startsWith("#") ? hash.slice(1) : hash;
  if (!raw) return null;

  const params = new URLSearchParams(raw);
  if (params.get("shortcut") !== "1") return null;

  const workspaceId = params.get("workspace_id")?.trim() ?? "";
  const projectId = params.get("project_id")?.trim() ?? "";
  const title = params.get("title")?.trim() ?? "";
  const text = params.get("text")?.trim() ?? "";
  const url = params.get("url")?.trim() ?? "";

  if (!workspaceId || !projectId || (!title && !text && !url)) return null;
  if (
    title.length > MAX_TITLE_LENGTH ||
    text.length > MAX_TEXT_LENGTH ||
    url.length > MAX_URL_LENGTH
  ) {
    return null;
  }

  return {
    title,
    text,
    url,
    workspaceId,
    projectId,
    autoSubmit: params.get("submit") === "1",
  };
}
