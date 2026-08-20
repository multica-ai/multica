"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, Loader2 } from "lucide-react";
import { pluginMCPToolsOptions, useApprovePluginMCPTools } from "@multica/core/plugins";
import type { PluginHook, PluginMCPTool } from "@multica/core/types";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { toast } from "sonner";
import { useT } from "../i18n";

/**
 * Approving the tools of an `mcp`-transport hook.
 *
 * An `http` hook needs nothing like this: it declares one endpoint in a manifest
 * the administrator read on the consent screen, and that endpoint is the whole
 * surface. An MCP server decides its own tool list at runtime and may change it
 * whenever it likes, so installing the plugin cannot be the grant — otherwise
 * approving it today authorises whatever the server offers next week.
 *
 * So: discovery is read-only and adopts nothing, and this panel is where an
 * administrator pins a specific set. The server stores each tool with its schema
 * digest, and the daemon refuses to start a connection whose approved tool went
 * missing or whose schema drifted.
 */

/** Which hooks need an approval panel at all. */
export function mcpHooks(hooks: readonly PluginHook[]): PluginHook[] {
  return hooks.filter((hook) => hook.transport === "mcp" && (hook.triggers ?? []).includes("agent"));
}

/**
 * What the save button will send.
 *
 * The complete approved set, never a delta — unchecking a tool and saving is
 * what revokes it, so the checkbox state IS the request. Also why a drifted tool
 * starts unchecked: its schema is not the one that was approved, and leaving it
 * checked would let one click re-approve a shape nobody looked at.
 */
export function initialSelection(tools: readonly PluginMCPTool[]): string[] {
  return tools.filter((tool) => tool.approved && !tool.drifted).map((tool) => tool.name);
}

export function PluginMCPApproval({
  wsId,
  installationId,
  hook,
  canManage,
}: {
  wsId: string;
  installationId: string;
  hook: PluginHook;
  canManage: boolean;
}) {
  const { t } = useT("settings");
  const { data, isLoading, isError, error } = useQuery(pluginMCPToolsOptions(wsId, installationId, hook.key));
  const approve = useApprovePluginMCPTools(wsId, installationId, hook.key);

  const tools = useMemo(() => data ?? [], [data]);
  // null means "not edited yet", so the checkboxes track the server until the
  // administrator touches one. A background refetch then cannot silently undo an
  // edit in progress; saving clears it back to null and the server wins again.
  const [selected, setSelected] = useState<string[] | null>(null);
  const current = selected ?? initialSelection(tools);

  const toggle = (name: string, checked: boolean) => {
    setSelected(checked ? [...current, name] : current.filter((entry) => entry !== name));
  };

  const save = () => {
    approve
      .mutateAsync(current)
      .then(() => {
        setSelected(null);
        toast.success(t(($) => $.plugins.mcp.approved));
      })
      .catch((cause: unknown) => {
        toast.error(cause instanceof Error ? cause.message : t(($) => $.plugins.action_failed));
      });
  };

  return (
    <div className="space-y-2 rounded-md border border-surface-border px-3 py-3">
      <div className="flex items-center gap-2">
        <span className="text-caption font-medium">{hook.name}</span>
        <Badge variant="outline">{t(($) => $.plugins.mcp.badge)}</Badge>
      </div>
      <p className="text-caption text-muted-foreground">{t(($) => $.plugins.mcp.description)}</p>

      {isLoading ? (
        <Skeleton className="h-16 w-full" aria-label={t(($) => $.plugins.mcp.loading)} />
      ) : isError ? (
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>{t(($) => $.plugins.mcp.unreachable)}</AlertTitle>
          {/* The endpoint failing is the actionable part: it is the author's own
              server, and the administrator can only fix it by telling them. */}
          <AlertDescription>{error instanceof Error ? error.message : ""}</AlertDescription>
        </Alert>
      ) : tools.length === 0 ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.plugins.mcp.empty)}</p>
      ) : (
        <>
          <ul className="space-y-1.5">
            {tools.map((tool) => (
              <li key={tool.name} className="flex items-start gap-2">
                <Checkbox
                  id={`${installationId}-${hook.key}-${tool.name}`}
                  className="mt-0.5"
                  disabled={!canManage || approve.isPending}
                  checked={current.includes(tool.name)}
                  onCheckedChange={(checked) => toggle(tool.name, checked === true)}
                />
                <label
                  htmlFor={`${installationId}-${hook.key}-${tool.name}`}
                  className="min-w-0 text-caption"
                >
                  <span className="font-mono">{tool.name}</span>
                  {tool.drifted ? (
                    <Badge variant="destructive" className="ml-2">
                      {t(($) => $.plugins.mcp.drifted)}
                    </Badge>
                  ) : null}
                  {tool.description ? (
                    <span className="block text-muted-foreground">{tool.description}</span>
                  ) : null}
                </label>
              </li>
            ))}
          </ul>
          <div className="flex justify-end">
            <Button size="sm" disabled={!canManage || approve.isPending} onClick={save}>
              {approve.isPending ? <Loader2 className="animate-spin" /> : null}
              {t(($) => $.plugins.mcp.save)}
            </Button>
          </div>
        </>
      )}
    </div>
  );
}
