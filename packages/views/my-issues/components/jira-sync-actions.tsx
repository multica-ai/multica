"use client";

import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { RefreshCw, Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { issueKeys } from "@multica/core/issues/queries";
import { useT } from "../../i18n";
import { getJiraBridge, useJiraSync } from "../../settings/jira/use-jira-sync";

/** Sync / clear Jira issues straight from the My Issues header. Desktop-only —
 *  the Jira REST calls run in the Electron main process, so this renders nothing
 *  on web. Both actions invalidate the workspace issue queries afterwards so the
 *  list reflects the created / deleted issues immediately. Clearing goes through
 *  an explicit confirm dialog. Errors surface via a toast; the Settings → Jira
 *  tab remains the place for connection config. */
export function JiraSyncActions({ wsId }: { wsId: string }) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const { syncNow, clearSynced, running, clearing, error } = useJiraSync();
  const [confirmOpen, setConfirmOpen] = useState(false);

  useEffect(() => {
    if (error) toast.error(error);
  }, [error]);

  if (!getJiraBridge()) return null;

  const refresh = () => void qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });

  const onSync = async () => {
    const result = await syncNow();
    if (result) {
      refresh();
      toast.success(
        t(($) => $.jira.last_sync, {
          created: result.created,
          updated: result.updated,
          comments: result.commentsAdded,
        }),
      );
    }
  };

  const onConfirmClear = async () => {
    const result = await clearSynced();
    if (result) {
      refresh();
      toast.success(t(($) => $.jira.cleared, { deleted: result.deleted }));
    }
    setConfirmOpen(false);
  };

  return (
    <>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant="outline"
              size="sm"
              aria-label={t(($) => $.jira.sync_now)}
              disabled={running || clearing}
              onClick={() => void onSync()}
            >
              <RefreshCw className={running ? "size-3.5 animate-spin" : "size-3.5"} />
            </Button>
          }
        />
        <TooltipContent side="bottom">
          {running ? t(($) => $.jira.syncing) : t(($) => $.jira.sync_now)}
        </TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant="outline"
              size="sm"
              aria-label={t(($) => $.jira.clear_synced)}
              disabled={running || clearing}
              onClick={() => setConfirmOpen(true)}
            >
              <Trash2 className="size-3.5" />
            </Button>
          }
        />
        <TooltipContent side="bottom">{t(($) => $.jira.clear_synced)}</TooltipContent>
      </Tooltip>

      <AlertDialog
        open={confirmOpen}
        onOpenChange={(v) => {
          if (!v && !clearing) setConfirmOpen(false);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.jira.clear_synced)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.jira.clear_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={clearing}>
              {t(($) => $.jira.clear_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={() => void onConfirmClear()} disabled={clearing}>
              {clearing ? t(($) => $.jira.clearing) : t(($) => $.jira.clear_synced)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
