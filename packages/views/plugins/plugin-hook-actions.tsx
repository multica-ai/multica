"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Loader2, Plug } from "lucide-react";
import { toast } from "sonner";
import { useFeatureEnabled } from "@multica/core/config";
import { PLUGINS_V1_FLAG } from "@multica/core/feature-flags";
import { useCurrentWorkspace } from "@multica/core/paths";
import { pluginInstallationsOptions, useInvokePluginHook } from "@multica/core/plugins";
import type { PluginHook, PluginInstallation } from "@multica/core/types";
import { useT } from "../i18n";

/**
 * Manual hook actions on an issue.
 *
 * `manual` is the trigger for "a person picked this from a menu", as opposed to
 * `ui` (a button the plugin drew inside its own panel) and `event` (nobody
 * picked anything). The distinction is not cosmetic — it decides which identity
 * the resulting writes carry — so the HOST owns this list rather than letting a
 * surface render its own menu entry.
 *
 * Like the Quick Actions list, this is NOT filtered by whether the call will
 * succeed. Hiding an action whose endpoint is currently down would give two
 * people on one issue different menus with nothing to explain the difference;
 * a refusal explains itself when it happens.
 */

export interface PluginHookAction {
  installation: PluginInstallation;
  hook: PluginHook;
}

export function pluginHookActionKey(action: PluginHookAction): string {
  return `${action.installation.id}:${action.hook.key}`;
}

/**
 * Collects the manual hooks of every enabled installation.
 *
 * Exported for its own test: the filter is the product rule (enabled install,
 * manual declared) and belongs in one place rather than re-derived per menu.
 */
export function collectManualHookActions(installations: readonly PluginInstallation[]): PluginHookAction[] {
  const actions: PluginHookAction[] = [];
  for (const installation of installations) {
    // Explicit === true per the API compatibility rule: a backend that stops
    // sending the field must read as "off", not as "truthy enough".
    if (installation.enabled !== true) continue;
    for (const hook of installation.hooks ?? []) {
      if ((hook.triggers ?? []).includes("manual")) actions.push({ installation, hook });
    }
  }
  return actions;
}

export function usePluginHookActions(): PluginHookAction[] {
  const pluginsEnabled = useFeatureEnabled(PLUGINS_V1_FLAG, false);
  const wsId = useCurrentWorkspace()?.id ?? "";
  const { data } = useQuery({
    ...pluginInstallationsOptions(wsId),
    enabled: pluginsEnabled && wsId.length > 0,
  });

  return useMemo(
    () => (pluginsEnabled ? collectManualHookActions(data?.plugins ?? []) : []),
    [data, pluginsEnabled],
  );
}

/**
 * Runs a manual hook and reports what happened.
 *
 * The message is outcome-specific on purpose. "refused" means the call never
 * left us — a scope the admin did not grant, a rate limit, a disabled install —
 * and telling somebody their plugin is broken when the real answer is "nobody
 * approved this" sends them to debug the wrong system.
 */
export function useRunPluginHook() {
  const { t } = useT("issues");
  const invoke = useInvokePluginHook();
  const [running, setRunning] = useState<string | null>(null);

  const run = async (action: PluginHookAction, issueId?: string) => {
    setRunning(pluginHookActionKey(action));
    try {
      const result = await invoke.mutateAsync({
        installationId: action.installation.id,
        hookKey: action.hook.key,
        trigger: "manual",
        issueId,
      });
      if (result.status === "ok") {
        toast.success(t(($) => $.plugins.hook_succeeded, { name: action.hook.name }));
      } else if (result.status === "refused") {
        toast.error(t(($) => $.plugins.hook_refused, { name: action.hook.name }), {
          description: result.error,
        });
      } else {
        toast.error(t(($) => $.plugins.hook_failed, { name: action.hook.name }), {
          description: result.error,
        });
      }
      return result;
    } catch (error) {
      toast.error(t(($) => $.plugins.hook_failed, { name: action.hook.name }), {
        description: error instanceof Error ? error.message : undefined,
      });
      return null;
    } finally {
      setRunning(null);
    }
  };

  return { run, running };
}

interface PluginHookMenuItemsProps {
  issueId: string;
  /**
   * The host menu's own Item component. Taken as a prop rather than imported so
   * the same list renders in the dropdown and the context menu — the pattern
   * `IssueActionsMenuItems` already uses, and the reason the two do not drift.
   */
  Item: React.ComponentType<{
    onClick?: () => void;
    disabled?: boolean;
    children?: React.ReactNode;
  }>;
  Separator?: React.ComponentType<Record<string, never>>;
}

export function PluginHookMenuItems({ issueId, Item, Separator }: PluginHookMenuItemsProps) {
  const actions = usePluginHookActions();
  const { run, running } = useRunPluginHook();

  if (actions.length === 0) return null;

  return (
    <>
      {Separator ? <Separator /> : null}
      {actions.map((action) => {
        const key = pluginHookActionKey(action);
        const isRunning = running === key;
        return (
          <Item key={key} disabled={isRunning} onClick={() => void run(action, issueId)}>
            {isRunning ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plug className="h-3.5 w-3.5" />}
            {/* The plugin's name, not just the action's: two installed plugins
                may both contribute "Summarize", and a menu showing the same
                word twice is a menu nobody can choose from. */}
            <span className="truncate">{action.hook.name}</span>
            <span className="ml-auto truncate pl-3 text-caption text-muted-foreground">
              {action.installation.name}
            </span>
          </Item>
        );
      })}
    </>
  );
}
