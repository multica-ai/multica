export const WORKSPACE_SLUG_REGEX = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

export const WORKSPACE_SLUG_FORMAT_ERROR =
  "Only lowercase letters, numbers, and hyphens";

export const WORKSPACE_SLUG_CONFLICT_ERROR =
  "That workspace URL is already taken.";

export function nameToWorkspaceSlug(name: string): string {
  return (
    name
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-|-$/g, "") || "workspace"
  );
}

export function isWorkspaceSlugConflict(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    "status" in error &&
    (error as { status?: unknown }).status === 409
  );
}
