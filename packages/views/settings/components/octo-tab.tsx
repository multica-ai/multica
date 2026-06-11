"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
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
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { octoInstallationsOptions, octoKeys } from "@multica/core/octo";
import { api, ApiError } from "@multica/core/api";
import type { OctoInstallation } from "@multica/core/types";
import { useT } from "../../i18n";

// OctoTab is the workspace settings panel for Octo IM bot installations.
// Listing is member-visible; configure / disconnect are admin-only (the backend
// enforces it via RequireWorkspaceRole; the UI hides the actions to match).
//
// Unlike Lark (device-flow QR scan), Octo configures with a single bf_* bot
// token, so the "Configure a bot" dialog is just an agent id + token form.
export function OctoTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);

  const { data: listing, isLoading } = useQuery({
    ...octoInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: !!wsId,
  });

  const isAdmin = members.some(
    (m) => m.user_id === user?.id && (m.role === "owner" || m.role === "admin"),
  );

  const installations = listing?.installations ?? [];
  const configured = listing?.configured === true;

  const [configureOpen, setConfigureOpen] = useState(false);
  const [revokeTarget, setRevokeTarget] = useState<OctoInstallation | null>(null);

  const refresh = () => {
    if (wsId) qc.invalidateQueries({ queryKey: octoKeys.installations(wsId) });
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium">{t(($) => $.octo.section_title)}</h3>
          <p className="text-sm text-muted-foreground">{t(($) => $.octo.description)}</p>
        </div>
        {isAdmin && configured && (
          <Button size="sm" onClick={() => setConfigureOpen(true)}>
            {t(($) => $.octo.configure)}
          </Button>
        )}
      </div>

      {!configured && (
        <Card>
          <CardContent className="py-6 text-sm text-muted-foreground">
            {t(($) => $.octo.not_configured)}
          </CardContent>
        </Card>
      )}

      {configured && isLoading && (
        <Card>
          <CardContent className="py-6 text-sm text-muted-foreground">{t(($) => $.octo.loading)}</CardContent>
        </Card>
      )}

      {configured && !isLoading && installations.length === 0 && (
        <Card>
          <CardContent className="py-6 text-sm text-muted-foreground">{t(($) => $.octo.empty)}</CardContent>
        </Card>
      )}

      {installations.map((inst) => (
        <Card key={inst.id}>
          <CardContent className="flex items-center justify-between py-4">
            <div className="min-w-0">
              <div className="truncate text-sm font-medium">{inst.bot_name || inst.robot_id}</div>
              <div className="truncate text-xs text-muted-foreground">
                {t(($) => $.octo.status_label)}: {inst.status}
              </div>
            </div>
            {isAdmin && (
              <Button
                size="icon"
                variant="ghost"
                aria-label={t(($) => $.octo.disconnect)}
                onClick={() => setRevokeTarget(inst)}
              >
                <Trash2 className="size-4" />
              </Button>
            )}
          </CardContent>
        </Card>
      ))}

      <ConfigureDialog
        open={configureOpen}
        onOpenChange={setConfigureOpen}
        wsId={wsId}
        onConfigured={() => {
          setConfigureOpen(false);
          refresh();
        }}
      />

      <AlertDialog open={!!revokeTarget} onOpenChange={(o) => !o && setRevokeTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.octo.disconnect_title)}</AlertDialogTitle>
            <AlertDialogDescription>{t(($) => $.octo.disconnect_body)}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.octo.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              onClick={async () => {
                const target = revokeTarget;
                setRevokeTarget(null);
                if (!target || !wsId) return;
                try {
                  await api.deleteOctoInstallation(wsId, target.id);
                  toast.success(t(($) => $.octo.disconnected));
                  refresh();
                } catch (err) {
                  toast.error(err instanceof ApiError ? err.message : t(($) => $.octo.disconnect_failed));
                }
              }}
            >
              {t(($) => $.octo.disconnect)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function ConfigureDialog({
  open,
  onOpenChange,
  wsId,
  onConfigured,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  wsId: string;
  onConfigured: () => void;
}) {
  const { t } = useT("settings");
  const [agentId, setAgentId] = useState("");
  const [botToken, setBotToken] = useState("");
  const [apiUrl, setApiUrl] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const submit = async () => {
    if (!agentId.trim() || !botToken.trim()) return;
    setSubmitting(true);
    try {
      await api.createOctoInstallation(wsId, {
        agent_id: agentId.trim(),
        bot_token: botToken.trim(),
        api_url: apiUrl.trim() || undefined,
      });
      toast.success(t(($) => $.octo.configured));
      setAgentId("");
      setBotToken("");
      setApiUrl("");
      onConfigured();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : t(($) => $.octo.configure_failed));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.octo.configure)}</DialogTitle>
          <DialogDescription>{t(($) => $.octo.configure_desc)}</DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="space-y-1">
            <label className="text-xs font-medium">{t(($) => $.octo.agent_id)}</label>
            <Input value={agentId} onChange={(e) => setAgentId(e.target.value)} placeholder="agent uuid" />
          </div>
          <div className="space-y-1">
            <label className="text-xs font-medium">{t(($) => $.octo.bot_token)}</label>
            <Input value={botToken} onChange={(e) => setBotToken(e.target.value)} placeholder="bf_…" />
          </div>
          <div className="space-y-1">
            <label className="text-xs font-medium">{t(($) => $.octo.api_url)}</label>
            <Input value={apiUrl} onChange={(e) => setApiUrl(e.target.value)} placeholder="https://…/api" />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t(($) => $.octo.cancel)}
          </Button>
          <Button disabled={submitting || !agentId.trim() || !botToken.trim()} onClick={submit}>
            {submitting ? t(($) => $.octo.saving) : t(($) => $.octo.configure)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
