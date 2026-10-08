/**
 * Append missing fallback directories onto a PATH string without prepending.
 * Prepending (e.g. /usr/local/bin before nvm) shadows a recovered login-shell
 * Node with a stale system binary and breaks shebang CLIs during daemon probes.
 */
export function appendMissingPathDirs(
  currentPath: string,
  fallbackDirs: readonly string[],
  separator = ":",
): string {
  const current = currentPath.split(separator).filter(Boolean);
  const existing = new Set(current);
  const missing = fallbackDirs.filter((p) => !existing.has(p));
  return [...current, ...missing].join(separator);
}
