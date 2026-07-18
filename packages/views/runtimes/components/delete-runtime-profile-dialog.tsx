"use client";

import { useEffect, useState } from "react";
import { AlertTriangle } from "lucide-react";
import { toast } from "sonner";
import type { RuntimeProfile } from "@multica/core/types";
import {
  parseRuntimeProfileBoundConflict,
  useDeleteRuntimeProfile,
} from "@multica/core/runtimes";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

// Confirmation dialog for deleting a custom runtime profile. The server
// refuses with a 409 when agents are still bound to the profile; we surface
// that refusal inline (and keep the dialog open) instead of dumping a raw
// error toast, so the admin can read why and back out gracefully.
export function DeleteRuntimeProfileDialog({
  open,
  onOpenChange,
  profile,
  wsId,
  onDeleted,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  profile: RuntimeProfile;
  wsId: string;
  onDeleted: () => void;
}) {
  const { t } = useT("runtimes");
  const deleteProfile = useDeleteRuntimeProfile(wsId);
  const [submitting, setSubmitting] = useState(false);
  // Server-issued "agents still bound" message, shown inline above the
  // actions. Reset whenever the dialog re-opens.
  const [boundMessage, setBoundMessage] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setSubmitting(false);
      setBoundMessage(null);
    }
  }, [open]);

  const handleOpenChange = (next: boolean) => {
    if (submitting) return;
    onOpenChange(next);
  };

  const handleConfirm = async () => {
    setSubmitting(true);
    setBoundMessage(null);
    try {
      await deleteProfile.mutateAsync(profile.id);
      toast.success(t(($) => $.profiles.delete_dialog.toast_deleted));
      onDeleted();
    } catch (err) {
      const conflict = parseRuntimeProfileBoundConflict(err);
      if (conflict) {
        // Prefer the server's specific wording; fall back to our localized
        // generic "still bound" copy when the body carried no message.
        setBoundMessage(
          conflict.message ||
            t(($) => $.profiles.delete_dialog.error_bound),
        );
        return;
      }
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.profiles.delete_dialog.error_generic),
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <AlertDialog open={open} onOpenChange={handleOpenChange}>
      <AlertDialogContent
        className="w-[calc(100vw-2rem)] !max-w-[440px] gap-0 overflow-hidden rounded-lg p-0"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="px-5 pb-4 pt-5">
          <AlertDialogTitle className="font-sans text-base font-semibold">
            {t(($) => $.profiles.delete_dialog.title)}
          </AlertDialogTitle>
          <AlertDialogDescription className="mt-1 text-left text-sm leading-5 text-pretty">
            {t(($) => $.profiles.delete_dialog.description, {
              name: profile.display_name,
            })}
          </AlertDialogDescription>
          {boundMessage && (
            <div
              role="alert"
              className="mt-3 flex items-start gap-2 rounded-md border border-warning/40 bg-warning/5 px-3 py-2 text-xs"
            >
              <AlertTriangle
                aria-hidden="true"
                className="mt-0.5 size-3.5 shrink-0 text-warning"
              />
              <span className="text-foreground">{boundMessage}</span>
            </div>
          )}
        </div>
        <div className="border-t bg-muted/25 px-5 py-3">
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            <Button
              type="button"
              variant="outline"
              className="w-full sm:w-auto"
              onClick={() => handleOpenChange(false)}
              disabled={submitting}
            >
              {t(($) => $.profiles.delete_dialog.cancel)}
            </Button>
            <Button
              type="button"
              variant="destructive"
              className="w-full sm:w-auto"
              onClick={handleConfirm}
              disabled={submitting}
            >
              {submitting
                ? t(($) => $.profiles.delete_dialog.deleting)
                : t(($) => $.profiles.delete_dialog.confirm)}
            </Button>
          </div>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  );
}
