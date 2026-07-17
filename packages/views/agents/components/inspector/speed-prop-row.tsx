"use client";

import { useQuery } from "@tanstack/react-query";
import { runtimeModelsOptions } from "@multica/core/runtimes";
import { PropRow } from "../../../common/prop-row";
import { useT } from "../../../i18n";

export function SpeedPropRow({
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
  onChange: (value: string) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const query = useQuery(runtimeModelsOptions(runtimeOnline ? runtimeId : null));
  const models = query.data?.models ?? [];
  const selected = model
    ? models.find((entry) => entry.id === model)
    : models.find((entry) => entry.default) ?? models[0];
  const levels = selected?.speed?.supported_levels ?? [];
  if (levels.length === 0 && !value) return null;
  const displayValue = value || "standard";

  return (
    <PropRow label={t(($) => $.inspector.prop_speed)} interactive={false}>
      {canEdit && levels.length > 0 ? (
        <select
          aria-label={t(($) => $.runtime_settings.speed)}
          value={displayValue}
          onChange={(event) => void onChange(event.target.value)}
          className="max-w-28 rounded border border-border bg-background px-1.5 py-0.5 font-mono text-[11px]"
        >
          {levels.map((level) => <option key={level.value} value={level.value}>{level.label}</option>)}
        </select>
      ) : (
        <span className="font-mono text-[11px] text-muted-foreground">{displayValue}</span>
      )}
    </PropRow>
  );
}
