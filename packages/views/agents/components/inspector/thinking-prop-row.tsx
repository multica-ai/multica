"use client";

import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { Brain } from "lucide-react";
import type { RuntimeModel } from "@multica/core/types";
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
  /** Runtime provider type (e.g. "codex", "claude"). Used to decide whether an
   *  empty model can safely preview a default model's effort catalog. */
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
  const entry = pickModelEntry(models, model, provider);
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

/** Full-width counterpart used by the General settings form. */
export function ThinkingSettingField({
  label,
  runtimeId,
  runtimeOnline,
  provider,
  model,
  value,
  canEdit,
  showUnavailable = false,
  onChange,
}: {
  label: ReactNode;
  runtimeId: string | null;
  runtimeOnline: boolean;
  provider: string;
  model: string;
  value: string;
  canEdit: boolean;
  /** Keep the row visible with an explicit capability status. The settings
   *  page enables this so an offline runtime cannot make reasoning look
   *  implicitly configured; compact creation flows may keep hiding it. */
  showUnavailable?: boolean;
  onChange: (next: string) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const modelsQuery = useQuery(
    runtimeModelsOptions(runtimeOnline ? runtimeId : null),
  );
  const models = modelsQuery.data?.models ?? [];
  const entry = pickModelEntry(models, model, provider);
  const levels = entry?.thinking?.supported_levels ?? [];

  if (levels.length === 0 && !value) {
    if (!showUnavailable) return null;

    const status = !runtimeId
      ? t(($) => $.pickers.thinking_runtime_required)
      : !runtimeOnline
        ? t(($) => $.pickers.thinking_runtime_offline)
        : modelsQuery.isPending
          ? t(($) => $.pickers.thinking_discovering)
          : modelsQuery.isError || modelsQuery.data?.supported === false
            ? t(($) => $.pickers.thinking_unavailable)
            : t(($) => $.pickers.thinking_unsupported);

    return (
      <SettingsRow label={label} size="select-wide">
        <div
          aria-disabled="true"
          className="flex min-h-10 w-full min-w-0 items-center gap-2 rounded-lg border border-input bg-input/50 px-3 text-body text-muted-foreground"
        >
          <Brain className="h-4 w-4 shrink-0" aria-hidden="true" />
          <span className="min-w-0 truncate">{status}</span>
        </div>
      </SettingsRow>
    );
  }

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

function pickModelEntry(
  models: RuntimeModel[],
  model: string,
  provider: string,
): RuntimeModel | undefined {
  if (model) return models.find((m) => m.id === model);
  // Empty model = "follow the runtime's own default". For codex that default
  // comes from the local config.toml and can be any installed model, so we
  // must NOT preview the flagged Default entry's effort catalog — gpt-5.6-sol
  // alone advertises `ultra`, which the actually-configured model may not
  // support. Fail closed (no preview): the row hides unless a stale level is
  // persisted, in which case it still renders so the orphan can be cleared.
  // Mirrors the backend ValidateThinkingLevel. (MUL-4347)
  if (provider === "codex") return undefined;
  return models.find((m) => m.default) ?? models[0];
}
