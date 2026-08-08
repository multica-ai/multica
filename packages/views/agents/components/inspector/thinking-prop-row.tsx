"use client";

import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type {
  RuntimeModel,
  RuntimeModelThinkingLevel,
} from "@multica/core/types";
import { runtimeModelsOptions } from "@multica/core/runtimes";
import { PropRow } from "../../../common/prop-row";
import { SettingsRow } from "../../../settings/components/settings-layout";
import { useT } from "../../../i18n";
import { ThinkingPicker } from "./thinking-picker";

/**
 * Thinking row for the agent inspector. Hidden when the active model has
 * no `supported_levels` advertised AND nothing is persisted, so providers
 * that don't expose reasoning never surface an empty row. If the agent
 * already has a `thinking_level` saved (model swap into a non-thinking
 * runtime, or the daemon / CLI catalog shrank and dropped the entry),
 * we still render the row so the user can see the orphan token the
 * backend is still sending and explicit-clear it via the picker footer.
 * PR1's per-model invalid behavior is daemon-side warn/drop, not a
 * synchronous DB clear, so the frontend has to surface the persisted
 * state honestly.
 *
 * Reuses the shared runtime-models query so it hits the same 60s cache
 * as the model picker; no extra round-trip on the inspector's hot path.
 * The sibling ModelPicker mounts unconditionally next to this row, so
 * the shared query subscription is established by the inspector mount
 * itself — returning null here does NOT cancel discovery.
 */
export function ThinkingPropRow({
  runtimeId,
  runtimeOnline,
  provider,
  model,
  value,
  canEdit,
  onChange,
}: {
  runtimeId: string | null;
  runtimeOnline: boolean;
  /** Runtime provider type (e.g. "codex", "claude"). Decides how an empty
   *  model resolves to a level set — see `pickLevels`. */
  provider: string;
  model: string;
  value: string;
  canEdit: boolean;
  onChange: (next: string) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const modelsQuery = useQuery(
    runtimeModelsOptions(runtimeOnline ? runtimeId : null),
  );

  const models = modelsQuery.data?.models ?? [];
  const levels = pickLevels(models, model, provider);
  if (levels.length === 0 && !value) return null;

  return (
    <PropRow label={t(($) => $.inspector.prop_thinking)} interactive={false}>
      <ThinkingPicker
        value={value}
        levels={levels}
        canEdit={canEdit}
        onChange={onChange}
      />
    </PropRow>
  );
}

/** Full-width counterpart used by the General settings form. */
export function ThinkingSettingField({
  label,
  runtimeId,
  runtimeOnline,
  provider,
  model,
  value,
  canEdit,
  onChange,
}: {
  label: ReactNode;
  runtimeId: string | null;
  runtimeOnline: boolean;
  provider: string;
  model: string;
  value: string;
  canEdit: boolean;
  onChange: (next: string) => Promise<void> | void;
}) {
  const modelsQuery = useQuery(
    runtimeModelsOptions(runtimeOnline ? runtimeId : null),
  );
  const models = modelsQuery.data?.models ?? [];
  const levels = pickLevels(models, model, provider);

  if (levels.length === 0 && !value) return null;

  return (
    <SettingsRow label={label} size="select-wide">
      <ThinkingPicker
        variant="field"
        showLabel={false}
        value={value}
        levels={levels}
        canEdit={canEdit}
        onChange={onChange}
      />
    </SettingsRow>
  );
}

// Effort levels to offer for a (model, provider) pair. Mirrors the backend
// ValidateThinkingLevel so the picker never offers a level the daemon would
// drop, and never hides one it would accept.
function pickLevels(
  models: RuntimeModel[],
  model: string,
  provider: string,
): RuntimeModelThinkingLevel[] {
  if (model) {
    return (
      models.find((m) => m.id === model)?.thinking?.supported_levels ?? []
    );
  }
  // Empty model = "follow the runtime's own default". For codex that default
  // comes from the local config.toml and can be any installed model, so we
  // must NOT preview the flagged Default entry's effort catalog — gpt-5.6-sol
  // alone advertises `ultra`, which the actually-configured model may not
  // support. Fail closed (no preview): the row hides unless a stale level is
  // persisted, in which case it still renders so the orphan can be cleared.
  // (MUL-4347)
  if (provider === "codex") return [];
  // claude / opencode resolve an unset model from local configuration we
  // can't read, so no single entry can stand in for it. Offer the union of
  // what the runtime advertises and let the CLI arbitrate — picking one
  // entry here would hide `xhigh` from a user whose CLI is set to Opus.
  return unionLevels(models);
}

// Distinct levels across every advertised model, in first-seen catalog order
// so the runtime's own low→high ordering survives.
function unionLevels(models: RuntimeModel[]): RuntimeModelThinkingLevel[] {
  const seen = new Set<string>();
  const out: RuntimeModelThinkingLevel[] = [];
  for (const m of models) {
    for (const level of m.thinking?.supported_levels ?? []) {
      if (seen.has(level.value)) continue;
      seen.add(level.value);
      out.push(level);
    }
  }
  return out;
}
