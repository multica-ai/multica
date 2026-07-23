"use client";

import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, Gauge } from "lucide-react";
import type {
  RuntimeModel,
  RuntimeModelServiceTier,
} from "@multica/core/types";
import { runtimeModelsOptions } from "@multica/core/runtimes";
import {
  PickerItem,
  PropertyPicker,
} from "../../../issues/components/pickers";
import { SettingsRow } from "../../../settings/components/settings-layout";
import { useT } from "../../../i18n";

/**
 * Full-width service-tier field for Codex agents. Capability comes from the
 * exact model's live catalog rather than a hard-coded Fast switch. An empty
 * model follows config.toml and cannot be resolved safely, so the field fails
 * closed unless a saved value needs to remain visible for explicit clearing.
 */
export function ServiceTierSettingField({
  label,
  runtimeId,
  runtimeOnline,
  model,
  value,
  canEdit,
  onChange,
}: {
  label: ReactNode;
  runtimeId: string | null;
  runtimeOnline: boolean;
  model: string;
  value: string;
  canEdit: boolean;
  onChange: (next: string) => Promise<void> | void;
}) {
  const modelsQuery = useQuery(
    runtimeModelsOptions(runtimeOnline ? runtimeId : null),
  );
  const entry = pickModelEntry(modelsQuery.data?.models ?? [], model);
  const tiers = entry?.service_tiers ?? [];

  if (tiers.length === 0 && !value) return null;

  return (
    <SettingsRow label={label} size="select-wide">
      <ServiceTierPicker
        value={value}
        tiers={tiers}
        canEdit={canEdit}
        onChange={onChange}
      />
    </SettingsRow>
  );
}

function ServiceTierPicker({
  value,
  tiers,
  canEdit,
  onChange,
}: {
  value: string;
  tiers: RuntimeModelServiceTier[];
  canEdit: boolean;
  onChange: (next: string) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  const selected = value ? tiers.find((tier) => tier.id === value) : undefined;
  const triggerLabel =
    selected?.name || value || t(($) => $.pickers.service_tier_default);
  const triggerTitle = t(($) => $.pickers.service_tier_tooltip, {
    value: triggerLabel,
  });

  const select = async (next: string) => {
    setOpen(false);
    if (next !== value) await onChange(next);
  };

  const display = (
    <div className="flex min-h-10 items-center gap-2 rounded-lg border border-input bg-input/50 px-3 text-sm text-muted-foreground">
      <Gauge className="h-4 w-4 shrink-0" aria-hidden="true" />
      <span className="min-w-0 truncate">{triggerLabel}</span>
    </div>
  );
  if (!canEdit) return display;

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-[var(--anchor-width)] min-w-[14rem] max-w-md"
      align="start"
      tooltip={triggerTitle}
      triggerRender={
        <button
          type="button"
          className="flex min-h-10 w-full min-w-0 items-center gap-2 rounded-lg border border-input bg-transparent px-3 text-left text-sm transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
          aria-label={triggerTitle}
        />
      }
      trigger={
        <>
          <Gauge
            className="h-4 w-4 shrink-0 text-muted-foreground"
            aria-hidden="true"
          />
          <span className="min-w-0 flex-1 truncate">{triggerLabel}</span>
          <ChevronDown
            className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${
              open ? "rotate-180" : ""
            }`}
            aria-hidden="true"
          />
        </>
      }
    >
      {tiers.map((tier) => (
        <PickerItem
          key={tier.id}
          selected={tier.id === value}
          onClick={() => void select(tier.id)}
        >
          <span className="block min-w-0 flex-1 text-left">
            <span className="truncate text-[13px] font-medium">
              {tier.name}
            </span>
            {tier.description ? (
              <span className="mt-0.5 block text-[11px] leading-snug text-muted-foreground">
                {tier.description}
              </span>
            ) : null}
          </span>
        </PickerItem>
      ))}
      {value ? (
        <button
          type="button"
          onClick={() => void select("")}
          className="mt-1 flex w-full items-center border-t px-3 py-2 text-left text-xs text-muted-foreground transition-colors hover:bg-accent/50"
          title={t(($) => $.pickers.service_tier_clear_title)}
        >
          {t(($) => $.pickers.service_tier_clear)}
        </button>
      ) : null}
    </PropertyPicker>
  );
}

function pickModelEntry(
  models: RuntimeModel[],
  model: string,
): RuntimeModel | undefined {
  if (!model) return undefined;
  return models.find((entry) => entry.id === model);
}
