/**
 * Validate a post-login redirect URL and return it only if safe to follow.
 *
 * Only single-slash relative paths (e.g. `/invite/abc`) are accepted. Returns
 * `null` for unsafe or empty input — call sites decide the fallback so this
 * helper never overloads a specific path with "user did not pass next".
 *
 * Rejects:
 *   - `null` / empty string
 *   - absolute URLs (`https://evil.com`, `javascript:alert(1)`, …)
 *   - protocol-relative URLs (`//evil.com`)
 *   - paths containing backslashes (Windows-style or `/\\host`)
 *   - paths containing ASCII control characters (`\x00`–`\x1f`)
 */
export function sanitizeNextUrl(raw: string | null): string | null {
  if (!raw) return null;
  if (!raw.startsWith("/") || raw.startsWith("//")) return null;
  if (/[\x00-\x1f\\]/.test(raw)) return null;
  return raw;
}
