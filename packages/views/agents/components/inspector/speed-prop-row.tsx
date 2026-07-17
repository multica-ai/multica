"use client";

import { useQuery } from "@tanstack/react-query";
import { runtimeModelsOptions } from "@multica/core/runtimes";
import { PropRow } from "../../../common/prop-row";
import { useT } from "../../../i18n";
// CEREBRO-PATCH(agent-runtime-setting-search-select): match the Model property picker interaction.
import { RuntimeSettingSearchSelect } from "@multica/cerebro-ui/components/runtime-setting-search-select";

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
        <RuntimeSettingSearchSelect
          variant="property"
          ariaLabel={t(($) => $.runtime_settings.speed)}
          value={displayValue}
          options={levels}
          searchPlaceholder={t(($) => $.runtime_settings.search_speed)}
          onChange={onChange}
        />
      ) : (
        <span className="font-mono text-[11px] text-muted-foreground">{displayValue}</span>
      )}
    </PropRow>
  );
}
