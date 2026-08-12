import type { SupportedLocale } from "@multica/core/i18n";
import { CELESTIAL_WORKSPACE_NAMES } from "./celestial-workspace-names";

export const WORKSPACE_SLUG_REGEX = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

const WORKSPACE_SLUG_SUFFIX_ALPHABET = "abcdefghijklmnopqrstuvwxyz0123456789";
const WORKSPACE_SLUG_SUFFIX_LENGTH = 4;

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

/**
 * Generates a localized workspace name while keeping its URL based on the
 * stable English slug. The server's unique slug constraint remains the final
 * authority if two users ever generate the same candidate.
 */
export function randomCelestialWorkspaceIdentity(
  locale: SupportedLocale,
  random: () => number = Math.random,
): { name: string; slug: string } {
  const celestial =
    CELESTIAL_WORKSPACE_NAMES[
      Math.floor(random() * CELESTIAL_WORKSPACE_NAMES.length)
    ]!;
  let suffix = "";

  for (let i = 0; i < WORKSPACE_SLUG_SUFFIX_LENGTH; i += 1) {
    suffix +=
      WORKSPACE_SLUG_SUFFIX_ALPHABET[
        Math.floor(random() * WORKSPACE_SLUG_SUFFIX_ALPHABET.length)
      ]!;
  }

  return {
    name: celestial.names[locale],
    slug: `${celestial.slugBase}-${suffix}`,
  };
}

export function isWorkspaceSlugConflict(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    "status" in error &&
    (error as { status?: unknown }).status === 409
  );
}
