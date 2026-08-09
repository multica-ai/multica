"use client";

import { useCallback, useEffect, useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { MemberWithUser } from "@multica/core/types";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  UI_EASE_OUT,
  UI_MOTION_DURATION,
} from "@multica/ui/lib/motion";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Archive,
  ArchiveRestore,
  ChevronUp,
  KeyRound,
  Loader2,
  MonitorCog,
  Square,
  Users,
  X,
} from "lucide-react";
import { useT } from "../../i18n";
import { AccessPicker, type AccessChange } from "./inspector/access-picker";
import type { AgentListRow } from "./agents-page";

/**
 * Floating batch-toolbar for the agents list page: a selection count plus one
 * "Actions" menu holding every bulk operation (MUL-5758 follow-up). The flat
 * button row it replaces stopped scaling at five actions; a menu grows by one
 * item per new operation and keeps the bar a fixed width in every locale.
 *
 * Menu items are always rendered (when their lifecycle state applies) and
 * disabled when no selected row qualifies, so users learn one stable menu
 * instead of a layout that reshuffles with each selection.
 *
 * Permission gates mirror the server's, per action: set access is gated by
 * `isOwnedByMe` (owner-only write for `permission_mode` / `invocation_targets`,
 * MUL-4302), the rest by `canManage`. Rows that fail a gate are dropped from
 * that action's hand-off rather than sent and skipped.
 */
