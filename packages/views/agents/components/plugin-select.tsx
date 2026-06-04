"use client";

import { useState } from "react";
import { ChevronDown, Plus, X, Puzzle } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { builtinPluginListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { PluginPickerList } from "./plugin-picker-list";

interface PluginSelectProps {
  /** Currently selected plugin ID (controlled). Empty string = none. */
  value: string;
  /** Called when selection changes. Empty string = clear. */
  onChange: (pluginId: string) => void;
}

/**
 * Searchable single-select for plugin binding in the create-agent dialog.
 * Collapsed by default; expands into a PluginPickerList with search.
 *
 * Follows the same visual pattern as SkillMultiSelect but is single-select
 * and backed by the external plugin catalog API.
 */
export function PluginSelect({ value, onChange }: PluginSelectProps) {
  const { t } = useT("agents");
  const { data: plugins, isLoading } = useQuery(builtinPluginListOptions());
  const [expanded, setExpanded] = useState(!!value);

  const items = plugins?.items ?? [];
  const selected = items.find((p) => p.id === value) ?? null;

  const label = t(($) => $.create_dialog.plugin_section.label);

  if (!expanded) {
    return (
      <div>
        <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {label}
        </div>
        <button
          type="button"
          onClick={() => setExpanded(true)}
          className="mt-1.5 flex w-full items-center gap-2.5 rounded-lg border bg-card px-3 py-3 text-left transition-colors hover:border-primary/40 hover:bg-accent/40"
        >
          <Plus className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1 truncate text-sm text-muted-foreground">
            {selected
              ? selected.name
              : t(($) => $.create_dialog.plugin_section.placeholder)}
          </div>
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground/40" />
        </button>
      </div>
    );
  }

  return (
    <div>
      <div className="flex items-center justify-between">
        <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {label}
        </div>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => setExpanded(false)}
          className="h-6 gap-1 px-2 text-xs"
        >
          <X className="h-3 w-3" />
          {t(($) => $.create_dialog.plugin_section.collapse)}
        </Button>
      </div>
      <div className="mt-1.5 rounded-lg border">
        <PluginPickerList
          plugins={items}
          selectedId={value || null}
          onSelect={(id) => onChange(id === value ? "" : id)}
          loading={isLoading}
        />
      </div>
      {value && (
        <div className="mt-1.5">
          <button
            type="button"
            onClick={() => onChange("")}
            className="flex w-full items-center gap-2 rounded-lg border px-3 py-2 text-left text-sm transition-colors hover:bg-accent/50"
          >
            <Puzzle className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium">
                {selected?.name ?? value}
              </div>
              {selected?.description ? (
                <div className="truncate text-xs text-muted-foreground">
                  {selected.description}
                </div>
              ) : null}
            </div>
            <X className="h-3.5 w-3.5 shrink-0 text-muted-foreground/40" />
          </button>
        </div>
      )}
    </div>
  );
}
