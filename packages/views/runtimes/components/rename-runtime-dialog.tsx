"use client";

import { useEffect, useState } from "react";
import { toast } from "sonner";
import type { AgentRuntime } from "@multica/core/types";
import { useUpdateRuntime } from "@multica/core/runtimes/mutations";
import {
  AlertDialog,
  AlertDialogContent,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { useT } from "../../i18n";

// RenameRuntimeDialog lets a runtime owner (or workspace admin) set a custom
// display name (MUL-4217). The name overrides the daemon-proposed default for
// display everywhere; clearing it reverts to the default. When the runtime is
// a local/remote daemon (has a daemon_id), it can host one runtime per
// provider, so the dialog offers to apply the name to the whole machine — the
// common intent behind "name my machine".
export interface RenameRuntimeDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  runtime: AgentRuntime;
  wsId: string;
}

export function RenameRuntimeDialog({
  open,
  onOpenChange,
  runtime,
  wsId,
}: RenameRuntimeDialogProps) {
  const { t } = useT("runtimes");
  const updateRuntime = useUpdateRuntime(wsId);

  // Machine-level naming only makes sense for daemon-backed runtimes; cloud
  // workers have no shared host to fan out across.
  const canApplyToMachine =
    !!runtime.daemon_id && runtime.runtime_mode !== "cloud";

  const [value, setValue] = useState(runtime.custom_name ?? "");
  const [applyToMachine, setApplyToMachine] = useState(canApplyToMachine);
  const submitting = updateRuntime.isPending;

  // Reset the form each time the dialog opens so a cancelled edit doesn't
  // leak into the next one.
  useEffect(() => {
    if (open) {
      setValue(runtime.custom_name ?? "");
      setApplyToMachine(canApplyToMachine);
    }
  }, [open, runtime.custom_name, canApplyToMachine]);

  const handleOpenChange = (next: boolean) => {
    if (submitting) return;
    onOpenChange(next);
  };

  const handleSave = () => {
    const trimmed = value.trim();
    updateRuntime.mutate(
      {
        runtimeId: runtime.id,
        // Empty string clears the override server-side.
        patch: {
          custom_name: trimmed,
          ...(canApplyToMachine && applyToMachine
            ? { apply_to_machine: true }
            : {}),
        },
      },
      {
        onSuccess: () => {
          toast.success(
            trimmed
              ? t(($) => $.detail.rename_dialog.toast_saved)
              : t(($) => $.detail.rename_dialog.toast_cleared),
          );
          onOpenChange(false);
        },
        onError: (err) =>
          toast.error(
            err instanceof Error && err.message
              ? err.message
              : t(($) => $.detail.rename_dialog.toast_failed),
          ),
      },
    );
  };

  return (
    <AlertDialog open={open} onOpenChange={handleOpenChange}>
      <AlertDialogContent
        className="w-[calc(100vw-2rem)] !max-w-[440px] gap-0 overflow-hidden rounded-lg p-0"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="px-5 pb-4 pt-5">
          <h2 className="text-base font-semibold">
            {t(($) => $.detail.rename_dialog.title)}
          </h2>
          <p className="mt-1 text-sm leading-5 text-muted-foreground">
            {t(($) => $.detail.rename_dialog.description)}
          </p>

          <Input
            className="mt-3"
            autoFocus
            value={value}
            maxLength={100}
            placeholder={runtime.name}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !submitting) {
                e.preventDefault();
                handleSave();
              }
            }}
          />
          <p className="mt-1.5 text-xs text-muted-foreground">
            {t(($) => $.detail.rename_dialog.hint)}
          </p>

          {canApplyToMachine && (
            <label className="mt-3 flex cursor-pointer items-start gap-2 text-sm text-foreground">
              <Checkbox
                className="mt-0.5"
                checked={applyToMachine}
                onCheckedChange={(next) => setApplyToMachine(next === true)}
                disabled={submitting}
              />
              <span className="leading-5">
                {t(($) => $.detail.rename_dialog.apply_to_machine)}
              </span>
            </label>
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
              {t(($) => $.detail.rename_dialog.cancel)}
            </Button>
            <Button
              type="button"
              className="w-full sm:w-auto"
              onClick={handleSave}
              disabled={submitting}
            >
              {submitting
                ? t(($) => $.detail.rename_dialog.saving)
                : t(($) => $.detail.rename_dialog.save)}
            </Button>
          </div>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  );
}
