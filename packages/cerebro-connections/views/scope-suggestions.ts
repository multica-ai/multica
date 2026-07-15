import type { ScopableArg } from "./types";

function scopeKey(scope: ScopableArg): string {
  return `${scope.tool.trim()}\u0000${scope.arg.trim()}`;
}

export function pendingScopeSuggestions(
  current: ScopableArg[],
  discovered: ScopableArg[],
): ScopableArg[] {
  const currentKeys = new Set(current.map(scopeKey));
  return discovered.filter((scope) => !currentKeys.has(scopeKey(scope)));
}

export function acceptScopeSuggestion(
  current: ScopableArg[],
  suggestion: ScopableArg,
): ScopableArg[] {
  return pendingScopeSuggestions(current, [suggestion]).length === 0
    ? current
    : [...current, suggestion];
}
