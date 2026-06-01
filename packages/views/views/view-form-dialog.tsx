"use client";

import { useEffect, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { Button } from "@multica/ui/components/ui/button";
import { isImeComposing } from "@multica/core/utils";
import { useT } from "../i18n";

export interface ViewFormValues {
  name: string;
  shared: boolean;
}

/**
 * Name (+ shared, create only) dialog shared by the "save view" and "rename"
 * flows. Local state resets every time the dialog opens so a cancelled edit
 * never bleeds into the next open. `shared` is only collected on create; rename
 * keeps the view's existing visibility (toggled separately from the menu).
 */
export function ViewFormDialog({
  open,
  mode,
  initialName = "",
  initialShared = false,
  busy = false,
  onSubmit,
  onOpenChange,
}: {
  open: boolean;
  mode: "create" | "rename";
  initialName?: string;
  initialShared?: boolean;
  busy?: boolean;
  onSubmit: (values: ViewFormValues) => void;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("common");
  const [name, setName] = useState(initialName);
  const [shared, setShared] = useState(initialShared);

  useEffect(() => {
    if (open) {
      setName(initialName);
      setShared(initialShared);
    }
  }, [open, initialName, initialShared]);

  const trimmed = name.trim();
  const canSubmit = trimmed.length > 0 && !busy;

  const submit = () => {
    if (!canSubmit) return;
    onSubmit({ name: trimmed, shared });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {mode === "create"
              ? t(($) => $.views.save_view)
              : t(($) => $.views.rename_view)}
          </DialogTitle>
        </DialogHeader>

        <div className="flex flex-col gap-4 py-1">
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (isImeComposing(e)) return;
              if (e.key === "Enter") {
                e.preventDefault();
                submit();
              }
            }}
            placeholder={t(($) => $.views.view_name_placeholder)}
            maxLength={80}
            autoFocus
            disabled={busy}
            autoComplete="off"
          />

          {mode === "create" && (
            <Label className="flex items-center justify-between gap-3 text-sm font-normal">
              <span className="flex flex-col">
                <span>{t(($) => $.views.shared)}</span>
                <span className="text-xs text-muted-foreground">
                  {t(($) => $.views.shared_hint)}
                </span>
              </span>
              <Switch checked={shared} onCheckedChange={setShared} disabled={busy} />
            </Label>
          )}
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={busy}
          >
            {t(($) => $.cancel)}
          </Button>
          <Button type="button" onClick={submit} disabled={!canSubmit}>
            {mode === "create" ? t(($) => $.views.create) : t(($) => $.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
