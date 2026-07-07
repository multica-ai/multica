export const SPACE_KEY_REGEX = /^[A-Z][A-Z0-9]{0,6}$/;

// Mirrors the server-side normalizeSpaceKey (handler/workspace.go) and the
// migration backfill: uppercase, strip characters outside [A-Z0-9], truncate
// to 7, and prefix digit-leading keys with "T".
export function normalizeSpaceKey(value: string): string {
  let key = value
    .trim()
    .toUpperCase()
    .replace(/[^A-Z0-9]/g, "")
    .slice(0, 7);
  if (/^[0-9]/.test(key)) {
    key = `T${key}`.slice(0, 7);
  }
  return key;
}
