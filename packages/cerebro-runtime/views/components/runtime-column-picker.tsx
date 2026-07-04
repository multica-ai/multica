"use client";

import { SlidersHorizontal } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useT } from "@multica/views/i18n";
import {
  RUNTIME_COLUMN_KEYS,
  type RuntimeColumnKey,
  useRuntimesViewStore,
} from "../runtime-view-store";

// FIR-2669: column picker for the Runtimes list. Mirrors the agents list's
// display popover (a Switch per column), reusing the existing runtime column
// header labels so the picker and table headers read identically.
export function RuntimeColumnPicker() {
  const { t } = useT("runtimes");
  const hiddenColumns = useRuntimesViewStore((s) => s.hiddenColumns);
  const toggleColumn = useRuntimesViewStore((s) => s.toggleColumn);

  const COLUMN_LABELS: Record<RuntimeColumnKey, string> = {
    machine: t(($) => $.list.col_machine),
    agents: t(($) => $.list.col_agents),
    cost: t(($) => $.list.col_cost),
    account: t(($) => $.list.col_account),
    cli: t(($) => $.list.col_cli),
  };

  return (
    <Popover>
      <Tooltip>
        <PopoverTrigger
          render={
            <TooltipTrigger
              render={
                <Button
                  variant="outline"
                  size="sm"
                  className="h-8 w-8 gap-1 px-0 text-muted-foreground md:w-auto md:px-2.5"
                >
                  <SlidersHorizontal className="size-3.5" />
                  <span className="hidden md:inline">
                    {t(($) => $.list.columns_label)}
                  </span>
                </Button>
              }
            />
          }
        />
        <TooltipContent side="bottom">
          {t(($) => $.list.columns_label)}
        </TooltipContent>
      </Tooltip>
      <PopoverContent align="end" className="w-56 p-0">
        <div className="px-3 py-2.5">
          <span className="text-xs font-medium text-muted-foreground">
            {t(($) => $.list.columns_label)}
          </span>
          <div className="mt-2 space-y-2">
            {RUNTIME_COLUMN_KEYS.map((key) => (
              <label
                key={key}
                className="flex cursor-pointer items-center justify-between"
              >
                <span className="text-sm">{COLUMN_LABELS[key]}</span>
                <Switch
                  size="sm"
                  checked={!hiddenColumns.includes(key)}
                  onCheckedChange={() => toggleColumn(key)}
                />
              </label>
            ))}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
