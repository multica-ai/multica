"use client";

import { useQuery } from "@tanstack/react-query";
import type { RuntimeModel } from "@multica/core/types";
import { runtimeModelsOptions } from "@multica/core/runtimes";
import { PropRow } from "../../../common/prop-row";
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
  model,
  value,
  canEdit,
  onChange,
}: {
  runtimeId: string | null;
  runtimeOnline: boolean;
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
  const entry = pickModelEntry(models, model);
  const levels = entry?.thinking?.supported_levels ?? [];
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

function pickModelEntry(
  models: RuntimeModel[],
  model: string,
): RuntimeModel | undefined {
  if (model) return models.find((m) => m.id === model);
  return models.find((m) => m.default) ?? models[0];
}
