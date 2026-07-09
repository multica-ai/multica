export const SPACE_KEY_REGEX = /^[A-Z][A-Z0-9]{0,6}$/;

// Mirrors reservedSpaceKeys in server/internal/handler/workspace.go: keys
// that would collide with a static route under /space/{key} (e.g. the
// create-space page at /space/new).
export const RESERVED_SPACE_KEYS = new Set(["NEW"]);

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

export function isValidSpaceKey(key: string): boolean {
  return SPACE_KEY_REGEX.test(key) && !RESERVED_SPACE_KEYS.has(key);
}

// Interactive input sanitizer for the create/edit key field: uppercase, drop
// characters outside [A-Z0-9], truncate to 7. Unlike normalizeSpaceKey it does
// NOT force a leading "T" onto digit-first input — the create form surfaces a
// "must start with a letter" error instead of silently mangling what the user
// typed. normalizeSpaceKey keeps the T-coercion for the migration/legacy
// mirror, which must always emit a CHECK-valid key from arbitrary legacy data.
export function sanitizeSpaceKeyInput(value: string): string {
  return value
    .trim()
    .toUpperCase()
    .replace(/[^A-Z0-9]/g, "")
    .slice(0, 7);
}
