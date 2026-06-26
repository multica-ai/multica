import type {
  RuntimeModel,
  RuntimeModelThinkingLevel,
} from "@multica/core/types";

// Shared, framework-free reasoning-level resolution. Both the inspector's
// ThinkingPropRow and the create-dialog ReasoningPicker derive the level set
// from the discovered model catalog the same way, so the logic lives here
// once (no fork). MUL-2339 / MUL-3772.

/**
 * Resolve the catalog entry for the active model. When `model` is empty
 * (the agent runs the runtime's default), fall back to the catalog's
 * `default` flag, then the first discovered model — matching how the model
 * picker presents the implicit default.
 */
export function pickModelEntry(
  models: RuntimeModel[],
  model: string,
): RuntimeModel | undefined {
  if (model) return models.find((m) => m.id === model);
  return models.find((m) => m.default) ?? models[0];
}

/**
 * The reasoning/effort levels the active (runtime, model) pair exposes.
 * Empty when the model advertises no `thinking` catalog — the caller treats
 * that as "no reasoning picker for this model".
 */
export function resolveThinkingLevels(
  models: RuntimeModel[],
  model: string,
): RuntimeModelThinkingLevel[] {
  return pickModelEntry(models, model)?.thinking?.supported_levels ?? [];
}
