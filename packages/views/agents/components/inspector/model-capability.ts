import type { RuntimeModel } from "@multica/core/types";

// Claude Code appends a context-window modifier to some runtime-native model
// IDs (for example, claude-opus-5[1m]). Restrict inheritance to a numeric
// context size so arbitrary bracketed variants remain fail-closed.
const CLAUDE_CONTEXT_WINDOW_TAG = /\[[1-9]\d*[km]\]$/;

export function modelIdForCapabilityLookup(
  provider: string,
  model: string,
): string {
  return provider === "claude"
    ? model.replace(CLAUDE_CONTEXT_WINDOW_TAG, "")
    : model;
}

/**
 * Resolves the catalog entry used for capability display and cleanup. The raw
 * model remains the value persisted and sent to the runtime; only this lookup
 * identity is normalized.
 */
export function findModelCapabilityEntry(
  models: readonly RuntimeModel[],
  model: string,
  provider: string,
): RuntimeModel | undefined {
  if (!model) return undefined;
  const lookupId = modelIdForCapabilityLookup(provider, model);
  return models.find((entry) => entry.id === lookupId);
}
