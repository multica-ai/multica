"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Copy, GitBranch, Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
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
import { forgejoConnectionsOptions } from "@multica/core/forgejo";
import { api } from "@multica/core/api";
import type { ConnectForgejoResponse } from "@multica/core/types";
import { useT } from "../../i18n";

export function ForgejoTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

  const { data } = useQuery(forgejoConnectionsOptions(wsId));
  const connections = data?.connections ?? [];
  const configured = data?.configured === true;
  const canManage = data?.can_manage === true;

  const [instanceUrl, setInstanceUrl] = useState("");
  const [token, setToken] = useState("");
  const [connecting, setConnecting] = useState(false);
  const [justConnected, setJustConnected] = useState<ConnectForgejoResponse | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  async function handleConnect() {
    if (connecting || !instanceUrl.trim() || !token.trim()) return;
    setConnecting(true);
    try {
      const resp = await api.connectForgejo(wsId, {
        instance_url: instanceUrl.trim(),
        access_token: token.trim(),
      });
      await qc.invalidateQueries({ queryKey: ["forgejo", wsId] });
      setJustConnected(resp);
      setInstanceUrl("");
      setToken("");
      toast.success(t(($) => $.forgejo.toast_connected));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.forgejo.toast_connect_failed));
    } finally {
      setConnecting(false);
    }
  }

  async function handleDelete() {
    if (!deleteTarget || deleting) return;
    setDeleting(true);
    try {
      await api.deleteForgejoConnection(wsId, deleteTarget);
      await qc.invalidateQueries({ queryKey: ["forgejo", wsId] });
      if (justConnected?.id === deleteTarget) setJustConnected(null);
      toast.success(t(($) => $.forgejo.toast_disconnected));
      setDeleteTarget(null);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.forgejo.toast_disconnect_failed));
    } finally {
      setDeleting(false);
    }
  }

  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      toast.success(t(($) => $.forgejo.copied));
    } catch {
      toast.error(t(($) => $.forgejo.copy_failed));
    }
  }

  return (
    <div className="space-y-6">
      <p className="text-sm text-muted-foreground">{t(($) => $.forgejo.page_description)}</p>

      {/* Existing connections */}
      {connections.length > 0 && (
        <div className="space-y-3">
          {connections.map((c) => (
            <Card key={c.id}>
              <CardContent className="flex items-start justify-between gap-4">
                <div className="flex items-start gap-3">
                  <div className="rounded-md border bg-muted/50 p-2 text-muted-foreground">
                    <GitBranch className="h-4 w-4" />
                  </div>
                  <div className="space-y-0.5">
                    <p className="text-sm font-medium">{c.instance_url}</p>
                    <p className="text-xs text-muted-foreground">
                      {t(($) => $.forgejo.connected_as, { login: c.account_login })}
                    </p>
                  </div>
                </div>
                {canManage && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setDeleteTarget(c.id)}
                  >
                    <Trash2 className="h-3 w-3" />
                    {t(($) => $.forgejo.disconnect)}
                  </Button>
                )}
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      {/* One-time webhook setup shown right after connecting */}
      {justConnected && (
        <Card className="border-primary/40">
          <CardContent className="space-y-3">
            <div className="space-y-1">
              <p className="text-sm font-medium">{t(($) => $.forgejo.webhook_setup_title)}</p>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.forgejo.webhook_setup_description)}
              </p>
            </div>
            <CopyField
              label={t(($) => $.forgejo.webhook_url_label)}
              value={justConnected.webhook_url || justConnected.webhook_path}
              onCopy={copy}
              copyLabel={t(($) => $.forgejo.copy)}
            />
            <CopyField
              label={t(($) => $.forgejo.webhook_secret_label)}
              value={justConnected.webhook_secret}
              onCopy={copy}
              copyLabel={t(($) => $.forgejo.copy)}
              mono
            />
            <p className="text-xs text-amber-600 dark:text-amber-500">
              {t(($) => $.forgejo.webhook_secret_warning)}
            </p>
          </CardContent>
        </Card>
      )}

      {/* Connect form */}
      {canManage && (
        <Card>
          <CardContent className="space-y-4">
            <p className="text-sm font-medium">{t(($) => $.forgejo.connect_title)}</p>
            {!configured ? (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.forgejo.not_configured)}{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[10px]">
                  MULTICA_FORGEJO_SECRET_KEY
                </code>
                .
              </p>
            ) : (
              <>
                <div className="space-y-1.5">
                  <Label htmlFor="forgejo-url">{t(($) => $.forgejo.form_instance_url_label)}</Label>
                  <Input
                    id="forgejo-url"
                    placeholder="https://forgejo.example.com"
                    value={instanceUrl}
                    onChange={(e) => setInstanceUrl(e.target.value)}
                    disabled={connecting}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="forgejo-token">{t(($) => $.forgejo.form_token_label)}</Label>
                  <Input
                    id="forgejo-token"
                    type="password"
                    placeholder={t(($) => $.forgejo.form_token_placeholder)}
                    value={token}
                    onChange={(e) => setToken(e.target.value)}
                    disabled={connecting}
                  />
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.forgejo.form_token_hint)}
                  </p>
                </div>
                <div className="flex justify-end">
                  <Button
                    size="sm"
                    onClick={handleConnect}
                    disabled={connecting || !instanceUrl.trim() || !token.trim()}
                  >
                    {connecting
                      ? t(($) => $.forgejo.connecting)
                      : t(($) => $.forgejo.connect)}
                  </Button>
                </div>
              </>
            )}
          </CardContent>
        </Card>
      )}

      {!canManage && connections.length === 0 && (
        <p className="text-xs text-muted-foreground">{t(($) => $.forgejo.contact_admin)}</p>
      )}

      <AlertDialog
        open={!!deleteTarget}
        onOpenChange={(v) => {
          if (!v && !deleting) setDeleteTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.forgejo.disconnect_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.forgejo.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>
              {t(($) => $.forgejo.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} disabled={deleting}>
              {deleting
                ? t(($) => $.forgejo.disconnecting)
                : t(($) => $.forgejo.disconnect_confirm_action)}
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
        <Input readOnly value={value} className={mono ? "font-mono text-xs" : "text-xs"} />
        <Button variant="outline" size="sm" onClick={() => onCopy(value)} title={copyLabel}>
          <Copy className="h-3 w-3" />
        </Button>
      </div>
    </div>
  );
}
