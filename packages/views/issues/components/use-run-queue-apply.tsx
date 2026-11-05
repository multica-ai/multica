"use client";

import { useState } from "react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useBatchSetRunQueue, useSetIssueRunQueue } from "@multica/core/issues/mutations";
import type { RunQueuePreview, RunQueueSetting } from "@multica/core/types";
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
import { useT } from "../../i18n";

interface Pending {
  ids: string[];
  queue: RunQueueSetting;
  preview: RunQueuePreview;
  onDone?: () => void;
  onCancel?: () => void;
}

interface ApplyCallbacks {
  /** Runs after the write succeeded. */
  onDone?: () => void;
  /** Runs when the write failed or the person declined the confirmation. */
  onCancel?: () => void;
}

/**
 * Writes run-queue settings. When the change would start agent runs at once it
 * asks first, the way a manual move to Todo does; taking issues out of the
 * queue, or waiting on unfinished work, applies directly.
 */
export function useRunQueueApply() {
  const { t } = useT("issues");
  const setOne = useSetIssueRunQueue();
  const setMany = useBatchSetRunQueue();
  const [pending, setPending] = useState<Pending | null>(null);

  const write = async (ids: string[], queue: RunQueueSetting, cb: ApplyCallbacks) => {
    try {
      if (ids.length === 1) await setOne.mutateAsync({ id: ids[0]!, queue });
      else await setMany.mutateAsync({ ids, queue });
      cb.onDone?.();
    } catch (err) {
      toast.error(err instanceof Error && err.message ? err.message : t(($) => $.batch.update_failed));
      cb.onCancel?.();
    }
  };

  const apply = async (ids: string[], queue: RunQueueSetting, cb: ApplyCallbacks = {}) => {
    if (ids.length === 0) return;
    if (queue.queued) {
      // A failed preview must not block the write; the person still chose the waits.
      const preview = await api.previewRunQueue(ids, queue).catch(() => null);
      if (preview && preview.run_count > 0) {
        setPending({ ids, queue, preview, ...cb });
        return;
      }
    }
    await write(ids, queue, cb);
  };

  const dialog = (
    <AlertDialog
      open={pending !== null}
      onOpenChange={(open) => {
        if (open) return;
        pending?.onCancel?.();
        setPending(null);
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.run_queue.confirm_title)}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.run_queue.confirm_desc, {
              count: pending?.preview.release_now ?? 0,
              runs: pending?.preview.run_count ?? 0,
            })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t(($) => $.batch.cancel)}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              const current = pending;
              setPending(null);
              if (current) void write(current.ids, current.queue, current);
            }}
          >
            {t(($) => $.run_queue.confirm_action)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );

  return { apply, dialog, isPending: setOne.isPending || setMany.isPending };
}