export function AgentBatchToolbar({
  rows,
  members,
  currentUserId,
  onClear,
  onSwitchRuntime,
  onInjectEnv,
}: {
  rows: AgentListRow[];
  members: MemberWithUser[];
  currentUserId: string | null;
  onClear: () => void;
  // Bulk quick actions. Like the row menu, the toolbar only announces intent —
  // the page owns the dialogs so the single-agent and bulk paths mount the
  // exact same component.
  onSwitchRuntime: (rows: AgentListRow[]) => void;
  onInjectEnv: (rows: AgentListRow[]) => void;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const shouldReduceMotion = useReducedMotion() ?? false;
  const [confirmArchive, setConfirmArchive] = useState(false);
  const [confirmCancel, setConfirmCancel] = useState(false);
  const [confirmAccess, setConfirmAccess] = useState(false);
  const [accessChange, setAccessChange] = useState<AccessChange | null>(null);
  const [busy, setBusy] = useState(false);

  // Must be stable: AccessPicker lists this in the effect that notifies us, so
  // an inline callback would re-notify on every render we cause by storing the
  // change — that is an update loop, not a re-render.
  const handleAccessReadyChange = useCallback(
    (ready: boolean, change?: AccessChange) => {
      setAccessChange(ready && change ? change : null);
    },
    [],
  );

  // The picker owns the draft; we own `accessChange`. Reset it on every open and
  // close so a previous selection can never leak into the next dialog session.
  const setAccessDialogOpen = useCallback((open: boolean) => {
    setAccessChange(null);
    setConfirmAccess(open);
  }, []);

  const allManageable = rows.every((r) => r.canManage);
  // Runtime switch and env injection are gated on canManage (agent owner or
  // workspace admin) rather than isOwnedByMe — that is the rule the server
  // applies to both. Non-manageable rows are dropped from the hand-off rather
  // than sent and skipped, so the dialog's counts describe what will actually
  // happen. Archived rows are excluded for the same reason the row menu hides
  // these actions on them.
  const manageableActiveRows = rows.filter(
    (r) => r.canManage && !r.agent.archived_at,
  );
  const anyManageableActive = manageableActiveRows.length > 0;
  // Same gate the row menu applies to its stop item: manageable, active, and
  // actually holding running or queued work — cancelling an idle agent's tasks
  // is a no-op the user should not be offered.
  const stoppableRows = manageableActiveRows.filter(
    (r) =>
      (r.presence?.runningCount ?? 0) + (r.presence?.queuedCount ?? 0) > 0,
  );
  const ownedRows = rows.filter((r) => r.isOwnedByMe);
  const anyOwned = ownedRows.length > 0;
  const anyActive = rows.some((r) => !r.agent.archived_at);
  const anyArchived = rows.some((r) => !!r.agent.archived_at);

  const invalidate = () =>
    qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });

  const accessConfirmEnabled = accessChange !== null;

  useEffect(() => {
    if (rows.length > 0) return;
    setConfirmArchive(false);
    setConfirmCancel(false);
    setConfirmAccess(false);
    setAccessChange(null);
  }, [rows.length]);

  const applyAccessBulk = async (change: AccessChange) => {
    const summary = await runBatch(
      (id) =>
        api.updateAgent(id, {
          permission_mode: change.permission_mode,
          invocation_targets: change.invocation_targets,
        }),
      ownedRows,
    );
    if (summary.failed > 0) {
      toast.error(
        t(($) => $.row_actions.set_access_bulk_partial, {
          succeeded: summary.succeeded,
          failed: summary.failed,
        }),
      );
    }
  };

  // Not routed through runBatch: the success toast reports the summed number
  // of cancelled tasks, which needs each call's `{ cancelled }` payload.
  const applyCancelTasksBulk = async () => {
    setBusy(true);
    const settled = await Promise.allSettled(
      stoppableRows.map((row) => api.cancelAgentTasks(row.agent.id)),
    );
    const cancelled = settled.reduce(
      (n, s) => (s.status === "fulfilled" ? n + s.value.cancelled : n),
      0,
    );
    const firstFailure = settled.find((s) => s.status === "rejected") as
      | PromiseRejectedResult
      | undefined;
    invalidate();
    onClear();
    setBusy(false);
    setConfirmCancel(false);
    if (firstFailure) {
      toast.error(
        firstFailure.reason instanceof Error
          ? firstFailure.reason.message
          : String(firstFailure.reason),
      );
      return;
    }
    toast.success(
      cancelled === 0
        ? t(($) => $.row_actions.no_tasks_to_cancel_toast)
        : t(($) => $.row_actions.cancelled_tasks_toast, { count: cancelled }),
    );
  };

  const runBatch = async (
    fn: (id: string) => Promise<unknown>,
    targets: AgentListRow[],
  ): Promise<{ succeeded: number; failed: number }> => {
    setBusy(true);
    const settled = await Promise.allSettled(
      targets.map((row) => fn(row.agent.id)),
    );
    const failed = settled.filter((s) => s.status === "rejected").length;
    const succeeded = settled.length - failed;
    invalidate();
    onClear();
    setBusy(false);
    if (failed > 0) {
      const first = settled.find((s) => s.status === "rejected") as
        | PromiseRejectedResult
        | undefined;
      if (first) {
        toast.error(first.reason instanceof Error ? first.reason.message : String(first.reason));
      }
    }
    return { succeeded, failed };
  };

  return (
    <>
      <AnimatePresence initial={false}>
        {rows.length > 0 && (
          <div
            key="agent-batch-toolbar"
            className="absolute bottom-6 left-1/2 z-50 -translate-x-1/2 max-md:above-chat-launcher"
          >
            <motion.div
              className="flex items-center gap-1 rounded-lg border bg-background px-2 py-1.5 shadow-lg"
              initial={{
                opacity: 0,
                transform: shouldReduceMotion
                  ? "translateY(0)"
                  : "translateY(8px)",
              }}
              animate={{
                opacity: 1,
                transform: "translateY(0)",
                transition: {
                  duration: UI_MOTION_DURATION.fast,
                  ease: UI_EASE_OUT,
                },
              }}
              exit={{
                opacity: 0,
                transform: shouldReduceMotion
                  ? "translateY(0)"
                  : "translateY(8px)",
                transition: {
                  duration: shouldReduceMotion
                    ? UI_MOTION_DURATION.fast
                    : UI_MOTION_DURATION.micro,
                  ease: UI_EASE_OUT,
                },
              }}
            >
        <div className="mr-1 flex items-center gap-1.5 border-r pl-1 pr-2">
          <span className="text-body font-medium">
            {t(($) => $.actions.selected, { count: rows.length })}
          </span>
          <button
            type="button"
            aria-label={t(($) => $.actions.clear_selection)}
            onClick={onClear}
            className="rounded p-0.5 transition-colors hover:bg-accent"
          >
            <X className="size-3.5 text-muted-foreground" />
          </button>
        </div>

        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="ghost" size="sm" disabled={busy}>
                {busy ? (
                  <Loader2 className="mr-1 size-3.5 animate-spin" />
                ) : null}
                {t(($) => $.actions.menu)}
                <ChevronUp className="ml-1 size-3.5 text-muted-foreground" />
              </Button>
            }
          />
          {/* Opens upward: the bar is anchored to the bottom of the list. */}
          <DropdownMenuContent side="top" align="end" sideOffset={8}>
            {anyActive && (
              <DropdownMenuItem
                disabled={stoppableRows.length === 0}
                onClick={() => setConfirmCancel(true)}
              >
                <Square className="h-3.5 w-3.5" />
                {t(($) => $.row_actions.cancel_all_tasks)}
              </DropdownMenuItem>
            )}
            {anyActive && (
              <DropdownMenuItem
                disabled={!anyManageableActive}
                onClick={() => onSwitchRuntime(manageableActiveRows)}
              >
                <MonitorCog className="h-3.5 w-3.5" />
                {t(($) => $.row_actions.switch_runtime)}
              </DropdownMenuItem>
            )}
            {anyActive && (
              <DropdownMenuItem
                disabled={!anyManageableActive}
                onClick={() => onInjectEnv(manageableActiveRows)}
              >
                <KeyRound className="h-3.5 w-3.5" />
                {t(($) => $.row_actions.inject_env)}
              </DropdownMenuItem>
            )}
            {anyActive && (
              <DropdownMenuItem
                disabled={!anyOwned}
                onClick={() => setAccessDialogOpen(true)}
              >
                <Users className="h-3.5 w-3.5" />
                {t(($) => $.row_actions.set_access)}
              </DropdownMenuItem>
            )}
            {anyArchived && (
              <DropdownMenuItem
                disabled={!allManageable}
                onClick={() =>
                  runBatch(
                    (id) => api.restoreAgent(id),
                    rows.filter((r) => !!r.agent.archived_at),
                  )
                }
              >
                <ArchiveRestore className="h-3.5 w-3.5" />
                {t(($) => $.row_actions.restore)}
              </DropdownMenuItem>
            )}
            {/* Archive sits last behind a separator: it is the destructive
                action, kept furthest from the routine ones. */}
            {anyActive && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  variant="destructive"
                  disabled={!allManageable}
                  onClick={() => setConfirmArchive(true)}
                >
                  <Archive className="h-3.5 w-3.5" />
                  {t(($) => $.row_actions.archive)}
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
            </motion.div>
          </div>
        )}
      </AnimatePresence>

      {/* Archive confirm dialog */}
      {rows.length > 0 && <Dialog open={confirmArchive} onOpenChange={setConfirmArchive}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {t(($) => $.row_actions.archive_dialog_title, {
                name:
                  rows.length === 1 && rows[0]
                    ? rows[0].agent.name
                    : String(rows.length),
              })}
            </DialogTitle>
            <DialogDescription>
              {t(($) => $.row_actions.archive_dialog_description)}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={() => setConfirmArchive(false)}
            >
              {t(($) => $.row_actions.archive_dialog_cancel)}
            </Button>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              disabled={busy}
              onClick={async () => {
                await runBatch(
                  (id) => api.archiveAgent(id),
                  rows.filter((r) => !r.agent.archived_at),
                );
                setConfirmArchive(false);
              }}
            >
              {busy ? (
                <Loader2 className="mr-1 size-3.5 animate-spin" />
              ) : null}
              {t(($) => $.row_actions.archive_dialog_confirm)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>}

      {/* Bulk cancel-tasks confirm dialog. Counts stoppable agents, not
          selected rows: it states what the confirm button will actually do. */}
      {rows.length > 0 && <Dialog open={confirmCancel} onOpenChange={setConfirmCancel}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {t(($) => $.row_actions.cancel_dialog_title_bulk, {
                count: stoppableRows.length,
              })}
            </DialogTitle>
            <DialogDescription>
              {t(($) => $.row_actions.cancel_dialog_running_note)}
              {t(($) => $.row_actions.cancel_dialog_irreversible)}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={() => setConfirmCancel(false)}
            >
              {t(($) => $.row_actions.cancel_dialog_keep)}
            </Button>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              disabled={busy}
              onClick={applyCancelTasksBulk}
            >
              {busy ? (
                <Loader2 className="mr-1 size-3.5 animate-spin" />
              ) : null}
              {t(($) => $.row_actions.cancel_dialog_confirm)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>}

      {/* Bulk access dialog — AccessPicker's internal Save is hidden (hideFooter)
          so this dialog's Confirm button is the sole apply trigger via onChange.
          a11y: focus trap + restore via Dialog; aria-live summary; accessible name. */}
      {rows.length > 0 && <Dialog open={confirmAccess} onOpenChange={setAccessDialogOpen}>
        <DialogContent
          className="sm:max-w-md"
          aria-describedby="bulk-access-summary"
        >
          <DialogHeader>
            <DialogTitle>
              {t(($) => $.row_actions.set_access_dialog_title)}
            </DialogTitle>
            <DialogDescription id="bulk-access-summary">
              <span aria-live="polite">
                {t(($) => $.row_actions.set_access_applies_to, {
                  count: ownedRows.length,
                })}
                {rows.length > ownedRows.length
                  ? ` ${t(($) => $.row_actions.set_access_skipped, { count: rows.length - ownedRows.length })}`
                  : ""}
              </span>
            </DialogDescription>
          </DialogHeader>
          <AccessPicker
            permissionMode="private"
            invocationTargets={undefined}
            visibility="private"
            members={members}
            ownerId={currentUserId}
            canEdit
            hideFooter
            onReadyChange={handleAccessReadyChange}
          />
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={() => setAccessDialogOpen(false)}
            >
              {t(($) => $.row_actions.archive_dialog_cancel)}
            </Button>
            <Button
              type="button"
              size="sm"
              disabled={busy || !accessConfirmEnabled}
              onClick={async () => {
                if (!accessChange) return;
                const change = accessChange;
                setAccessDialogOpen(false);
                await applyAccessBulk(change);
              }}
            >
              {busy ? (
                <Loader2 className="mr-1 size-3.5 animate-spin" />
              ) : null}
              {t(($) => $.row_actions.set_access_dialog_confirm)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>}
    </>
  );
}
