"use client";

import { useEffect, useState, useCallback } from "react";
import { GitBranch, Trash2, ExternalLink, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { toast } from "sonner";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { api } from "@/shared/api";
import type { GitHubInstallation } from "@/shared/types";
import { useWSEvent } from "@/features/realtime";

export function IntegrationsTab() {
  const user = useAuthStore((s) => s.user);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const members = useWorkspaceStore((s) => s.members);

  const [installations, setInstallations] = useState<GitHubInstallation[]>([]);
  const [loading, setLoading] = useState(true);

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";

  const fetchInstallations = useCallback(async () => {
    try {
      const data = await api.listGitHubInstallations();
      setInstallations(data);
    } catch {
      // Silently handle — workspace may not have any installations
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchInstallations();
  }, [fetchInstallations]);

  // Real-time updates
  useWSEvent(
    "github_installation:created",
    useCallback(() => fetchInstallations(), [fetchInstallations]),
  );
  useWSEvent(
    "github_installation:deleted",
    useCallback(() => fetchInstallations(), [fetchInstallations]),
  );

  const handleConnect = () => {
    const appSlug = process.env.NEXT_PUBLIC_GITHUB_APP_SLUG;
    if (!appSlug) {
      toast.error("GitHub App not configured. Set NEXT_PUBLIC_GITHUB_APP_SLUG.");
      return;
    }
    // The callback URL is configured in the GitHub App settings.
    // We pass workspace_id as state so the backend knows which workspace to link.
    const callbackUrl = `${window.location.origin}/api/github/callback`;
    const installUrl = `https://github.com/apps/${appSlug}/installations/new?state=${workspace?.id}`;
    window.open(installUrl, "_blank", "noopener,noreferrer");
  };

  const handleDisconnect = async (installation: GitHubInstallation) => {
    try {
      await api.deleteGitHubInstallation(installation.installation_id);
      setInstallations((prev) => prev.filter((i) => i.id !== installation.id));
      toast.success(`Disconnected ${installation.account_login}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to disconnect");
    }
  };

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">GitHub Integration</h2>

        <Card>
          <CardContent className="space-y-4">
            <p className="text-xs text-muted-foreground">
              Connect your GitHub repositories to automatically link pull requests to issues.
              When a PR branch or description contains an issue identifier (e.g. MUL-82),
              the PR status will appear on the issue and merging will mark it as done.
            </p>

            {loading ? (
              <div className="text-xs text-muted-foreground">Loading...</div>
            ) : installations.length > 0 ? (
              <div className="space-y-2">
                {installations.map((gi) => (
                  <div
                    key={gi.id}
                    className="flex items-center justify-between rounded-lg border p-3"
                  >
                    <div className="flex items-center gap-3">
                      <GitBranch className="h-5 w-5 text-muted-foreground" />
                      <div>
                        <div className="text-sm font-medium">{gi.account_login}</div>
                        <div className="text-xs text-muted-foreground">
                          {gi.account_type} &middot; Connected{" "}
                          {new Date(gi.created_at).toLocaleDateString()}
                        </div>
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <a
                        href={`https://github.com/settings/installations/${gi.installation_id}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center justify-center h-8 w-8 rounded-md hover:bg-accent transition-colors"
                      >
                        <ExternalLink className="h-3.5 w-3.5 text-muted-foreground" />
                      </a>
                      {canManage && (
                        <Button
                          variant="ghost"
                          size="icon"
                          className="text-muted-foreground hover:text-destructive"
                          onClick={() => handleDisconnect(gi)}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="rounded-lg border border-dashed p-6 text-center">
                <GitBranch className="mx-auto h-8 w-8 text-muted-foreground/50 mb-2" />
                <p className="text-sm text-muted-foreground mb-1">
                  No GitHub accounts connected
                </p>
                <p className="text-xs text-muted-foreground">
                  Install the Multica GitHub App to link PRs with issues automatically.
                </p>
              </div>
            )}

            {canManage && (
              <div className="pt-1">
                <Button variant="outline" size="sm" onClick={handleConnect}>
                  <Plus className="h-3 w-3" />
                  Connect GitHub
                </Button>
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
