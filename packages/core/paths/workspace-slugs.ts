import { isReservedSlug } from "./reserved-slugs";
import { WORKSPACE_PAGES } from "./route-icons";

type WorkspaceSlugLookup = (slug: string) => boolean;

const noWorkspaces: WorkspaceSlugLookup = () => false;
let lookup: WorkspaceSlugLookup = noWorkspaces;

/**
 * Bind the account's workspace membership lookup without copying server
 * state. Returns an unbind that only resets the lookup if it is still the one
 * installed here, so a stale owner can never clear a newer binding.
 */
export function setWorkspaceSlugLookup(next: WorkspaceSlugLookup): () => void {
  lookup = next;
  return () => {
    if (lookup === next) lookup = noWorkspaces;
  };
}

const workspaceRoutes = new Set([
  ...Object.values(WORKSPACE_PAGES).map((page) => page.segment),
  "members",
  "attachments",
]);

function splitWorkspacePath(path: string): { slug: string; route?: string } | null {
  const pathname = path.split(/[?#]/, 1)[0] ?? "";
  const [rawSlug, route] = pathname.split("/").filter(Boolean);
  if (!rawSlug) return null;
  try {
    return { slug: decodeURIComponent(rawSlug), route };
  } catch {
    return null;
  }
}

/**
 * Reservation governs creation, not existing workspace membership. For a
 * reserved slug, require a workspace route too so /dingtalk/bind and other
 * global endpoints remain global. Saved groups may supply their own slug
 * until the shell validates them against the loaded membership list.
 */
export function workspaceSlugForPath(path: string, savedGroupSlug?: string): string | null {
  const parts = splitWorkspacePath(path);
  if (!parts) return null;
  const { slug, route } = parts;
  if (isReservedSlug(slug.toLowerCase())) {
    if (slug !== savedGroupSlug && !lookup(slug)) return null;
    if (!route || !workspaceRoutes.has(route)) return null;
  }
  return slug;
}

/**
 * Report whether a path is `/{slug}/{workspace-route}/...` for a workspace the
 * current account actually belongs to. Unlike `workspaceSlugForPath`, an
 * arbitrary non-reserved first segment does not qualify — callers use this to
 * tell a real workspace URL apart from a legacy unscoped one such as `/chat`.
 */
export function isKnownWorkspacePath(path: string): boolean {
  const parts = splitWorkspacePath(path);
  if (!parts?.route || !workspaceRoutes.has(parts.route)) return false;
  return lookup(parts.slug);
}
