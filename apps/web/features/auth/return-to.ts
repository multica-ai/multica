import { sanitizeNextUrl } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { isLegacyWorkspacePath } from "@/lib/legacy-workspace-path";

const GOOGLE_RETURN_KEY = "multica.google-return-to";

export function returnToCurrentPage(destination = paths.login()): string {
  const { pathname, search, hash } = window.location;
  return `${destination}?next=${encodeURIComponent(pathname + search + hash)}`;
}

export function readLoginReturnTo(raw: string | null): string | null {
  const next = sanitizeNextUrl(raw);
  if (!next || !isLegacyWorkspacePath(next) || next.includes("#")) return next;
  // Proxy can capture a legacy URL's path and query. The browser carries its
  // fragment separately on the HTTP redirect to /login.
  return typeof window === "undefined" ? next : next + window.location.hash;
}

export function resolveReturnTo(
  next: string | null,
  workspaces: readonly { slug: string }[],
): string | null {
  if (!next || !isLegacyWorkspacePath(next)) return next;
  const first = workspaces[0];
  return first ? `/${encodeURIComponent(first.slug)}${next}` : null;
}

// OAuth state crosses origins. Keep the destination, including its query and
// fragment, in this tab and send only a reference to Google.
export function rememberGoogleReturnTo(next: string): string | undefined {
  try {
    const words = crypto.getRandomValues(new Uint32Array(4));
    const id = Array.from(words, (word) => word.toString(16).padStart(8, "0")).join("");
    sessionStorage.setItem(GOOGLE_RETURN_KEY, `${id}:${next}`);
    return id;
  } catch {
    return undefined;
  }
}

export function takeGoogleReturnTo(id: string): string | null {
  if (!/^[a-f0-9]{32}$/.test(id)) return null;
  try {
    const stored = sessionStorage.getItem(GOOGLE_RETURN_KEY);
    if (!stored?.startsWith(`${id}:`)) return null;
    sessionStorage.removeItem(GOOGLE_RETURN_KEY);
    return sanitizeNextUrl(stored.slice(id.length + 1));
  } catch {
    return null;
  }
}
