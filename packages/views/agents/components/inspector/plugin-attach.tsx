"use client";

import { useState } from "react";
import { Puzzle, Plus, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { builtinPluginListOptions } from "@multica/core/workspace/queries";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { useT } from "../../../i18n";
import { PluginPickerList } from "../plugin-picker-list";

interface PluginAttachProps {
  agent: Agent;
  canEdit: boolean;
  /** Called when user selects/deselects a plugin. Empty string = clear. */
  onChange: (pluginId: string) => void;
}

/**
 * Inline editable plugin picker for the agent detail inspector. Shows the
 * current plugin name as a badge (read-only) or as a clickable trigger that
 * opens a Popover with a PluginPickerList.
 *
 * Follows the same pattern as SkillAttach but uses Popover instead of Dialog
 * for a lighter inline-editing experience.
 */
export function PluginAttach({ agent, canEdit, onChange }: PluginAttachProps) {
  const { t } = useT("agents");
  const { data: plugins } = useQuery(builtinPluginListOptions());
  const [open, setOpen] = useState(false);

  const items = plugins?.items ?? [];
  const selected = items.find((p) => p.id === agent.plugin_id) ?? null;
  const stale = !selected && !!agent.plugin_id;

  // --- Read-only display ---
  if (!canEdit) {
    if (!agent.plugin_id) {
      return (
        <span className="text-xs italic text-muted-foreground/50">
          {t(($) => $.inspector.plugin_none)}
        </span>
      );
    }
    if (stale) {
      return (
        <span className="rounded-md bg-warning/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-warning">
          {t(($) => $.inspector.plugin_unavailable_chip, { id: agent.plugin_id.slice(0, 8) + "..." })}
        </span>
      );
    }
    return (
      <span className="rounded-md bg-muted px-1.5 py-0.5 font-mono text-[10px] font-medium text-muted-foreground">
        {selected?.name}
      </span>
    );
  }

  // --- Editable picker ---
  const handleSelect = (pluginId: string) => {
    onChange(pluginId === agent.plugin_id ? "" : pluginId);
    setOpen(false);
  };

  const handleClear = () => {
    onChange("");
    setOpen(false);
  };

  // Stale binding: show warning chip with remove action
  if (stale) {
    return (
      <span className="inline-flex items-center gap-0.5 rounded-md border border-warning/30 bg-warning/10 px-1.5 py-0.5 text-[10px] font-medium text-warning">
        <Puzzle className="h-2.5 w-2.5" />
        {t(($) => $.inspector.plugin_unavailable_chip, { id: agent.plugin_id!.slice(0, 8) + "..." })}
        <button
          type="button"
          onClick={handleClear}
          className="ml-1 inline-flex items-center rounded-sm hover:text-destructive"
          aria-label={t(($) => $.inspector.plugin_clear)}
        >
          <X className="h-2.5 w-2.5" />
        </button>
      </span>
    );
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <button
            type="button"
            className="inline-flex cursor-pointer items-center gap-0.5 rounded-md border border-dashed border-muted-foreground/30 px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground/70 transition-colors hover:border-muted-foreground/60 hover:bg-accent/50 hover:text-muted-foreground"
            aria-label={
              selected
                ? t(($) => $.inspector.plugin_change_aria)
                : t(($) => $.inspector.plugin_attach_aria)
            }
            title={
              selected
                ? t(($) => $.inspector.plugin_change_aria)
                : t(($) => $.inspector.plugin_attach_aria)
            }
          >
            {selected ? (
              <>
                <Puzzle className="h-2.5 w-2.5" />
                {selected.name}
              </>
            ) : (
              <>
                <Plus className="h-2.5 w-2.5" />
                {t(($) => $.inspector.plugin_attach)}
              </>
            )}
          </button>
        }
      />
      <PopoverContent align="start" className="w-72 p-0">
        <PluginPickerList
          plugins={items}
          selectedId={agent.plugin_id}
          onSelect={handleSelect}
        />
        {agent.plugin_id && (
          <div className="border-t border-border px-3 py-2">
            <button
              type="button"
              onClick={handleClear}
              className="w-full rounded px-2 py-1 text-left text-xs text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
            >
              <X className="mr-1.5 inline-block h-3 w-3" />
              {t(($) => $.inspector.plugin_clear)}
            </button>
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}
