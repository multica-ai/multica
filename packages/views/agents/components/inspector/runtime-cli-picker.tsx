"use client";

import { useMemo, useState } from "react";
import { ChevronDown, Monitor } from "@multica/ui/icons";
import {
  uniqueRuntimeClis,
  type RuntimeCliSource,
} from "@multica/core/agents";
import { providerDisplayName } from "@multica/core/runtimes";
import {
  PickerItem,
  PropertyPicker,
} from "../../../issues/components/pickers";
import { ProviderLogo } from "../../../runtimes/components/provider-logo";
import { useT } from "../../../i18n";

/**
 * Smart-runtime recommended CLI picker. The closed control shows only the
 * CLI name (Cursor / Codex / …) — never owner or machine. Selecting a CLI
 * writes `cli`, not a runtime attribution.
 */
export function RuntimeCliPicker({
  value,
  runtimes,
  canEdit = true,
  onChange,
}: {
  value: string;
  runtimes: RuntimeCliSource[];
  canEdit?: boolean;
  onChange: (cli: string) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  const clis = useMemo(() => uniqueRuntimeClis(runtimes), [runtimes]);
  const selected = value.trim().toLowerCase();
  const selectedLabel = selected
    ? providerDisplayName(selected)
    : t(($) => $.pickers.cli_none);

  if (!canEdit) {
    return (
      <div className="flex min-h-10 items-center gap-2 rounded-lg border border-input bg-input/50 px-3 text-body text-muted-foreground">
        {selected ? (
          <ProviderLogo provider={selected} className="h-4 w-4 shrink-0" />
        ) : (
          <Monitor
            className="h-4 w-4 shrink-0 text-muted-foreground"
            aria-hidden="true"
          />
        )}
        <span className="min-w-0 flex-1 truncate">{selectedLabel}</span>
      </div>
    );
  }

  const triggerTitle = selected
    ? t(($) => $.pickers.cli_tooltip, { name: selectedLabel })
    : t(($) => $.pickers.cli_tooltip_none);

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-[var(--anchor-width)] min-w-[18rem] max-w-md"
      align="start"
      tooltip={triggerTitle}
      triggerRender={
        <button
          type="button"
          className="flex min-h-10 w-full min-w-0 items-center gap-2 rounded-lg border border-input bg-transparent px-3 text-left text-body transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
          aria-label={triggerTitle}
        />
      }
      trigger={
        <>
          {selected ? (
            <ProviderLogo provider={selected} className="h-4 w-4 shrink-0" />
          ) : (
            <Monitor
              className="h-4 w-4 shrink-0 text-muted-foreground"
              aria-hidden="true"
            />
          )}
          <span className="min-w-0 flex-1 truncate">{selectedLabel}</span>
          <ChevronDown
            className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${
              open ? "rotate-180" : ""
            }`}
            aria-hidden="true"
          />
        </>
      }
    >
      {clis.length === 0 ? (
        <p className="px-2 py-3 text-center text-caption text-muted-foreground">
          {t(($) => $.pickers.cli_empty)}
        </p>
      ) : (
        clis.map((cli) => (
          <PickerItem
            key={cli}
            selected={cli === selected}
            onClick={() => {
              setOpen(false);
              if (cli !== selected) void onChange(cli);
            }}
            tooltip={providerDisplayName(cli)}
          >
            <ProviderLogo provider={cli} className="h-4 w-4 shrink-0" />
            <span className="min-w-0 flex-1 truncate text-body font-medium">
              {providerDisplayName(cli)}
            </span>
          </PickerItem>
        ))
      )}
    </PropertyPicker>
  );
}
