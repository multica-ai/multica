"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Copy, GitBranch, RefreshCw, Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
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
import { useWorkspaceId } from "@multica/core/hooks";
import { vcsConnectionsOptions } from "@multica/core/vcs";
import { api } from "@multica/core/api";
import type { ConnectVCSResponse, VCSProvider } from "@multica/core/types";
import { useT } from "../../i18n";

const PROVIDERS: VCSProvider[] = ["forgejo", "gitea", "gitlab"];
const PROVIDER_LABELS: Record<VCSProvider, string> = {
  forgejo: "Forgejo",
  gitea: "Gitea",
  gitlab: "GitLab",
};
const PROVIDER_OPTIONS = PROVIDERS.map((p) => ({
  value: p,
  label: PROVIDER_LABELS[p],
}));


export function VCSTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

  const { data } = useQuery(vcsConnectionsOptions(wsId));
  const connections = data?.connections ?? [];
  const configured = data?.configured === true;
  const canManage = data?.can_manage === true;

  const [provider, setProvider] = useState<VCSProvider>("forgejo");
  const [instanceUrl, setInstanceUrl] = useState("");
  const [token, setToken] = useState("");
  const [connecting, setConnecting] = useState(false);
  const [justConnected, setJustConnected] = useState<ConnectVCSResponse | null>(null);
  const [rotateTarget, setRotateTarget] = useState<string | null>(null);
  const [rotating, setRotating] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  async function handleConnect() {
    if (connecting || !instanceUrl.trim() || !token.trim()) return;
    setConnecting(true);
    try {
      const resp = await api.connectVCS(wsId, {
        provider,
        instance_url: instanceUrl.trim(),
        access_token: token.trim(),
      });
      await qc.invalidateQueries({ queryKey: ["vcs", wsId] });
      setJustConnected(resp);
      setInstanceUrl("");
      setToken("");
      toast.success(t(($) => $.vcs.toast_connected));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.vcs.toast_connect_failed));
    } finally {
      setConnecting(false);
    }
  }

  async function handleRotateWebhook() {
    if (!rotateTarget || rotating) return;
    setRotating(true);
    try {
      const resp = await api.rotateVCSWebhook(wsId, rotateTarget);
      await qc.invalidateQueries({ queryKey: ["vcs", wsId] });
      setJustConnected(resp);
      setRotateTarget(null);
      toast.success(t(($) => $.vcs.toast_rotated));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.vcs.toast_rotate_failed));
    } finally {
      setRotating(false);
    }
  }

  async function handleDelete() {
    if (!deleteTarget || deleting) return;
    setDeleting(true);
    try {
      await api.deleteVCSConnection(wsId, deleteTarget);
      await qc.invalidateQueries({ queryKey: ["vcs", wsId] });
      if (justConnected?.id === deleteTarget) setJustConnected(null);
      toast.success(t(($) => $.vcs.toast_disconnected));
      setDeleteTarget(null);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.vcs.toast_disconnect_failed));
    } finally {
      setDeleting(false);
    }
  }

  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      toast.success(t(($) => $.vcs.copied));
    } catch {
      toast.error(t(($) => $.vcs.copy_failed));
    }
  }

  return (
    <div className="space-y-6">
      <p className="text-sm text-muted-foreground">{t(($) => $.vcs.page_description)}</p>

      {connections.length > 0 && (
        <div className="space-y-3">
          {connections.map((c) => (
            <Card key={c.id}>
              <CardContent className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between sm:gap-4">
                <div className="flex min-w-0 items-start gap-3">
                  <div className="rounded-md border bg-muted/50 p-2 text-muted-foreground shrink-0">
                    <GitBranch className="h-4 w-4" />
                  </div>
                  <div className="min-w-0 space-y-0.5">
                    <p className="text-sm font-medium break-all">
                      {(PROVIDER_LABELS[c.provider] ?? c.provider) + " · " + c.instance_url}
                    </p>
                    <p className="text-xs text-muted-foreground break-all">
                      {t(($) => $.vcs.connected_as, { login: c.account_login })}
                    </p>
                  </div>
                </div>
                {canManage && (
                  <div className="flex flex-wrap items-center gap-2 shrink-0">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setRotateTarget(c.id)}
                    >
                      <RefreshCw className="h-3 w-3" />
                      {t(($) => $.vcs.regenerate_webhook)}
                    </Button>
                    <Button variant="outline" size="sm" onClick={() => setDeleteTarget(c.id)}>
                      <Trash2 className="h-3 w-3" />
                      {t(($) => $.vcs.disconnect)}
                    </Button>
                  </div>
                )}
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      {justConnected && (
        <Card className="border-primary/40">
          <CardContent className="space-y-3">
            <div className="space-y-1">
              <p className="text-sm font-medium">{t(($) => $.vcs.webhook_setup_title)}</p>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.vcs.webhook_setup_description)}
              </p>
            </div>
            <CopyField
              label={t(($) => $.vcs.webhook_url_label)}
              value={justConnected.webhook_url || justConnected.webhook_path}
              onCopy={copy}
              copyLabel={t(($) => $.vcs.copy)}
            />
            <CopyField
              label={t(($) => $.vcs.webhook_secret_label)}
              value={justConnected.webhook_secret}
              onCopy={copy}
              copyLabel={t(($) => $.vcs.copy)}
              mono
            />
            <p className="text-xs text-amber-600 dark:text-amber-500">
              {t(($) => $.vcs.webhook_secret_warning)}
            </p>
          </CardContent>
        </Card>
      )}

      {canManage && (
        <Card>
          <CardContent className="space-y-4">
            <p className="text-sm font-medium">{t(($) => $.vcs.connect_title)}</p>
            {!configured ? (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.vcs.not_configured)}{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[10px]">
                  MULTICA_VCS_SECRET_KEY
                </code>
                .
              </p>
            ) : (
              <>
                <div className="space-y-1.5">
                  <Label htmlFor="vcs-provider">{t(($) => $.vcs.form_provider_label)}</Label>
                  <Select
                    items={PROVIDER_OPTIONS}
                    value={provider}
                    onValueChange={(v) => setProvider(v as VCSProvider)}
                  >
                    <SelectTrigger id="vcs-provider" disabled={connecting}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {PROVIDERS.map((p) => (
                        <SelectItem key={p} value={p}>
                          {PROVIDER_LABELS[p]}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="vcs-url">{t(($) => $.vcs.form_instance_url_label)}</Label>
                  <Input
                    id="vcs-url"
                    placeholder="https://forgejo.example.com"
                    value={instanceUrl}
                    onChange={(e) => setInstanceUrl(e.target.value)}
                    disabled={connecting}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="vcs-token">{t(($) => $.vcs.form_token_label)}</Label>
                  <Input
                    id="vcs-token"
                    type="password"
                    placeholder={t(($) => $.vcs.form_token_placeholder)}
                    value={token}
                    onChange={(e) => setToken(e.target.value)}
                    disabled={connecting}
                  />
                  <p className="text-xs text-muted-foreground">{t(($) => $.vcs.form_token_hint)}</p>
                </div>
                <div className="flex justify-end">
                  <Button
                    size="sm"
                    onClick={handleConnect}
                    disabled={connecting || !instanceUrl.trim() || !token.trim()}
                  >
                    {connecting ? t(($) => $.vcs.connecting) : t(($) => $.vcs.connect)}
                  </Button>
                </div>
              </>
            )}
          </CardContent>
        </Card>
      )}

      {!canManage && connections.length === 0 && (
        <p className="text-xs text-muted-foreground">{t(($) => $.vcs.contact_admin)}</p>
      )}

      <AlertDialog
        open={!!rotateTarget}
        onOpenChange={(v) => {
          if (!v && !rotating) setRotateTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.vcs.rotate_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.vcs.rotate_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={rotating}>
              {t(($) => $.vcs.rotate_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleRotateWebhook} disabled={rotating}>
              {rotating ? t(($) => $.vcs.rotating) : t(($) => $.vcs.rotate_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog
        open={!!deleteTarget}
        onOpenChange={(v) => {
          if (!v && !deleting) setDeleteTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.vcs.disconnect_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.vcs.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>
              {t(($) => $.vcs.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} disabled={deleting}>
              {deleting ? t(($) => $.vcs.disconnecting) : t(($) => $.vcs.disconnect_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function CopyField({
  label,
  value,
  onCopy,
  copyLabel,
  mono,
}: {
  label: string;
  value: string;
  onCopy: (v: string) => void;
  copyLabel: string;
  mono?: boolean;
}) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs">{label}</Label>
      <div className="flex items-center gap-2">
        <Input
          readOnly
          value={value}
          className={mono ? "min-w-0 font-mono text-xs" : "min-w-0 text-xs"}
        />
        <Button
          variant="outline"
          size="sm"
          className="shrink-0"
          onClick={() => onCopy(value)}
          title={copyLabel}
        >
          <Copy className="h-3 w-3" />
        </Button>
      </div>
    </div>
  );
}
