import type { RuntimeModel } from "@multica/core/types";
import { findModelCapabilityEntry } from "./model-capability";

/**
 * The exact per-model catalog for the agent's runtime, or `null` when it is not
 * authoritative — the runtime is offline, discovery is still in flight, or it
 * failed. `[]` means the provider answered and offers no models.
 */
export type ModelCatalog = readonly RuntimeModel[] | null;

/** Type alias (not an interface) so it stays assignable to the inspector's
 *  `Record<string, unknown>` update payload. */
export type ModelChangeUpdate = {
  model: string;
  thinking_level?: string;
  service_tier?: string;
};

/**
 * Builds the single PATCH body for a model change on the settings page.
 *
 * `thinking_level` and catalog service tiers are per-model capabilities:
 * keeping a value the new model never advertised leaves an orphan override
 * that the daemon silently drops at execution time, so the settings page would
 * claim a tier that never ran (MUL-5390). Explicit Standard is the exception:
 * its advertised capability belongs to the installed Codex CLI. Clearing
 * unconditionally is worse though — moving between two models that both
 * support Fast would throw the user's choice away, and clearing while the
 * catalog is unknown would delete a value the daemon would have honoured.
 *
 * So: clear only what the authoritative catalog says the new model does not
 * support. Unknown catalog, unknown model, or an empty model ("follow the
 * runtime default", which resolves from the daemon host's own config and cannot
 * be checked here) leave the stored values untouched — they stay visible in the
 * inspector so the user can clear them explicitly.
 *
 * Both clears travel with the model in ONE request: a second follow-up write
 * would leave a window where the agent is persisted with a model/tier
 * combination the UI never intended.
 */
export function buildModelChangeUpdate(input: {
  provider: string;
  model: string;
  thinkingLevel: string;
  serviceTier: string;
  catalog: ModelCatalog;
}): ModelChangeUpdate {
  const update: ModelChangeUpdate = { model: input.model };
  if (!input.thinkingLevel && !input.serviceTier) return update;
  if (input.catalog === null || !input.model) return update;

  const entry = findModelCapabilityEntry(
    input.catalog,
    input.model,
    input.provider,
  );
  if (!entry) return update;

  const supportsThinking = (entry.thinking?.supported_levels ?? []).some(
    (level) => level.value === input.thinkingLevel,
  );
  if (input.thinkingLevel && !supportsThinking) update.thinking_level = "";

  const supportsTier =
    (entry.service_tiers ?? []).some((tier) => tier.id === input.serviceTier) ||
    (input.serviceTier === "default" &&
      input.catalog.some(
        (candidate) =>
          candidate.supports_explicit_standard_service_tier === true,
      ));
  if (input.serviceTier && !supportsTier) update.service_tier = "";

  return update;
}
