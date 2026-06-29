"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Copy, ExternalLink, Plus, Trash2 } from "lucide-react";
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
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { adoInstallationsOptions, adoKeys } from "@multica/core/azuredevops";
import { api } from "@multica/core/api";
import type { ADOInstallation } from "@multica/core/types";
import { AzureDevOpsMark } from "./azuredevops-mark";
import { useT } from "../../i18n";

interface ConnectFormState {
  orgUrl: string;
  pat: string;
}

export function AzureDevOpsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data: instData } = useQuery({
    ...adoInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const installations = instData?.installations ?? [];

  const [showConnectForm, setShowConnectForm] = useState(false);
  const [form, setForm] = useState<ConnectFormState>({ orgUrl: "", pat: "" });
  const [connecting, setConnecting] = useState(false);
  const [newWebhookURL, setNewWebhookURL] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ADOInstallation | null>(null);
  const [deleting, setDeleting] = useState(false);

  async function handleConnect() {
    if (!form.orgUrl.trim() || !form.pat.trim()) {
      toast.error(t(($) => $.azuredevops.toast_connect_required));
      return;
    }
    setConnecting(true);
    try {
      const inst = await api.createADOInstallation(wsId, form.orgUrl.trim(), form.pat.trim());
      await qc.invalidateQueries({ queryKey: adoKeys.installations(wsId) });
      setForm({ orgUrl: "", pat: "" });
      setShowConnectForm(false);
      if (inst.webhook_url) {
        setNewWebhookURL(inst.webhook_url);
      }
      toast.success(t(($) => $.azuredevops.toast_connected, { name: inst.display_name }));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.azuredevops.toast_connect_failed));
    } finally {
      setConnecting(false);
    }
  }

  async function handleDelete() {
    if (!deleteTarget || deleting) return;
    setDeleting(true);
    try {
      await api.deleteADOInstallation(wsId, deleteTarget.id);
      await qc.invalidateQueries({ queryKey: adoKeys.installations(wsId) });
      toast.success(t(($) => $.azuredevops.toast_disconnected, { name: deleteTarget.display_name }));
      setDeleteTarget(null);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.azuredevops.toast_disconnect_failed));
    } finally {
      setDeleting(false);
    }
  }

  function copyWebhookUrl(url: string) {
    navigator.clipboard.writeText(url).then(
      () => toast.success(t(($) => $.azuredevops.toast_webhook_copied)),
      () => toast.error(t(($) => $.azuredevops.toast_copy_failed)),
    );
  }

  return (
    <div className="space-y-8">
      <section className="space-y-1">
        <p className="text-sm text-muted-foreground">
          {t(($) => $.azuredevops.page_description)}
        </p>
      </section>

      {/* Webhook URL copy banner — shown once after a successful connection */}
      {newWebhookURL && (
        <Card className="border-blue-200 dark:border-blue-800 bg-blue-50 dark:bg-blue-950/30">
          <CardContent className="space-y-3">
            <p className="text-sm font-medium text-blue-900 dark:text-blue-200">
              {t(($) => $.azuredevops.webhook_banner_title)}
            </p>
            <p className="text-xs text-blue-700 dark:text-blue-300">
              {t(($) => $.azuredevops.webhook_instructions_prefix)}{" "}
              <strong>{t(($) => $.azuredevops.webhook_instructions_service_hooks)}</strong>
              {t(($) => $.azuredevops.webhook_instructions_middle)}{" "}
              <strong>{t(($) => $.azuredevops.webhook_event_pr_created)}</strong>,{" "}
              <strong>{t(($) => $.azuredevops.webhook_event_pr_updated)}</strong>,{" "}
              {t(($) => $.azuredevops.webhook_event_and)}{" "}
              <strong>{t(($) => $.azuredevops.webhook_event_build_completed)}</strong>{" "}
              {t(($) => $.azuredevops.webhook_instructions_suffix)}
            </p>
            <div className="flex items-center gap-2">
              <code className="flex-1 rounded bg-blue-100 dark:bg-blue-900/50 px-2 py-1 text-[11px] font-mono text-blue-800 dark:text-blue-200 break-all">
                {newWebhookURL}
              </code>
              <Button
                variant="outline"
                size="sm"
                className="shrink-0"
                onClick={() => copyWebhookUrl(newWebhookURL)}
              >
                <Copy className="h-3 w-3" />
                {t(($) => $.azuredevops.webhook_copy)}
              </Button>
            </div>
            <Button
              variant="ghost"
              size="sm"
              className="text-blue-600 dark:text-blue-400"
              onClick={() => setNewWebhookURL(null)}
            >
              {t(($) => $.azuredevops.webhook_dismiss)}
            </Button>
          </CardContent>
        </Card>
      )}

      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-semibold">{t(($) => $.azuredevops.section_orgs)}</h2>
          {canManage && !showConnectForm && (
            <Button variant="outline" size="sm" onClick={() => setShowConnectForm(true)}>
              <Plus className="h-3 w-3" />
              {t(($) => $.azuredevops.connect_button)}
            </Button>
          )}
        </div>

        {/* Connect form */}
        {showConnectForm && canManage && (
          <Card>
            <CardContent className="space-y-4">
              <p className="text-sm font-medium">{t(($) => $.azuredevops.connect_form_title)}</p>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.azuredevops.connect_form_pat_hint_create)}{" "}
                <a
                  href="https://dev.azure.com"
                  target="_blank"
                  rel="noreferrer noopener"
                  className="underline underline-offset-2 inline-flex items-center gap-0.5"
                >
                  {t(($) => $.azuredevops.connect_form_pat_link_label)}
                  <ExternalLink className="h-3 w-3" />
                </a>{" "}
                {t(($) => $.azuredevops.connect_form_pat_hint_after_link)}{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[10px]">
                  {t(($) => $.azuredevops.connect_form_pat_scope)}
                </code>.
              </p>
              <div className="space-y-3">
                <div className="space-y-1.5">
                  <Label htmlFor="ado-org-url" className="text-xs">
                    {t(($) => $.azuredevops.connect_form_org_url_label)}
                  </Label>
                  <Input
                    id="ado-org-url"
                    placeholder={t(($) => $.azuredevops.connect_form_org_url_placeholder)}
                    value={form.orgUrl}
                    onChange={(e) => setForm({ ...form, orgUrl: e.target.value })}
                    className="text-sm font-mono"
                  />
                  <p className="text-[11px] text-muted-foreground">
                    {t(($) => $.azuredevops.org_url_legacy_hint_prefix)}{" "}
                    <code className="rounded bg-muted px-1 py-0.5 text-[10px]">
                      {t(($) => $.azuredevops.org_url_legacy_example)}
                    </code>{" "}
                    {t(($) => $.azuredevops.org_url_legacy_hint_suffix)}
                  </p>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="ado-pat" className="text-xs">
                    {t(($) => $.azuredevops.connect_form_pat_label)}
                  </Label>
                  <Input
                    id="ado-pat"
                    type="password"
                    placeholder={t(($) => $.azuredevops.connect_form_pat_placeholder)}
                    value={form.pat}
                    onChange={(e) => setForm({ ...form, pat: e.target.value })}
                    className="text-sm font-mono"
                  />
                </div>
              </div>
              <div className="flex items-center gap-2 justify-end">
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={connecting}
                  onClick={() => {
                    setShowConnectForm(false);
                    setForm({ orgUrl: "", pat: "" });
                  }}
                >
                  {t(($) => $.azuredevops.connect_form_cancel)}
                </Button>
                <Button size="sm" disabled={connecting} onClick={handleConnect}>
                  {connecting
                    ? t(($) => $.azuredevops.connect_form_submitting)
                    : t(($) => $.azuredevops.connect_form_submit)}
                </Button>
              </div>
            </CardContent>
          </Card>
        )}

        {/* Installation list */}
        <Card>
          <CardContent className="space-y-2">
            {installations.length === 0 ? (
              <p className="text-xs text-muted-foreground italic py-2">
                {t(($) => $.azuredevops.empty_orgs)}
                {!canManage && ` ${t(($) => $.azuredevops.empty_orgs_ask_admin)}`}
              </p>
            ) : (
              installations.map((inst) => (
                <InstallationRow
                  key={inst.id}
                  installation={inst}
                  canManage={canManage}
                  copyTooltip={t(($) => $.azuredevops.row_copy_webhook_tooltip)}
                  disconnectTooltip={t(($) => $.azuredevops.row_disconnect_tooltip)}
                  onCopyWebhookUrl={
                    inst.webhook_url ? () => copyWebhookUrl(inst.webhook_url!) : undefined
                  }
                  onDelete={() => setDeleteTarget(inst)}
                />
              ))
            )}
            {!canManage && installations.length > 0 && (
              <p className="text-xs text-muted-foreground pt-1">
                {t(($) => $.azuredevops.read_only_hint)}
              </p>
            )}
          </CardContent>
        </Card>
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-semibold">{t(($) => $.azuredevops.section_how_it_works)}</h2>
        <Card>
          <CardContent className="space-y-2 text-xs text-muted-foreground">
            <p>
              <strong className="text-foreground">{t(($) => $.azuredevops.how_pat_title)}</strong>
              {" — "}{t(($) => $.azuredevops.how_pat_body)}
            </p>
            <p>
              <strong className="text-foreground">{t(($) => $.azuredevops.how_webhooks_title)}</strong>
              {" — "}{t(($) => $.azuredevops.how_webhooks_body)}
            </p>
            <p>
              <strong className="text-foreground">{t(($) => $.azuredevops.how_policy_title)}</strong>
              {" — "}{t(($) => $.azuredevops.how_policy_body_prefix)}{" "}
              <em>{t(($) => $.azuredevops.how_policy_approved)}</em>,{" "}
              <em>{t(($) => $.azuredevops.how_policy_blocked)}</em>,{" "}
              {t(($) => $.azuredevops.how_policy_or)}{" "}
              <em>{t(($) => $.azuredevops.how_policy_pending)}</em>.
            </p>
            <p>
              <strong className="text-foreground">{t(($) => $.azuredevops.how_autoclose_title)}</strong>
              {" — "}{t(($) => $.azuredevops.how_autoclose_body_prefix)}{" "}
              <code className="rounded bg-muted px-1 py-0.5 text-[10px]">
                {t(($) => $.azuredevops.how_autoclose_example)}
              </code>{" "}
              {t(($) => $.azuredevops.how_autoclose_body_suffix)}
            </p>
          </CardContent>
        </Card>
      </section>

      <AlertDialog
        open={!!deleteTarget}
        onOpenChange={(v) => {
          if (!v && !deleting) setDeleteTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.azuredevops.disconnect_title, { name: deleteTarget?.display_name ?? "" })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.azuredevops.disconnect_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>
              {t(($) => $.azuredevops.disconnect_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} disabled={deleting}>
              {deleting
                ? t(($) => $.azuredevops.disconnecting)
                : t(($) => $.azuredevops.disconnect_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function InstallationRow({
  installation,
  canManage,
  copyTooltip,
  disconnectTooltip,
  onCopyWebhookUrl,
  onDelete,
}: {
  installation: ADOInstallation;
  canManage: boolean;
  copyTooltip: string;
  disconnectTooltip: string;
  onCopyWebhookUrl?: () => void;
  onDelete: () => void;
}) {
  return (
    <div className="group flex items-center justify-between gap-4 rounded-md border bg-muted/30 px-3 py-2">
      <div className="flex items-center gap-2.5 min-w-0">
        <AzureDevOpsMark className="h-4 w-4 shrink-0" />
        <div className="min-w-0">
          <p className="text-sm font-medium truncate">{installation.display_name}</p>
          <p className="text-xs text-muted-foreground truncate font-mono">{installation.org_url}</p>
        </div>
      </div>
      {canManage && (
        <div className="flex shrink-0 items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100 [@media(hover:none)]:opacity-100">
          {onCopyWebhookUrl && (
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-muted-foreground hover:text-foreground"
              title={copyTooltip}
              onClick={onCopyWebhookUrl}
            >
              <Copy className="h-3.5 w-3.5" />
            </Button>
          )}
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 text-muted-foreground hover:text-destructive"
            title={disconnectTooltip}
            onClick={onDelete}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      )}
    </div>
  );
}
