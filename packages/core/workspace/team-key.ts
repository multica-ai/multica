export const TEAM_KEY_REGEX = /^[A-Z][A-Z0-9]{0,6}$/;

export function normalizeTeamKey(value: string): string {
  return value.trim().toUpperCase();
}

export function defaultTeamKeyFromSlug(slug: string): string {
  const key = slug
    .trim()
    .toLowerCase()
    .replace(/[^a-z]/g, "")
    .slice(0, 3)
    .toUpperCase();
  return key || "T";
}
