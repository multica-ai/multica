"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Copy, Link2, RefreshCw, Trash2 } from "lucide-react";
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
import { jiraConnectionsOptions } from "@multica/core/jira";
import { issueKeys } from "@multica/core/issues";
import { api, ApiError } from "@multica/core/api";
import type { ConnectJiraResponse } from "@multica/core/types";
import { useT } from "../../i18n";

/** Absolute inbound webhook URL to paste into Jira. Falls back to prefixing
 * the current origin when the server has no public URL configured. */
function resolveWebhookUrl(webhookUrl: string, webhookPath: string): string {
  if (webhookUrl) return webhookUrl;
  const origin = typeof window !== "undefined" ? window.location.origin : "";
  return origin + webhookPath;
}

export function JiraTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

  const { data } = useQuery(jiraConnectionsOptions(wsId));
  const connections = data?.connections ?? [];
  const configured = data?.configured === true;
  const canManage = data?.can_manage === true;

  const [baseUrl, setBaseUrl] = useState("");
  const [email, setEmail] = useState("");
  const [token, setToken] = useState("");
  const [jql, setJql] = useState("");
  const [syncingId, setSyncingId] = useState<string | null>(null);
  const [connecting, setConnecting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [justConnected, setJustConnected] = useState<ConnectJiraResponse | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  async function handleConnect() {
    if (connecting || !baseUrl.trim() || !email.trim() || !token.trim()) return;
    setConnecting(true);
    setFormError(null);
    try {
      const resp = await api.connectJira(wsId, {
        base_url: baseUrl.trim(),
        account_email: email.trim(),
        api_token: token.trim(),
        jql: jql.trim(),
      });
      await qc.invalidateQueries({ queryKey: ["jira", wsId] });
      setJustConnected(resp);
      setBaseUrl("");
      setEmail("");
      setToken("");
      setJql("");
      toast.success(t(($) => $.jira.toast_connected));
    } catch (e) {
      // Bad credentials (400) and unreachable site (502) are expected user
      // errors — surface them inline next to the form instead of a toast.
      if (e instanceof ApiError && e.status === 400) {
        setFormError(t(($) => $.jira.error_bad_credentials));
      } else if (e instanceof ApiError && e.status === 502) {
        setFormError(t(($) => $.jira.error_unreachable));
      } else {
        toast.error(e instanceof Error ? e.message : t(($) => $.jira.toast_connect_failed));
      }
    } finally {
      setConnecting(false);
    }
  }

  async function handleDelete() {
    if (!deleteTarget || deleting) return;
    setDeleting(true);
    try {
      await api.deleteJiraConnection(wsId, deleteTarget);
      await qc.invalidateQueries({ queryKey: ["jira", wsId] });
      if (justConnected?.id === deleteTarget) setJustConnected(null);
      toast.success(t(($) => $.jira.toast_disconnected));
      setDeleteTarget(null);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.jira.toast_disconnect_failed));
    } finally {
      setDeleting(false);
    }
  }

  async function handleSync(connectionId: string) {
    if (syncingId) return;
    setSyncingId(connectionId);
    try {
      const summary = await api.syncJiraConnection(wsId, connectionId);
      // Pulled issues should show up in the issue lists right away.
      await qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
      toast.success(
        t(($) => $.jira.toast_sync_summary, {
          created: summary.created,
          updated: summary.updated,
        }),
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.jira.toast_sync_failed));
    } finally {
      setSyncingId(null);
    }
  }

  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      toast.success(t(($) => $.jira.copied));
    } catch {
      toast.error(t(($) => $.jira.copy_failed));
    }
  }

  return (
    <div className="space-y-6">
      <p className="text-sm text-muted-foreground">{t(($) => $.jira.page_description)}</p>

      {connections.length > 0 && (
        <div className="space-y-3">
          {connections.map((c) => (
            <Card key={c.id}>
              <CardContent className="space-y-3">
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between sm:gap-4">
                  <div className="flex min-w-0 items-start gap-3">
                    <div className="rounded-md border bg-muted/50 p-2 text-muted-foreground shrink-0">
                      <Link2 className="h-4 w-4" />
                    </div>
                    <div className="min-w-0 space-y-0.5">
                      <p className="text-sm font-medium break-all">{c.base_url}</p>
                      <p className="text-xs text-muted-foreground break-all">
                        {t(($) => $.jira.connected_as, { email: c.account_email })}
                      </p>
                    </div>
                  </div>
                  {canManage && (
                    <div className="flex flex-wrap items-center gap-2 shrink-0">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => handleSync(c.id)}
                        disabled={syncingId !== null}
                      >
                        <RefreshCw
                          className={syncingId === c.id ? "h-3 w-3 animate-spin" : "h-3 w-3"}
                        />
                        {syncingId === c.id
                          ? t(($) => $.jira.syncing)
                          : t(($) => $.jira.sync_now)}
                      </Button>
                      <Button variant="outline" size="sm" onClick={() => setDeleteTarget(c.id)}>
                        <Trash2 className="h-3 w-3" />
                        {t(($) => $.jira.disconnect)}
                      </Button>
                    </div>
                  )}
                </div>
                <CopyField
                  label={t(($) => $.jira.webhook_url_label)}
                  value={resolveWebhookUrl(c.webhook_url, c.webhook_path)}
                  onCopy={copy}
                  copyLabel={t(($) => $.jira.copy)}
                />
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      {justConnected && (
        <Card className="border-primary/40">
          <CardContent className="space-y-3">
            <div className="space-y-1">
              <p className="text-sm font-medium">{t(($) => $.jira.webhook_setup_title)}</p>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.jira.webhook_setup_description)}
              </p>
            </div>
            <CopyField
              label={t(($) => $.jira.webhook_url_label)}
              value={resolveWebhookUrl(justConnected.webhook_url, justConnected.webhook_path)}
              onCopy={copy}
              copyLabel={t(($) => $.jira.copy)}
            />
            <CopyField
              label={t(($) => $.jira.webhook_secret_label)}
              value={justConnected.webhook_secret}
              onCopy={copy}
              copyLabel={t(($) => $.jira.copy)}
              mono
            />
            <p className="text-xs text-amber-600 dark:text-amber-500">
              {t(($) => $.jira.webhook_secret_warning)}
            </p>
          </CardContent>
        </Card>
      )}

      {canManage && (
        <Card>
          <CardContent className="space-y-4">
            <p className="text-sm font-medium">{t(($) => $.jira.connect_title)}</p>
            {!configured ? (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.jira.not_configured)}{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[10px]">
                  MULTICA_JIRA_SECRET_KEY
                </code>
                .
              </p>
            ) : (
              <>
                <div className="space-y-1.5">
                  <Label htmlFor="jira-url">{t(($) => $.jira.form_base_url_label)}</Label>
                  <Input
                    id="jira-url"
                    placeholder="https://your-site.atlassian.net"
                    value={baseUrl}
                    onChange={(e) => setBaseUrl(e.target.value)}
                    disabled={connecting}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="jira-email">{t(($) => $.jira.form_email_label)}</Label>
                  <Input
                    id="jira-email"
                    type="email"
                    placeholder="you@example.com"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    disabled={connecting}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="jira-token">{t(($) => $.jira.form_token_label)}</Label>
                  <Input
                    id="jira-token"
                    type="password"
                    placeholder={t(($) => $.jira.form_token_placeholder)}
                    value={token}
                    onChange={(e) => setToken(e.target.value)}
                    disabled={connecting}
                  />
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.jira.form_token_hint)}
                  </p>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="jira-jql">{t(($) => $.jira.form_jql_label)}</Label>
                  <Input
                    id="jira-jql"
                    placeholder="assignee = currentUser()"
                    value={jql}
                    onChange={(e) => setJql(e.target.value)}
                    disabled={connecting}
                  />
                  <p className="text-xs text-muted-foreground">{t(($) => $.jira.form_jql_hint)}</p>
                </div>
                {formError && <p className="text-xs text-destructive">{formError}</p>}
                <div className="flex justify-end">
                  <Button
                    size="sm"
                    onClick={handleConnect}
                    disabled={connecting || !baseUrl.trim() || !email.trim() || !token.trim()}
                  >
                    {connecting ? t(($) => $.jira.connecting) : t(($) => $.jira.connect)}
                  </Button>
                </div>
              </>
            )}
          </CardContent>
        </Card>
      )}

      {!canManage && connections.length === 0 && (
        <p className="text-xs text-muted-foreground">{t(($) => $.jira.contact_admin)}</p>
      )}

      <AlertDialog
        open={!!deleteTarget}
        onOpenChange={(v) => {
          if (!v && !deleting) setDeleteTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.jira.disconnect_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.jira.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>
              {t(($) => $.jira.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} disabled={deleting}>
              {deleting
                ? t(($) => $.jira.disconnecting)
                : t(($) => $.jira.disconnect_confirm_action)}
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
