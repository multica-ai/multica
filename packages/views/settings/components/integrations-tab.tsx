"use client";

import { useState } from "react";
import { Trash2 } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { githubInstallationsOptions, githubKeys } from "@multica/core/github/queries";
import { api } from "@multica/core/api";

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
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const qc = useQueryClient();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data, isLoading } = useQuery(githubInstallationsOptions(wsId));
  const [connecting, setConnecting] = useState(false);

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";

  const installations = data?.installations ?? [];
  const configured = data?.configured ?? false;

  async function handleConnect() {
    setConnecting(true);
    try {
      const resp = await api.getGitHubConnectURL(wsId);
      if (!resp.configured || !resp.url) {
        toast.error("GitHub integration is not configured for this deployment");
        return;
      }
      window.open(resp.url, "_blank", "noopener");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to open GitHub install");
    } finally {
      setConnecting(false);
    }
  }

  async function handleDisconnect(installationDbId: string) {
    try {
      await api.deleteGitHubInstallation(wsId, installationDbId);
      qc.invalidateQueries({ queryKey: githubKeys.installations(wsId) });
      toast.success("Disconnected GitHub account");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to disconnect");
    }
  }

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">Integrations</h2>

        <Card>
          <CardContent className="space-y-4">
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <GitHubMark className="h-6 w-6 mt-0.5 shrink-0" />
                <div className="space-y-1">
                  <p className="text-sm font-medium">GitHub</p>
                  <p className="text-xs text-muted-foreground">
                    Link issues to pull requests automatically. When a PR with{" "}
                    <code className="rounded bg-muted px-1 py-0.5 text-[10px]">MUL-123</code>{" "}
                    in its branch, title, or body is merged, the corresponding issue moves to <strong>Done</strong>.
                  </p>
                </div>
              </div>
              {canManage && (
                <Button
                  size="sm"
                  onClick={handleConnect}
                  disabled={connecting || !configured}
                  title={!configured ? "GitHub App is not configured on this server" : undefined}
                >
                  {connecting ? "Opening…" : "Connect GitHub"}
                </Button>
              )}
            </div>

            {!configured && (
              <p className="text-xs text-muted-foreground">
                GitHub integration is not configured for this deployment. Operators must set{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[10px]">GITHUB_APP_SLUG</code>{" "}
                and{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[10px]">GITHUB_WEBHOOK_SECRET</code>.
              </p>
            )}

            {configured && (
              <div className="space-y-2">
                {isLoading && <p className="text-xs text-muted-foreground">Loading…</p>}
                {!isLoading && installations.length === 0 && (
                  <p className="text-xs text-muted-foreground">
                    No GitHub accounts connected yet.
                  </p>
                )}
                {installations.map((inst) => (
                  <div
                    key={inst.id}
                    className="flex items-center justify-between gap-3 rounded-md border px-3 py-2"
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      {inst.account_avatar_url && (
                        <img
                          src={inst.account_avatar_url}
                          alt=""
                          className="h-6 w-6 rounded-full shrink-0"
                        />
                      )}
                      <div className="min-w-0">
                        <p className="text-sm font-medium truncate">{inst.account_login}</p>
                        <p className="text-xs text-muted-foreground">
                          {inst.account_type} · connected{" "}
                          {new Date(inst.created_at).toLocaleDateString("en-US", {
                            month: "short",
                            day: "numeric",
                          })}
                        </p>
                      </div>
                    </div>
                    {canManage && (
                      <Button
                        variant="ghost"
                        size="icon"
                        className="text-muted-foreground hover:text-destructive shrink-0"
                        onClick={() => handleDisconnect(inst.id)}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    )}
                  </div>
                ))}
              </div>
            )}

            {!canManage && (
              <p className="text-xs text-muted-foreground">
                Only admins and owners can manage integrations.
              </p>
            )}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
