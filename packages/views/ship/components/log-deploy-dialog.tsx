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
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useLogDeploy } from "@multica/core/ship";
import type { DeployEnvironment, DeployStatus } from "@multica/core/types";
import { useT } from "../../i18n";

interface LogDeployDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  environment: DeployEnvironment;
}

const STATUSES: DeployStatus[] = [
  "pending",
  "in_progress",
  "succeeded",
  "failed",
  "rolled_back",
];

export function LogDeployDialog({
  open,
  onOpenChange,
  environment,
}: LogDeployDialogProps) {
  const { t } = useT("ship");
  const log = useLogDeploy(environment.id);

  const [ref, setRef] = useState(environment.target_branch);
  const [sha, setSha] = useState("");
  const [status, setStatus] = useState<DeployStatus>("succeeded");
  const [logUrl, setLogUrl] = useState("");
  const [errorMessage, setErrorMessage] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setRef(environment.target_branch);
    setSha("");
    setStatus("succeeded");
    setLogUrl("");
    setErrorMessage("");
    setError(null);
  }, [open, environment]);

  const handleSubmit = async () => {
    const trimmedSha = sha.trim();
    if (!trimmedSha) {
      setError(t(($) => $.log_dialog.sha_required));
      return;
    }
    setError(null);
    try {
      await log.mutateAsync({
        ref: ref.trim() || undefined,
        sha: trimmedSha,
        status,
        log_url: logUrl.trim() || null,
        error_message: errorMessage.trim() || null,
      });
      onOpenChange(false);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.log_dialog.save_failed),
      );
    }
  };

  const statusLabel = (s: DeployStatus): string => {
    switch (s) {
      case "pending":
        return t(($) => $.log_dialog.status_pending);
      case "in_progress":
        return t(($) => $.log_dialog.status_in_progress);
      case "succeeded":
        return t(($) => $.log_dialog.status_succeeded);
      case "failed":
        return t(($) => $.log_dialog.status_failed);
      case "rolled_back":
        return t(($) => $.log_dialog.status_rolled_back);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.log_dialog.title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.log_dialog.description)}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground" htmlFor="ship-deploy-ref">
              {t(($) => $.log_dialog.ref_label)}
            </Label>
            <Input
              id="ship-deploy-ref"
              value={ref}
              onChange={(e) => setRef(e.target.value)}
              placeholder={t(($) => $.log_dialog.ref_placeholder)}
            />
            <p className="text-xs text-muted-foreground">
              {t(($) => $.log_dialog.ref_hint)}
            </p>
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground" htmlFor="ship-deploy-sha">
              {t(($) => $.log_dialog.sha_label)}
            </Label>
            <Input
              id="ship-deploy-sha"
              value={sha}
              onChange={(e) => setSha(e.target.value)}
              placeholder={t(($) => $.log_dialog.sha_placeholder)}
              className="font-mono"
            />
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">
              {t(($) => $.log_dialog.status_label)}
            </Label>
            <Select
              value={status}
              onValueChange={(v) => {
                if (v) setStatus(v as DeployStatus);
              }}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {STATUSES.map((s) => (
                  <SelectItem key={s} value={s}>
                    {statusLabel(s)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground" htmlFor="ship-deploy-log-url">
              {t(($) => $.log_dialog.log_url_label)}
            </Label>
            <Input
              id="ship-deploy-log-url"
              type="url"
              value={logUrl}
              onChange={(e) => setLogUrl(e.target.value)}
              placeholder={t(($) => $.log_dialog.log_url_placeholder)}
            />
          </div>

          {(status === "failed" || status === "rolled_back") && (
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground" htmlFor="ship-deploy-error">
                {t(($) => $.log_dialog.error_label)}
              </Label>
              <Textarea
                id="ship-deploy-error"
                value={errorMessage}
                onChange={(e) => setErrorMessage(e.target.value)}
                placeholder={t(($) => $.log_dialog.error_placeholder)}
                rows={3}
              />
            </div>
          )}

          {error && (
            <p className="text-xs text-destructive" role="alert">
              {error}
            </p>
          )}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t(($) => $.log_dialog.cancel)}
          </Button>
          <Button onClick={handleSubmit} disabled={log.isPending}>
            {log.isPending
              ? t(($) => $.log_dialog.saving)
              : t(($) => $.log_dialog.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
