"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Copy, FolderGit2, GitBranch, MoreHorizontal, RefreshCw, Unplug } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
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
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useWorkspaceId } from "@multica/core/hooks";
import { vcsConnectionsOptions } from "@multica/core/vcs";
import { api } from "@multica/core/api";
import type { ConnectVCSResponse, VCSProvider } from "@multica/core/types";
import { useT } from "../../i18n";
import { SettingsRow } from "./settings-layout";
import { HostMark, HostStatus } from "./code-host";

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

/**
 * Self-hosted Forgejo / Gitea / GitLab connections, rendered as rows of the
 * Code page's "Code hosting" card: one row per connected instance, then a row
 * to connect another. Connecting and rotating both end on the one-time webhook
 * secret, shown in a dialog the user has to dismiss.
 */
export function VCSConnectionRows() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

  const { data } = useQuery(vcsConnectionsOptions(wsId));
  const connections = data?.connections ?? [];
  const configured = data?.configured === true;
  const canManage = data?.can_manage === true;

  const [connectOpen, setConnectOpen] = useState(false);
  const [provider, setProvider] = useState<VCSProvider>("forgejo");
  const [instanceUrl, setInstanceUrl] = useState("");
  const [token, setToken] = useState("");
  const [connecting, setConnecting] = useState(false);
  const [webhook, setWebhook] = useState<ConnectVCSResponse | null>(null);
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
      setInstanceUrl("");
      setToken("");
      setConnectOpen(false);
      setWebhook(resp);
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
      setRotateTarget(null);
      setWebhook(resp);
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

  const connectDetail = !canManage
    ? connections.length === 0
      ? t(($) => $.vcs.contact_admin)
      : undefined
    : !configured
      ? (
          <>
            {t(($) => $.vcs.not_configured)}{" "}
            <code className="rounded-xs bg-muted px-1 py-0.5 text-micro">
              MULTICA_VCS_SECRET_KEY
            </code>
          </>
        )
      : t(($) => $.vcs.hosts_hint);

  return (
    <>
      {connections.map((c) => (
        <SettingsRow
          key={c.id}
          label={
            <span className="flex min-w-0 items-center gap-3">
              <HostMark>
                <GitBranch className="size-4" />
              </HostMark>
              <span className="flex min-w-0 flex-wrap items-center gap-x-2">
                <span className="break-all">
                  {`${PROVIDER_LABELS[c.provider] ?? c.provider} · ${c.instance_url}`}
                </span>
                <HostStatus tone="success" label={t(($) => $.integrations.status_connected)} />
              </span>
            </span>
          }
          description={
            <span className="block break-all pl-12">
              {t(($) => $.vcs.connected_as, { login: c.account_login })}
            </span>
          }
        >
          {canManage ? (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={t(($) => $.vcs.connection_actions, { url: c.instance_url })}
                  />
                }
              >
                <MoreHorizontal />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-auto">
                <DropdownMenuItem onClick={() => setRotateTarget(c.id)}>
                  <RefreshCw />
                  {t(($) => $.vcs.regenerate_webhook)}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" onClick={() => setDeleteTarget(c.id)}>
                  <Unplug />
                  {t(($) => $.vcs.disconnect)}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
        </SettingsRow>
      ))}

      {canManage || connections.length === 0 ? (
        <SettingsRow
          anchor="vcs"
          label={
            <span className="flex min-w-0 items-center gap-3">
              <HostMark>
                <FolderGit2 className="size-4" />
              </HostMark>
              <span className="flex min-w-0 flex-wrap items-center gap-x-2">
                {t(($) => $.vcs.section_title)}
                {connections.length === 0 ? (
                  <HostStatus
                    tone="muted"
                    label={t(($) => $.integrations.status_not_connected)}
                  />
                ) : null}
              </span>
            </span>
          }
          description={
            connectDetail ? <span className="block pl-12">{connectDetail}</span> : undefined
          }
        >
          {canManage ? (
            <Button
              variant="outline"
              size="sm"
              disabled={!configured}
              onClick={() => setConnectOpen(true)}
            >
              {connections.length === 0
                ? t(($) => $.vcs.connect)
                : t(($) => $.vcs.connect_another)}
            </Button>
          ) : null}
        </SettingsRow>
      ) : null}

      <Dialog
        open={connectOpen}
        onOpenChange={(open) => {
          if (!open && !connecting) setConnectOpen(false);
        }}
      >
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{t(($) => $.vcs.connect_title)}</DialogTitle>
            <DialogDescription>{t(($) => $.vcs.page_description)}</DialogDescription>
          </DialogHeader>
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              void handleConnect();
            }}
          >
            <div className="space-y-1.5">
              <Label htmlFor="vcs-provider">{t(($) => $.vcs.form_provider_label)}</Label>
              <Select
                items={PROVIDER_OPTIONS}
                value={provider}
                onValueChange={(v) => setProvider(v as VCSProvider)}
              >
                <SelectTrigger id="vcs-provider" className="w-full" disabled={connecting}>
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
                autoComplete="off"
                placeholder={t(($) => $.vcs.form_token_placeholder)}
                value={token}
                onChange={(e) => setToken(e.target.value)}
                disabled={connecting}
              />
              <p className="text-caption text-muted-foreground">{t(($) => $.vcs.form_token_hint)}</p>
            </div>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setConnectOpen(false)}
                disabled={connecting}
              >
                {t(($) => $.vcs.disconnect_confirm_cancel)}
              </Button>
              <Button
                type="submit"
                disabled={connecting || !instanceUrl.trim() || !token.trim()}
                aria-busy={connecting || undefined}
              >
                {connecting ? t(($) => $.vcs.connecting) : t(($) => $.vcs.connect)}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!webhook}
        onOpenChange={(open) => {
          if (!open) setWebhook(null);
        }}
      >
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{t(($) => $.vcs.webhook_setup_title)}</DialogTitle>
            <DialogDescription>{t(($) => $.vcs.webhook_setup_description)}</DialogDescription>
          </DialogHeader>
          {webhook ? (
            <div className="space-y-3">
              <CopyField
                id="vcs-webhook-url"
                label={t(($) => $.vcs.webhook_url_label)}
                value={webhook.webhook_url || webhook.webhook_path}
                onCopy={copy}
                copyLabel={t(($) => $.vcs.copy)}
              />
              <CopyField
                id="vcs-webhook-secret"
                label={t(($) => $.vcs.webhook_secret_label)}
                value={webhook.webhook_secret}
                onCopy={copy}
                copyLabel={t(($) => $.vcs.copy)}
                mono
              />
              <p className="text-caption text-warning">{t(($) => $.vcs.webhook_secret_warning)}</p>
            </div>
          ) : null}
          <DialogFooter>
            <Button onClick={() => setWebhook(null)}>{t(($) => $.vcs.webhook_done)}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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
            <AlertDialogAction variant="destructive" onClick={handleDelete} disabled={deleting}>
              {deleting ? t(($) => $.vcs.disconnecting) : t(($) => $.vcs.disconnect_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function CopyField({
  id,
  label,
  value,
  onCopy,
  copyLabel,
  mono,
}: {
  id: string;
  label: string;
  value: string;
  onCopy: (v: string) => void;
  copyLabel: string;
  mono?: boolean;
}) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id} className="text-caption">
        {label}
      </Label>
      <div className="flex items-center gap-2">
        <Input
          id={id}
          readOnly
          value={value}
          className={mono ? "min-w-0 font-mono text-caption" : "min-w-0 text-caption"}
        />
        <Button
          variant="outline"
          size="icon-sm"
          className="shrink-0"
          onClick={() => onCopy(value)}
          aria-label={`${copyLabel}: ${label}`}
        >
          <Copy />
        </Button>
      </div>
    </div>
  );
}
