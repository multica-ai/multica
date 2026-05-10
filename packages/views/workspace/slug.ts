export const WORKSPACE_SLUG_REGEX = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

/**
 * Auto-generate a slug from a workspace name.
 *
 * Returns empty string when the name produces no valid characters (e.g.
 * Chinese / Japanese / emoji-only names). The form leaves the slug field
 * empty in this case and the user must type one — this is preferable to a
 * hardcoded fallback like "workspace" which (a) silently chooses a useless
 * URL slug and (b) causes 409 conflicts for the second non-ASCII-named
 * workspace on the same instance.
 */
export function nameToWorkspaceSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

export function isWorkspaceSlugConflict(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    "status" in error &&
    (error as { status?: unknown }).status === 409
  );
}
