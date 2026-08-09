"use client";

import { useEffect, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import type {
  Agent,
  AgentRuntime,
  MemberWithUser,
  MigrateAgentsToRuntimeResponse,
} from "@multica/core/types";
import { api } from "@multica/core/api";
import { useMigrateAgentsToRuntime } from "@multica/core/runtimes/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import { ModelPicker } from "./inspector/model-picker";
import { RuntimePicker } from "./inspector/runtime-picker";
import { ServiceTierSettingField } from "./inspector/service-tier-setting-field";
import { ThinkingSettingField } from "./inspector/thinking-prop-row";

/**
 * Declarative "set runtime + model settings" form for one agent or many
 * (MUL-5758, reshaped 2026-08-06). What the form shows is what every checked
 * agent gets: the target runtime, and either the picked model / thinking /
 * speed or the runtime default when left empty. Agents already on the target
 * are updated in place; queued tasks follow agents that actually change
 * runtime.
 *
 * Three entry points share this component — the Agent List row menu, the
 * batch toolbar, and the Runtime detail page — and differ only in what they
 * pass in. A single agent is `agents` of length one, not a separate flow.
 *
 * The summary numbers come from a server dry run, not from presence: the
 * presence projection merges 'dispatched' into "queued" and omits 'deferred',
 * so it cannot state the task split. The preview is informational only —
 * submission never waits on it.
 */
export function SwitchAgentRuntimeDialog({
  open,
  onOpenChange,
  agents,
  runtimes,
  members,
  currentUserId,
  wsId,
  expectedSourceRuntimeId,
  onMigrated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The agents to move. One entry is the single-agent case. */
  agents: Agent[];
  runtimes: AgentRuntime[];
  members: MemberWithUser[];
  currentUserId: string | null;
  wsId: string;
  /** Set by the Runtime detail entry point so the server can refuse a plan
   *  that drifted since the page rendered it. */
  expectedSourceRuntimeId?: string;
  onMigrated?: (result: MigrateAgentsToRuntimeResponse) => void;
}) {
  const { t } = useT("agents");
  const migrate = useMigrateAgentsToRuntime(wsId);

  const [targetRuntimeId, setTargetRuntimeId] = useState("");
  const [excluded, setExcluded] = useState<ReadonlySet<string>>(new Set());
  // Set when the user presses Confirm while the dialog cannot submit, and
  // names the specific blocker. The button itself stays clickable: a silently
  // disabled button tells the user nothing about what to fix.
  const [blocker, setBlocker] = useState<string | null>(null);
  // Optional uniform replacement for the cleared model settings. Empty model
  // means today's behaviour: clear and let the new runtime resolve defaults.
  const [model, setModel] = useState("");
  const [thinkingLevel, setThinkingLevel] = useState("");
  const [serviceTier, setServiceTier] = useState("");

  // Reset on every open so a previous selection can never leak into the next
  // dialog session (same discipline as the bulk access dialog).
  useEffect(() => {
    if (!open) return;
    setTargetRuntimeId("");
    setExcluded(new Set());
    setModel("");
    setThinkingLevel("");
    setServiceTier("");
  }, [open]);

  // A model choice is only meaningful on the runtime it was picked from;
  // changing the target resets the whole chain (model → thinking → speed).
  useEffect(() => {
    setModel("");
    setThinkingLevel("");
    setServiceTier("");
  }, [targetRuntimeId]);

  // A blocker message describes the state at the moment Confirm was pressed.
  // Any input change invalidates it — keeping it up would scold the user for
  // something they may have just fixed.
  useEffect(() => {
    setBlocker(null);
  }, [targetRuntimeId, excluded, model]);

  const selected = agents.filter((a) => !excluded.has(a.id));
  const selectedIds = selected.map((a) => a.id);
  const isBulk = agents.length > 1;

  // Server-computed projection of this exact selection, purely informational
  // (task counts, offline warning). Keyed on selection + target + model so a
  // change re-asks instead of showing numbers for a set the user has since
  // edited; submission never waits on it.
  const preview = useQuery({
    queryKey: [
      "agents",
      wsId,
      "migrate-preview",
      targetRuntimeId,
      model,
      [...selectedIds].sort().join(","),
    ],
    queryFn: () =>
      api.migrateAgentsToRuntime(targetRuntimeId, {
        agent_ids: selectedIds,
        dry_run: true,
        model: model || undefined,
      }),
    enabled: open && !!targetRuntimeId && selectedIds.length > 0,
    staleTime: 0,
    // Keep showing the previous selection's numbers (dimmed) while the next
    // dry run is in flight. Without this every checkbox toggle unmounts the
    // whole summary into a spinner line and remounts it, and the dialog
    // visibly jumps in height twice per click.
    placeholderData: keepPreviousData,
  });

  const targetRuntime = runtimes.find((r) => r.id === targetRuntimeId) ?? null;

  const toggleExcluded = (id: string) => {
    setExcluded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const handleConfirm = async () => {
    // Validate on click and say what is missing, instead of a silently
    // disabled button. The dry-run preview is informational only — the server
    // re-classifies everything inside the write transaction, so submitting
    // does not wait on it.
    if (!targetRuntimeId) {
      setBlocker(t(($) => $.migrate_dialog.blocker_no_target));
      return;
    }
    if (selectedIds.length === 0) {
      setBlocker(t(($) => $.migrate_dialog.blocker_no_agents));
      return;
    }
    setBlocker(null);
    try {
      const result = await migrate.mutateAsync({
        targetRuntimeId,
        agentIds: selectedIds,
        expectedSourceRuntimeId,
        // Only sent when the user picked a replacement; thinking / speed are
        // model-native and never travel without the model.
        model: model || undefined,
        thinkingLevel: model ? thinkingLevel || undefined : undefined,
        serviceTier: model ? serviceTier || undefined : undefined,
      });
      onOpenChange(false);
      onMigrated?.(result);
      toast.success(
        t(($) => $.migrate_dialog.success_toast, {
          count: result.migrated.length,
          tasks: result.tasks_migrated,
        }),
      );
      if (result.skipped.length > 0) {
        toast.warning(
          t(($) => $.migrate_dialog.skipped_toast, {
            count: result.skipped.length,
          }),
        );
      }
    } catch (e) {
      // A 409 means the agent set moved under the user (Runtime detail entry
      // point). Nothing was written, so the honest recovery is to close and
      // let the refreshed page offer the current set.
      toast.error(
        e instanceof Error ? e.message : t(($) => $.migrate_dialog.failed_toast),
      );
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* Pinned to a fixed top instead of the default vertical centering: a
          centered dialog re-centers on EVERY content height change, moving
          every element (including the title the user is reading). Pinned,
          growth only pushes downward and the interactive top half never
          moves — this was the main jitter source in the 2026-08-06 trace. */}
      <DialogContent className="top-[10%] translate-y-0 sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {isBulk
              ? t(($) => $.migrate_dialog.title_bulk, { count: agents.length })
              : t(($) => $.migrate_dialog.title_single, {
                  name: agents[0]?.name ?? "",
                })}
          </DialogTitle>
          <DialogDescription className="sr-only">
            {t(($) => $.migrate_dialog.title_bulk, { count: agents.length })}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <RuntimePicker
            variant="field"
            value={targetRuntimeId}
            runtimes={runtimes}
            members={members}
            currentUserId={currentUserId}
            onChange={(id) => setTargetRuntimeId(id)}
          />

          {isBulk && (
            <div className="max-h-48 space-y-1 overflow-y-auto rounded-lg border p-2">
              {agents.map((agent) => (
                <button
                  key={agent.id}
                  type="button"
                  onClick={() => toggleExcluded(agent.id)}
                  className="flex w-full items-center gap-2 rounded-md px-1.5 py-1 text-left transition-colors hover:bg-accent"
                >
                  <Checkbox
                    checked={!excluded.has(agent.id)}
                    tabIndex={-1}
                    className="pointer-events-none"
                  />
                  <ActorAvatar
                    actorType="agent"
                    actorId={agent.id}
                    size="sm"
                  />
                  <span className="min-w-0 flex-1 truncate text-body">
                    {agent.name}
                  </span>
                </button>
              ))}
            </div>
          )}

          {/* Optional model for the migrated agents (MUL-5758 follow-up). The
              runtime → model → thinking → speed chain is bound: the pickers
              read the TARGET runtime's catalog, and the dependent fields
              self-hide until a model that advertises them is chosen. When the
              catalog is unreachable (offline target — the evacuation case)
              the picker degrades to manual entry and leaving it empty keeps
              today's clear-to-default behaviour. */}
          {!!targetRuntimeId && (
            <div className="space-y-1 rounded-lg border p-3">
              <p className="text-caption font-medium">
                {t(($) => $.migrate_dialog.model_section_title)}
              </p>
              <ModelPicker
                variant="field"
                showLabel={false}
                runtimeId={targetRuntimeId}
                runtimeOnline={targetRuntime?.status === "online"}
                value={model}
                canEdit={!migrate.isPending}
                onChange={(next) => {
                  setModel(next);
                  setThinkingLevel("");
                  setServiceTier("");
                }}
              />
              <ThinkingSettingField
                label={t(($) => $.inspector.prop_thinking)}
                runtimeId={targetRuntimeId}
                runtimeOnline={targetRuntime?.status === "online"}
                provider={targetRuntime?.provider ?? ""}
                model={model}
                value={thinkingLevel}
                canEdit={!migrate.isPending}
                onChange={setThinkingLevel}
              />
              <ServiceTierSettingField
                label={t(($) => $.inspector.prop_speed)}
                runtimeId={targetRuntimeId}
                runtimeOnline={targetRuntime?.status === "online"}
                model={model}
                value={serviceTier}
                canEdit={!migrate.isPending}
                onChange={setServiceTier}
              />
            </div>
          )}

          {/* Everything below is the consequence summary. It stays empty until
              a target is picked, because none of it is knowable before then.
              Three states, none of which unmounts the block once shown:
              first load renders skeleton lines at the summary's height, a
              refetch dims the previous numbers in place (keepPreviousData),
              and fresh data swaps in without a layout jump. */}
          {!!targetRuntimeId && selectedIds.length > 0 && (
              <div>
                {!preview.data ? (
                  <div className="space-y-2" aria-live="polite">
                    <Skeleton className="h-4 w-3/5" />
                    <Skeleton className="h-4 w-2/5" />
                  </div>
                ) : (
                  <div
                    className={`space-y-3 text-caption transition-opacity ${
                      preview.isFetching ? "opacity-60" : "opacity-100"
                    }`}
                    aria-live="polite"
                  >
                    {/* Only stated when something actually moves — "0 tasks
                        will migrate" is noise on every no-task selection. */}
                    {preview.data.tasks_migrated > 0 && (
                      <p className="flex items-center gap-2 text-muted-foreground">
                        {t(($) => $.migrate_dialog.tasks_migrating, {
                          count: preview.data.tasks_migrated,
                        })}
                        {preview.isFetching && (
                          <Loader2 className="size-3 animate-spin" />
                        )}
                      </p>
                    )}
                    {preview.data.tasks_staying_active > 0 && (
                      <p className="text-muted-foreground">
                        {t(($) => $.migrate_dialog.tasks_staying, {
                          count: preview.data.tasks_staying_active,
                        })}
                      </p>
                    )}

                    {targetRuntime && targetRuntime.status !== "online" && (
                      <p className="text-amber-600 dark:text-amber-400">
                        {t(($) => $.migrate_dialog.target_offline)}
                      </p>
                    )}
                  </div>
                )}
              </div>
            )}
        </div>

        {/* Names what Confirm is waiting for, set when the user presses it
            while the dialog cannot submit. Rendered above the footer so the
            answer appears where the user is already looking. */}
        {blocker && (
          <p className="text-caption text-destructive" aria-live="assertive">
            {blocker}
          </p>
        )}

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={migrate.isPending}
            onClick={() => onOpenChange(false)}
          >
            {t(($) => $.migrate_dialog.cancel)}
          </Button>
          {/* Deliberately clickable in every non-pending state: pressing it
              with missing input answers WHY it cannot submit (see
              handleConfirm) instead of leaving a mute disabled button. */}
          <Button
            type="button"
            size="sm"
            disabled={migrate.isPending}
            onClick={handleConfirm}
          >
            {migrate.isPending ? (
              <Loader2 className="mr-1 size-3.5 animate-spin" />
            ) : null}
            {t(($) => $.migrate_dialog.confirm)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
