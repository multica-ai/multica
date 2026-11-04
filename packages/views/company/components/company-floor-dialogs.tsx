"use client";

import { useState } from "react";
import { toast } from "sonner";
import {
  useCreateCompanyFloor,
  useUpdateCompanyFloor,
  type CompanyFloor,
} from "@multica/core/company";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { useT } from "../../i18n";

/** Creates a department (floor) when `floor` is null, otherwise renames `floor`. */
export function FloorNameDialog({
  floor,
  onClose,
  onSaved,
}: {
  floor: CompanyFloor | null;
  onClose: () => void;
  onSaved: (floor: CompanyFloor) => void;
}) {
  const { t } = useT("company");
  const { t: tc } = useT("common");
  const create = useCreateCompanyFloor();
  const update = useUpdateCompanyFloor();
  const [name, setName] = useState(floor?.name ?? "");
  const pending = create.isPending || update.isPending;
  const trimmed = name.trim();

  const submit = () => {
    if (!trimmed || pending) return;
    const done = (saved: CompanyFloor) => {
      onSaved(saved);
      onClose();
    };
    const failed = () => toast.error(t(($) => $.department_save_failed));
    if (floor) {
      update.mutate({ id: floor.id, name: trimmed }, { onSuccess: done, onError: failed });
    } else {
      create.mutate({ name: trimmed }, { onSuccess: done, onError: failed });
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>
            {floor ? t(($) => $.department_rename_title) : t(($) => $.department_create_title)}
          </DialogTitle>
        </DialogHeader>
        <form
          className="flex flex-col gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            submit();
          }}
        >
          <Label htmlFor="company-floor-name">{t(($) => $.department_name)}</Label>
          <Input
            id="company-floor-name"
            value={name}
            maxLength={60}
            autoFocus
            onChange={(event) => setName(event.target.value)}
          />
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              {tc(($) => $.cancel)}
            </Button>
            <Button type="submit" disabled={!trimmed || pending}>
              {tc(($) => $.save)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
