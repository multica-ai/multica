"use client";

import { useCallback, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { MemberWithUser } from "@multica/core/types";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Archive, ArchiveRestore, Loader2, X } from "lucide-react";
import { useT } from "../../i18n";
import { AccessPicker, type AccessChange } from "./inspector/access-picker";
import type { AgentListRow } from "./agents-page";

/**
 * Floating batch-toolbar for the agents list page. Renders archive/restore
 * actions (existing) and a "Set access scope" action (new, MUL-4302 / 2026-07-14)
 * that opens a single confirmation dialog with an embedded AccessPicker.
 *
 * The bulk action is gated by `isOwnedByMe` (not `canManage`) to match the
 * backend's owner-only write gate for `permission_mode` / `invocation_targets`.
 * Non-owned selected agents are skipped; the skip count is shown in both the
 * dialog summary and the partial-failure toast.
 */
export function AgentBatchToolbar({
  rows,
  members,
  currentUserId,
  onClear,
}: {
  rows: AgentListRow[];
  members: MemberWithUser[];
  currentUserId: string | null;
  onClear: () => void;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [confirmArchive, setConfirmArchive] = useState(false);
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

  if (rows.length === 0) return null;

  const allManageable = rows.every((r) => r.canManage);
  const ownedRows = rows.filter((r) => r.isOwnedByMe);
  const anyOwned = ownedRows.length > 0;
  const anyActive = rows.some((r) => !r.agent.archived_at);
  const anyArchived = rows.some((r) => !!r.agent.archived_at);

  const invalidate = () =>
    qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });

  const accessConfirmEnabled = accessChange !== null;

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
      <div className="absolute bottom-6 left-1/2 z-50 flex -translate-x-1/2 items-center gap-1 rounded-lg border bg-background px-2 py-1.5 shadow-lg">
        <div className="mr-1 flex items-center gap-1.5 border-r pl-1 pr-2">
          <span className="text-sm font-medium">
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

        {anyArchived && (
          <Button
            variant="ghost"
            size="sm"
            disabled={!allManageable || busy}
            onClick={() =>
              runBatch(
                (id) => api.restoreAgent(id),
                rows.filter((r) => !!r.agent.archived_at),
              )
            }
          >
            <ArchiveRestore className="mr-1 size-3.5" />
            {t(($) => $.row_actions.restore)}
          </Button>
        )}
        {anyActive && (
          <Button
            variant="ghost"
            size="sm"
            disabled={!anyOwned || busy}
            onClick={() => setAccessDialogOpen(true)}
          >
            {t(($) => $.row_actions.set_access)}
          </Button>
        )}
        {/* Archive sits last: it is the destructive action, kept furthest from
            the other batch actions. */}
        {anyActive && (
          <Button
            variant="ghost"
            size="sm"
            disabled={!allManageable || busy}
            onClick={() => setConfirmArchive(true)}
          >
            <Archive className="mr-1 size-3.5" />
            {t(($) => $.row_actions.archive)}
          </Button>
        )}
      </div>

      {/* Archive confirm dialog */}
      <Dialog open={confirmArchive} onOpenChange={setConfirmArchive}>
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
      </Dialog>

      {/* Bulk access dialog — AccessPicker's internal Save is hidden (hideFooter)
          so this dialog's Confirm button is the sole apply trigger via onChange.
          a11y: focus trap + restore via Dialog; aria-live summary; accessible name. */}
      <Dialog open={confirmAccess} onOpenChange={setAccessDialogOpen}>
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
      </Dialog>
    </>
  );
}
