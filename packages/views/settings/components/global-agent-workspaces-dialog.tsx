"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  globalAgentWorkspacesOptions,
  useDisableGlobalAgent,
  useEnableGlobalAgent,
} from "@multica/core/global-agents";
import type {
  GlobalAgent,
  GlobalAgentRuntimeOption,
  GlobalAgentWorkspaceTarget,
} from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useT } from "../../i18n";

type TargetStatus = "enabled" | "disabled" | "not_added";

function targetStatus(target: GlobalAgentWorkspaceTarget): TargetStatus {
  if (!target.agent) return "not_added";
  return target.agent.archived === true ? "disabled" : "enabled";
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * Per-workspace switchboard for one global agent. Enabling adds (or restores)
 * the linked workspace agent on the chosen runtime; disabling archives it.
 */
export function GlobalAgentWorkspacesDialog({
  agent,
  onClose,
}: {
  agent: GlobalAgent;
  onClose: () => void;
}) {
  const { t } = useT("settings");
  const targetsQuery = useQuery(globalAgentWorkspacesOptions(agent.id));
  const enableMutation = useEnableGlobalAgent();
  const disableMutation = useDisableGlobalAgent();

  // Runtime choice per workspace; unset rows fall back to the server's
  // suggestion so "Enable in all workspaces" needs no clicks per row.
  const [runtimeChoice, setRuntimeChoice] = useState<Record<string, string>>(
    {},
  );
  const [pendingWorkspaces, setPendingWorkspaces] = useState<Set<string>>(
    () => new Set(),
  );
  const [enablingAll, setEnablingAll] = useState(false);

  const targets = targetsQuery.data ?? [];
  const selectedRuntime = (target: GlobalAgentWorkspaceTarget) => {
    const chosen = runtimeChoice[target.workspace_id];
    if (chosen && target.runtimes.some((rt) => rt.id === chosen)) return chosen;
    return target.runtimes.some((rt) => rt.id === target.suggested_runtime_id)
      ? target.suggested_runtime_id
      : "";
  };
  const enableCandidates = targets.filter(
    (target) => targetStatus(target) !== "enabled" && !!selectedRuntime(target),
  );

  const setPending = (workspaceId: string, on: boolean) =>
    setPendingWorkspaces((current) => {
      const next = new Set(current);
      if (on) next.add(workspaceId);
      else next.delete(workspaceId);
      return next;
    });

  const enableIn = async (target: GlobalAgentWorkspaceTarget) => {
    const runtimeId = selectedRuntime(target);
    if (!runtimeId) return false;
    setPending(target.workspace_id, true);
    try {
      await enableMutation.mutateAsync({
        id: agent.id,
        workspace_id: target.workspace_id,
        runtime_id: runtimeId,
      });
      return true;
    } catch (err) {
      toast.error(
        t(($) => $.global_agents.workspaces.enable_failed_toast, {
          workspace: target.workspace_name,
          message: errorMessage(err),
        }),
      );
      return false;
    } finally {
      setPending(target.workspace_id, false);
    }
  };

  const handleEnable = async (target: GlobalAgentWorkspaceTarget) => {
    if (await enableIn(target)) {
      toast.success(
        t(($) => $.global_agents.workspaces.enabled_toast, {
          workspace: target.workspace_name,
        }),
      );
    }
  };

  const handleDisable = async (target: GlobalAgentWorkspaceTarget) => {
    setPending(target.workspace_id, true);
    try {
      await disableMutation.mutateAsync({
        id: agent.id,
        workspaceId: target.workspace_id,
      });
      toast.success(
        t(($) => $.global_agents.workspaces.disabled_toast, {
          workspace: target.workspace_name,
        }),
      );
    } catch (err) {
      toast.error(
        t(($) => $.global_agents.workspaces.disable_failed_toast, {
          workspace: target.workspace_name,
          message: errorMessage(err),
        }),
      );
    } finally {
      setPending(target.workspace_id, false);
    }
  };

  // Sequential on purpose: each enable validates the agent name inside one
  // workspace, and a failure in one must not hide the others' outcomes.
  const handleEnableAll = async () => {
    setEnablingAll(true);
    let enabled = 0;
    try {
      for (const target of enableCandidates) {
        if (await enableIn(target)) enabled += 1;
      }
    } finally {
      setEnablingAll(false);
    }
    if (enabled > 0) {
      toast.success(
        t(($) => $.global_agents.workspaces.enable_all_done_toast, {
          count: enabled,
        }),
      );
    }
  };

  const busy = enablingAll || pendingWorkspaces.size > 0;

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {t(($) => $.global_agents.workspaces.title, { name: agent.name })}
          </DialogTitle>
          <DialogDescription>
            {t(($) => $.global_agents.workspaces.description)}
          </DialogDescription>
        </DialogHeader>

        {targetsQuery.isLoading ? (
          <div className="space-y-2">
            {Array.from({ length: 2 }).map((_, i) => (
              <Skeleton key={i} className="h-14 w-full" />
            ))}
          </div>
        ) : targetsQuery.isError ? (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.global_agents.workspaces.load_failed)}
          </p>
        ) : targets.length === 0 ? (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.global_agents.workspaces.empty)}
          </p>
        ) : (
          <ul className="divide-y divide-surface-border rounded-lg border border-surface-border">
            {targets.map((target) => (
              <WorkspaceRow
                key={target.workspace_id}
                target={target}
                runtimeId={selectedRuntime(target)}
                onRuntimeChange={(runtimeId) =>
                  setRuntimeChoice((current) => ({
                    ...current,
                    [target.workspace_id]: runtimeId,
                  }))
                }
                pending={pendingWorkspaces.has(target.workspace_id)}
                disabled={enablingAll}
                onEnable={() => void handleEnable(target)}
                onDisable={() => void handleDisable(target)}
              />
            ))}
          </ul>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t(($) => $.global_agents.workspaces.close)}
          </Button>
          <Button
            onClick={() => void handleEnableAll()}
            disabled={busy || enableCandidates.length === 0}
            aria-busy={enablingAll || undefined}
          >
            {t(($) => $.global_agents.workspaces.enable_all)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function WorkspaceRow({
  target,
  runtimeId,
  onRuntimeChange,
  pending,
  disabled,
  onEnable,
  onDisable,
}: {
  target: GlobalAgentWorkspaceTarget;
  runtimeId: string;
  onRuntimeChange: (runtimeId: string) => void;
  pending: boolean;
  disabled: boolean;
  onEnable: () => void;
  onDisable: () => void;
}) {
  const { t } = useT("settings");
  const status = targetStatus(target);
  const runtimeLabel = (runtime: GlobalAgentRuntimeOption) =>
    runtime.status === "online"
      ? runtime.name
      : t(($) => $.global_agents.workspaces.runtime_offline, {
          name: runtime.name,
        });
  const runtimeItems = target.runtimes.map((runtime) => ({
    value: runtime.id,
    label: runtimeLabel(runtime),
  }));
  const statusLabel =
    status === "enabled"
      ? t(($) => $.global_agents.workspaces.status_enabled)
      : status === "disabled"
        ? t(($) => $.global_agents.workspaces.status_disabled)
        : t(($) => $.global_agents.workspaces.status_not_added);
  const boundRuntime =
    status === "enabled"
      ? target.runtimes.find((rt) => rt.id === target.agent?.runtime_id)
      : undefined;

  return (
    <li className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center">
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-body font-medium">
            {target.workspace_name || target.workspace_slug}
          </span>
          <Badge variant={status === "enabled" ? "secondary" : "outline"}>
            {statusLabel}
          </Badge>
        </div>
        {boundRuntime ? (
          <div className="mt-0.5 truncate text-caption text-muted-foreground">
            {runtimeLabel(boundRuntime)}
          </div>
        ) : null}
      </div>

      {status === "enabled" ? (
        <Button
          variant="outline"
          size="sm"
          onClick={onDisable}
          disabled={pending || disabled}
          aria-busy={pending || undefined}
        >
          {t(($) => $.global_agents.workspaces.disable)}
        </Button>
      ) : target.runtimes.length === 0 ? (
        <p className="text-caption text-muted-foreground sm:max-w-56 sm:text-right">
          {t(($) => $.global_agents.workspaces.no_runtime)}
        </p>
      ) : (
        <div className="flex items-center gap-2">
          <Select
            items={runtimeItems}
            value={runtimeId || null}
            onValueChange={(value) => {
              if (value) onRuntimeChange(value);
            }}
          >
            <SelectTrigger
              size="sm"
              className="w-full sm:w-56"
              aria-label={t(($) => $.global_agents.workspaces.runtime_label, {
                workspace: target.workspace_name || target.workspace_slug,
              })}
              disabled={pending || disabled}
            >
              <SelectValue
                placeholder={t(
                  ($) => $.global_agents.workspaces.runtime_placeholder,
                )}
              />
            </SelectTrigger>
            <SelectContent>
              {runtimeItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            size="sm"
            onClick={onEnable}
            disabled={!runtimeId || pending || disabled}
            aria-busy={pending || undefined}
          >
            {t(($) => $.global_agents.workspaces.enable)}
          </Button>
        </div>
      )}
    </li>
  );
}
