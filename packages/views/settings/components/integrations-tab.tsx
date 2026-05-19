"use client";

import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { githubInstallationsOptions } from "@multica/core/github/queries";
import { api } from "@multica/core/api";
import { useT } from "../../i18n";

// lucide-react v1.x dropped brand marks (including Github). Render an inline
// SVG of the official GitHub octocat mark so the card is still recognizable.
function GitHubMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className={className} fill="currentColor">
      <path d="M12 .5C5.6.5.5 5.6.5 12c0 5.1 3.3 9.4 7.9 10.9.6.1.8-.2.8-.6v-2.2c-3.2.7-3.9-1.5-3.9-1.5-.5-1.3-1.3-1.7-1.3-1.7-1.1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1 1.8 2.7 1.3 3.4 1 .1-.8.4-1.3.8-1.6-2.6-.3-5.3-1.3-5.3-5.7 0-1.3.5-2.3 1.2-3.1-.1-.3-.5-1.5.1-3.1 0 0 1-.3 3.3 1.2.9-.3 1.9-.4 2.9-.4s2 .1 2.9.4c2.3-1.5 3.3-1.2 3.3-1.2.6 1.6.2 2.8.1 3.1.7.8 1.2 1.8 1.2 3.1 0 4.4-2.7 5.4-5.3 5.7.4.4.8 1.1.8 2.2v3.3c0 .3.2.7.8.6 4.6-1.5 7.9-5.8 7.9-10.9C23.5 5.6 18.4.5 12 .5z" />
    </svg>
  );
}

export function IntegrationsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const [connecting, setConnecting] = useState(false);

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";

  // Only used to gate the Connect button + show a "not configured" hint;
  // we no longer render the installation list here — admins manage existing
  // installations on GitHub directly via the Connect flow.
  const { data } = useQuery({
    ...githubInstallationsOptions(wsId),
    enabled: !!wsId && canManage,
  });
  const configured = data?.configured ?? false;

  async function handleConnect() {
    setConnecting(true);
    try {
      const resp = await api.getGitHubConnectURL(wsId);
      if (!resp.configured || !resp.url) {
        toast.error(t(($) => $.integrations.toast_not_configured));
        return;
      }
      window.open(resp.url, "_blank", "noopener");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.integrations.toast_open_failed));
    } finally {
      setConnecting(false);
    }
  }

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.integrations.section_title)}</h2>

        <Card>
          <CardContent className="space-y-4">
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <GitHubMark className="h-6 w-6 mt-0.5 shrink-0" />
                <div className="space-y-1">
                  <p className="text-sm font-medium">{t(($) => $.integrations.github_title)}</p>
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.integrations.github_description_prefix)}{" "}
                    <code className="rounded bg-muted px-1 py-0.5 text-[10px]">
                      {t(($) => $.integrations.github_identifier_example)}
                    </code>{" "}
                    {t(($) => $.integrations.github_description_suffix)}{" "}
                    <strong>{t(($) => $.integrations.github_description_done)}</strong>.
                  </p>
                </div>
              </div>
              {canManage && (
                <Button
                  size="sm"
                  onClick={handleConnect}
                  disabled={connecting || !configured}
                  title={!configured ? t(($) => $.integrations.connect_disabled_tooltip) : undefined}
                >
                  {connecting
                    ? t(($) => $.integrations.connect_opening)
                    : t(($) => $.integrations.connect_github)}
                </Button>
              )}
            </div>

            {canManage && !configured && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.integrations.not_configured)}{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[10px]">GITHUB_APP_SLUG</code>{" "}
                {t(($) => $.integrations.not_configured_and)}{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[10px]">GITHUB_WEBHOOK_SECRET</code>.
              </p>
            )}

            {!canManage && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.integrations.manage_hint)}
              </p>
            )}
          </CardContent>
        </Card>

        {canManage && <GiteeIntegrationCard wsId={wsId} />}
      </section>
    </div>
  );
}

// ── Gitee Integration Card ──────────────────────────────────────────────────

function GiteeMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className={className} fill="currentColor">
      <path d="M11.984 0A12 12 0 0 0 0 12a12 12 0 0 0 12 12 12 12 0 0 0 12-12A12 12 0 0 0 12 0a12 12 0 0 0-.016 0zm6.09 5.333c.328 0 .593.266.592.593v1.482a.594.594 0 0 1-.593.592H9.777c-.982 0-1.778.796-1.778 1.778v5.926c0 .982.796 1.778 1.778 1.778h4.446c.982 0 1.778-.796 1.778-1.778V14.52a.593.593 0 0 0-.592-.593h-4.45a.593.593 0 0 1-.592-.592v-1.482a.593.593 0 0 1 .593-.593h6.814a.593.593 0 0 1 .593.593v4.519a4 4 0 0 1-4 4H9.777a4 4 0 0 1-4-4V9.778a4 4 0 0 1 4-4h8.297z" />
    </svg>
  );
}

function GiteeIntegrationCard({ wsId }: { wsId: string }) {
  const queryClient = useQueryClient();
  const [repoOwner, setRepoOwner] = useState("");
  const [repoName, setRepoName] = useState("");
  const [adding, setAdding] = useState(false);

  const { data: configs = [] } = useQuery({
    queryKey: ["gitee-webhook-configs", wsId],
    queryFn: async () => {
      const resp = await api.listGiteeWebhookConfigs(wsId);
      return resp.configs ?? [];
    },
    enabled: !!wsId,
  });

  const createMutation = useMutation({
    mutationFn: async () => {
      return api.createGiteeWebhookConfig(wsId, repoOwner, repoName);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["gitee-webhook-configs", wsId] });
      setRepoOwner("");
      setRepoName("");
      setAdding(false);
      toast.success("Gitee webhook config created");
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : "Failed"),
  });

  const deleteMutation = useMutation({
    mutationFn: async (configId: string) => {
      return api.deleteGiteeWebhookConfig(wsId, configId);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["gitee-webhook-configs", wsId] });
      toast.success("Gitee webhook config deleted");
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : "Failed"),
  });

  return (
    <Card>
      <CardContent className="space-y-4">
        <div className="flex items-start gap-3">
          <GiteeMark className="h-6 w-6 mt-0.5 shrink-0" />
          <div className="space-y-1">
            <p className="text-sm font-medium">Gitee PR Integration</p>
            <p className="text-xs text-muted-foreground">
              Configure Gitee webhook to auto-link PRs containing issue identifiers like{" "}
              <code className="rounded bg-muted px-1 py-0.5 text-[10px]">OPE-918</code>{" "}
              in title, body, or branch name.
            </p>
          </div>
        </div>

        {configs.length > 0 && (
          <div className="space-y-2">
            {configs.map((cfg) => (
              <div key={cfg.id} className="flex items-center justify-between rounded border px-3 py-2 text-xs">
                <div className="space-y-0.5">
                  <p className="font-medium">{cfg.repo_owner}/{cfg.repo_name}</p>
                  <p className="text-muted-foreground">
                    Webhook URL: <code className="rounded bg-muted px-1 py-0.5">{cfg.webhook_url}</code>
                  </p>
                  <p className="text-muted-foreground">
                    Secret: <code className="rounded bg-muted px-1 py-0.5">{cfg.secret}</code>
                  </p>
                </div>
                <Button
                  size="sm"
                  variant="ghost"
                  className="text-destructive"
                  onClick={() => deleteMutation.mutate(cfg.id)}
                >
                  Remove
                </Button>
              </div>
            ))}
          </div>
        )}

        {adding ? (
          <div className="flex items-end gap-2">
            <div className="space-y-1">
              <label className="text-[10px] text-muted-foreground">Owner</label>
              <Input
                className="h-7 text-xs"
                placeholder="wujie-agent"
                value={repoOwner}
                onChange={(e) => setRepoOwner(e.target.value)}
              />
            </div>
            <div className="space-y-1">
              <label className="text-[10px] text-muted-foreground">Repo</label>
              <Input
                className="h-7 text-xs"
                placeholder="multica"
                value={repoName}
                onChange={(e) => setRepoName(e.target.value)}
              />
            </div>
            <Button
              size="sm"
              className="h-7"
              disabled={!repoOwner || !repoName || createMutation.isPending}
              onClick={() => createMutation.mutate()}
            >
              Save
            </Button>
            <Button size="sm" variant="ghost" className="h-7" onClick={() => setAdding(false)}>
              Cancel
            </Button>
          </div>
        ) : (
          <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
            Add Gitee Repository
          </Button>
        )}
      </CardContent>
    </Card>
  );
}
