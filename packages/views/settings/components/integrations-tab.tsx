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
import { api } from "@multica/core/api";
import { LarkTab } from "./lark-tab";
import { useT } from "../../i18n";

// Integrations is the umbrella tab for third-party platform connections.
// GitHub has its own top-level tab (see github-tab.tsx); everything else
// — currently just Lark, with Slack/Linear etc. to follow — lives in
// here under its own section heading so additional integrations slot in
// without changing the IA. IntegrationsTab is just the host; each
// integration owns its own description and install flow.
export function IntegrationsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";

  return (
    <div className="space-y-10">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.lark.section_title)}</h2>
        <LarkTab />
      </section>

      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.integrations.gitee_title)}</h2>
        {canManage ? (
          <GiteeIntegrationCard wsId={wsId} />
        ) : (
          <p className="text-xs text-muted-foreground">
            {t(($) => $.integrations.manage_hint)}
          </p>
        )}
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
