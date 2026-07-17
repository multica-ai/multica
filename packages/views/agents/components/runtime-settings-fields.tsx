"use client";

import { useEffect, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { runtimeModelsOptions } from "@multica/core/runtimes";
import { Label } from "@multica/ui/components/ui/label";
import { useT } from "../../i18n";

export function RuntimeSettingsFields({
  runtimeId,
  runtimeOnline,
  model,
  thinkingLevel,
  speedMode,
  onThinkingChange,
  onSpeedChange,
}: {
  runtimeId: string | null;
  runtimeOnline: boolean;
  model: string;
  thinkingLevel: string;
  speedMode: string;
  onThinkingChange: (value: string) => void;
  onSpeedChange: (value: string) => void;
}) {
  const { t } = useT("agents");
  const modelsQuery = useQuery(runtimeModelsOptions(runtimeOnline ? runtimeId : null));
  const models = useMemo(() => modelsQuery.data?.models ?? [], [modelsQuery.data]);
  const selected = model
    ? models.find((entry) => entry.id === model)
    : models.find((entry) => entry.default) ?? models[0];
  const thinkingLevels = useMemo(
    () => selected?.thinking?.supported_levels ?? [],
    [selected],
  );
  const speedLevels = useMemo(
    () => selected?.speed?.supported_levels ?? [],
    [selected],
  );

  useEffect(() => {
    if (!modelsQuery.data) return;
    if (thinkingLevel && !thinkingLevels.some((level) => level.value === thinkingLevel)) {
      onThinkingChange("");
    }
  }, [modelsQuery.data, thinkingLevel, thinkingLevels, onThinkingChange]);

  useEffect(() => {
    if (!modelsQuery.data) return;
    if (speedMode && !speedLevels.some((level) => level.value === speedMode)) {
      onSpeedChange("");
    }
  }, [modelsQuery.data, speedMode, speedLevels, onSpeedChange]);

  if (thinkingLevels.length === 0 && speedLevels.length === 0) return null;

  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
      {thinkingLevels.length > 0 && (
        <div>
          <Label htmlFor="agent-runtime-effort" className="text-xs text-muted-foreground">
            {t(($) => $.runtime_settings.effort)}
          </Label>
          <select
            id="agent-runtime-effort"
            aria-label={t(($) => $.runtime_settings.effort)}
            value={thinkingLevel}
            onChange={(event) => onThinkingChange(event.target.value)}
            className="mt-1.5 h-10 w-full rounded-lg border border-border bg-background px-3 text-sm"
          >
            <option value="">{t(($) => $.runtime_settings.runtime_default)}</option>
            {thinkingLevels.map((level) => (
              <option key={level.value} value={level.value}>{level.label}</option>
            ))}
          </select>
        </div>
      )}
      {speedLevels.length > 0 && (
        <div>
          <Label htmlFor="agent-runtime-speed" className="text-xs text-muted-foreground">
            {t(($) => $.runtime_settings.speed)}
          </Label>
          <select
            id="agent-runtime-speed"
            aria-label={t(($) => $.runtime_settings.speed)}
            value={speedMode || "standard"}
            onChange={(event) => onSpeedChange(event.target.value)}
            className="mt-1.5 h-10 w-full rounded-lg border border-border bg-background px-3 text-sm"
          >
            {speedLevels.map((level) => (
              <option key={level.value} value={level.value}>{level.label}</option>
            ))}
          </select>
        </div>
      )}
    </div>
  );
}
