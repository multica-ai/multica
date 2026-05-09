"use client";

import { useEffect, useState } from "react";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useUpsertDeployEnvironment } from "@multica/core/ship";
import type {
  CreateDeployEnvironmentRequest,
  DeployEnvironment,
  DeployEnvironmentKind,
} from "@multica/core/types";
import { useT } from "../../i18n";

interface ConfigureDeployEnvDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projectId: string;
  /** When provided, the dialog edits this environment instead of creating
   *  a new one. The backend upsert is keyed on (project, kind), so editing
   *  the kind is effectively a "move + create" — we lock the kind in edit
   *  mode to keep the UX honest. */
  existing?: DeployEnvironment;
}

export function ConfigureDeployEnvDialog({
  open,
  onOpenChange,
  projectId,
  existing,
}: ConfigureDeployEnvDialogProps) {
  const { t } = useT("ship");
  const upsert = useUpsertDeployEnvironment(projectId);

  const [kind, setKind] = useState<DeployEnvironmentKind>(
    existing?.kind ?? "staging",
  );
  const [name, setName] = useState(existing?.name ?? "");
  const [branch, setBranch] = useState(existing?.target_branch ?? "main");
  const [url, setUrl] = useState(existing?.target_url ?? "");
  const [autoPromote, setAutoPromote] = useState(existing?.auto_promote ?? false);
  const [error, setError] = useState<string | null>(null);

  // Reset form whenever the dialog opens — different `existing` values
  // share the same component instance.
  useEffect(() => {
    if (!open) return;
    setKind(existing?.kind ?? "staging");
    setName(existing?.name ?? "");
    setBranch(existing?.target_branch ?? "main");
    setUrl(existing?.target_url ?? "");
    setAutoPromote(existing?.auto_promote ?? false);
    setError(null);
  }, [open, existing]);

  const handleSubmit = async () => {
    const trimmedName = name.trim();
    if (!trimmedName) {
      setError(t(($) => $.configure_dialog.name_required));
      return;
    }
    setError(null);
    const payload: CreateDeployEnvironmentRequest = {
      kind,
      name: trimmedName,
      target_branch: branch.trim() || null,
      target_url: url.trim() || null,
      auto_promote: autoPromote,
    };
    try {
      await upsert.mutateAsync(payload);
      onOpenChange(false);
    } catch (e) {
      toast.error(
        e instanceof Error
          ? e.message
          : t(($) => $.configure_dialog.save_failed),
      );
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {existing
              ? t(($) => $.configure_dialog.title_edit)
              : t(($) => $.configure_dialog.title_create)}
          </DialogTitle>
          <DialogDescription>
            {t(($) => $.configure_dialog.description)}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">
              {t(($) => $.configure_dialog.kind_label)}
            </Label>
            <Select
              value={kind}
              onValueChange={(v) => {
                if (v) setKind(v as DeployEnvironmentKind);
              }}
              disabled={!!existing}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="staging">
                  {t(($) => $.configure_dialog.kind_staging)}
                </SelectItem>
                <SelectItem value="production">
                  {t(($) => $.configure_dialog.kind_production)}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground" htmlFor="ship-env-name">
              {t(($) => $.configure_dialog.name_label)}
            </Label>
            <Input
              id="ship-env-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t(($) => $.configure_dialog.name_placeholder)}
            />
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground" htmlFor="ship-env-branch">
              {t(($) => $.configure_dialog.branch_label)}
            </Label>
            <Input
              id="ship-env-branch"
              value={branch}
              onChange={(e) => setBranch(e.target.value)}
              placeholder={t(($) => $.configure_dialog.branch_placeholder)}
            />
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground" htmlFor="ship-env-url">
              {t(($) => $.configure_dialog.url_label)}
            </Label>
            <Input
              id="ship-env-url"
              type="url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder={t(($) => $.configure_dialog.url_placeholder)}
            />
          </div>

          <div className="flex items-start justify-between gap-3 rounded-md border bg-muted/20 p-3">
            <div>
              <p className="text-sm font-medium">
                {t(($) => $.configure_dialog.auto_promote_label)}
              </p>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.configure_dialog.auto_promote_hint)}
              </p>
            </div>
            <Switch
              checked={autoPromote}
              onCheckedChange={setAutoPromote}
              aria-label={t(($) => $.configure_dialog.auto_promote_label)}
            />
          </div>

          {error && (
            <p className="text-xs text-destructive" role="alert">
              {error}
            </p>
          )}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t(($) => $.configure_dialog.cancel)}
          </Button>
          <Button onClick={handleSubmit} disabled={upsert.isPending}>
            {upsert.isPending
              ? t(($) => $.configure_dialog.saving)
              : t(($) => $.configure_dialog.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
