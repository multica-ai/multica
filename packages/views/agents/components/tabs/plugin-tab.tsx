"use client";

import { Puzzle, Info, X } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Agent } from "@multica/core/types";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import {
  builtinPluginListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../../i18n";
import { PluginPickerList } from "../plugin-picker-list";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";

export function PluginTab({
  agent,
}: {
  agent: Agent;
}) {
  const { t } = useT("agents");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const { data: plugins } = useQuery(builtinPluginListOptions());
  const items = plugins?.items ?? [];
  const selected = items.find((p) => p.id === agent.plugin_id) ?? null;
  const stale = !selected && !!agent.plugin_id;

  const handleChange = async (pluginId: string) => {
    try {
      await api.updateAgent(agent.id, {
        plugin_id: pluginId === agent.plugin_id ? "" : pluginId,
      });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(t(($) => $.detail.agent_updated_toast));
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.detail.update_failed_toast),
      );
    }
  };

  const handleRemove = async () => {
    try {
      await api.updateAgent(agent.id, { plugin_id: "" });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(t(($) => $.detail.agent_updated_toast));
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.detail.update_failed_toast),
      );
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-3">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.plugin.intro)}
        </p>
        <PluginPickerPopover
          items={items}
          selectedId={agent.plugin_id}
          onSelect={handleChange}
          triggerLabel={t(($) => $.tab_body.plugin.change_action)}
        />
      </div>

      {items.length > 0 && (
        <div className="flex items-start gap-2 rounded-md border border-info/20 bg-info/5 px-3 py-2.5">
          <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-info" />
          <p className="text-xs text-muted-foreground">
            {t(($) => $.tab_body.plugin.info_hint)}
          </p>
        </div>
      )}

      {stale ? (
        <div className="rounded-lg border border-warning/30 bg-warning/5">
          <div className="flex items-start gap-3 px-4 py-4">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-warning/10">
              <Puzzle className="h-5 w-5 text-warning" />
            </div>
            <div className="min-w-0 flex-1">
              <h3 className="truncate text-sm font-semibold text-warning">
                {t(($) => $.inspector.plugin_unavailable_chip, { id: agent.plugin_id!.slice(0, 8) + "..." })}
              </h3>
              <p className="mt-1 text-xs text-muted-foreground">
                {t(($) => $.inspector.plugin_removed_hint)}
              </p>
            </div>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={handleRemove}
              className="text-muted-foreground hover:text-destructive"
            >
              <X className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
      ) : !selected ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12">
          <Puzzle className="h-8 w-8 text-muted-foreground/40" />
          <p className="mt-3 text-sm text-muted-foreground">
            {t(($) => $.tab_body.plugin.empty_title)}
          </p>
          <p className="mt-1 max-w-xs text-center text-xs text-muted-foreground">
            {t(($) => $.tab_body.plugin.empty_hint)}
          </p>
          {items.length > 0 && (
            <PluginPickerPopover
              items={items}
              selectedId={agent.plugin_id}
              onSelect={handleChange}
              className="mt-3"
              triggerLabel={t(($) => $.tab_body.plugin.select_action)}
            />
          )}
        </div>
      ) : (
        <div className="rounded-lg border">
          <div className="flex items-start gap-3 px-4 py-4">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-purple-500/10">
              <Puzzle className="h-5 w-5 text-purple-500" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <h3 className="truncate text-sm font-semibold">
                  {selected.name}
                </h3>
                {selected.version && (
                  <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                    v{selected.version}
                  </span>
                )}
              </div>
              {selected.description && (
                <p className="mt-1 text-xs text-muted-foreground">
                  {selected.description}
                </p>
              )}
              <div className="mt-2 flex flex-wrap items-center gap-1.5">
                <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                  {selected.category}
                </span>
                <span className="font-mono text-[10px] text-muted-foreground/60">
                  {selected.slug}
                </span>
              </div>
            </div>
            <div className="flex shrink-0 items-center gap-1">
              <PluginPickerPopover
                items={items}
                selectedId={agent.plugin_id}
                onSelect={handleChange}
                triggerLabel={t(($) => $.tab_body.plugin.change_action)}
              />
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={handleRemove}
                className="text-muted-foreground hover:text-destructive"
              >
                <X className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

/** Compact plugin picker popover for the PluginTab. */
function PluginPickerPopover({
  items,
  selectedId,
  onSelect,
  className,
  triggerLabel,
}: {
  items: BuiltinPlugin[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  className?: string;
  triggerLabel: string;
}) {
  const { t } = useT("agents");

  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button variant="outline" size="sm" className={className}>
            <Puzzle className="h-3 w-3" />
            {triggerLabel}
          </Button>
        }
      />
      <PopoverContent align="start" className="w-72 p-0">
        <PluginPickerList
          plugins={items}
          selectedId={selectedId}
          onSelect={(id) => {
            onSelect(id);
          }}
        />
        {selectedId && (
          <div className="border-t border-border px-3 py-2">
            <button
              type="button"
              onClick={() => onSelect(selectedId)}
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
