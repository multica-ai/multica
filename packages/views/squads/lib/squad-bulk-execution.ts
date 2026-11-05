import type { RuntimeModel, RuntimeModelThinkingLevel } from "@multica/core/types";
import { findModelCapabilityEntry } from "../../agents/components/inspector/model-capability";

/**
 * The value every row shares, or `null` when rows differ (the squad header then
 * shows "Custom"). No rows means nothing is shared.
 */
export function sharedValue(values: readonly string[]): string | null {
  const [first] = values;
  if (first === undefined) return null;
  return values.every((v) => v === first) ? first : null;
}

/**
 * Models every runtime in use offers. A runtime whose catalog is not loaded
 * (offline, still discovering, failed) makes the whole list empty, since the
 * header cannot pick a model it cannot confirm everywhere.
 */
export function commonModels(catalogs: readonly (readonly RuntimeModel[] | undefined)[]): RuntimeModel[] {
  const loaded: (readonly RuntimeModel[])[] = [];
  for (const catalog of catalogs) {
    if (!catalog) return [];
    loaded.push(catalog);
  }
  const [first] = loaded;
  if (!first) return [];
  const otherIds = loaded.slice(1).map((c) => new Set(c.map((m) => m.id)));
  return first.filter((m) => otherIds.every((ids) => ids.has(m.id)));
}

/** The model's thinking levels for one agent, or none when the model is unknown or the agent follows the CLI default. */
export function thinkingLevelsFor(
  catalog: readonly RuntimeModel[] | undefined,
  model: string,
  provider: string,
): RuntimeModelThinkingLevel[] {
  if (!catalog || !model) return [];
  return findModelCapabilityEntry(catalog, model, provider)?.thinking?.supported_levels ?? [];
}

/** Levels every agent's model supports, matched by token. Empty when any agent has none. */
export function commonThinkingLevels(
  lists: readonly (readonly RuntimeModelThinkingLevel[])[],
): RuntimeModelThinkingLevel[] {
  if (lists.some((l) => l.length === 0)) return [];
  const [first, ...rest] = lists;
  if (!first) return [];
  return first.filter((level) => rest.every((list) => list.some((x) => x.value === level.value)));
}

/**
 * Waits for every agent's write and counts the failures. Each agent saves on
 * its own, so one failure does not stop the others and the caller reports the
 * count.
 */
export async function settleAgentWrites(
  writes: readonly Promise<unknown>[],
): Promise<{ total: number; failed: number }> {
  const results = await Promise.allSettled(writes);
  return { total: results.length, failed: results.filter((r) => r.status === "rejected").length };
}
