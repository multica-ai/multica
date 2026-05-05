"use client";

import { useEffect, useState } from "react";
import { Save, Plus, Trash2 } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Badge } from "@multica/ui/components/ui/badge";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useConfigStore } from "@multica/core/config";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import type { Workspace, WorkspaceRepoStatus } from "@multica/core/types";
import { useT } from "../../i18n";

// Local repo shape: status is optional so unsaved rows can be edited before
// the server stamps an initial status. Persisted rows always carry one.
type RepoDraft = { url: string; status?: WorkspaceRepoStatus };

export function RepositoriesTab() {
  const { t } = useT("settings");
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const repoApprovalRequired = useConfigStore((s) => s.repoApprovalRequired);

  const [repos, setRepos] = useState<RepoDraft[]>(workspace?.repos ?? []);
  const [saving, setSaving] = useState(false);

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManageWorkspace = currentMember?.role === "owner" || currentMember?.role === "admin";

  useEffect(() => {
    setRepos(workspace?.repos ?? []);
  }, [workspace]);

  const handleSave = async () => {
    if (!workspace) return;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        repos: repos.map((r) => ({ url: r.url, status: r.status })),
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success(t(($) => $.repositories.toast_saved));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.repositories.toast_save_failed));
    } finally {
      setSaving(false);
    }
  };

  const handleAddRepo = () => {
    setRepos([...repos, { url: "" }]);
  };

  const handleRemoveRepo = (index: number) => {
    setRepos(repos.filter((_, i) => i !== index));
  };

  const handleRepoChange = (index: number, value: string) => {
    setRepos(repos.map((r, i) => (i === index ? { ...r, url: value } : r)));
  };

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.repositories.section_title)}</h2>

        <Card>
          <CardContent className="space-y-3">
            <p className="text-xs text-muted-foreground">
              {t(($) => $.repositories.description)}
            </p>

            {repos.map((repo, index) => (
              <div key={index} className="flex items-center gap-2">
                <div className="relative flex-1 min-w-0">
                  <Input
                    type="text"
                    value={repo.url}
                    onChange={(e) => handleRepoChange(index, e.target.value)}
                    disabled={!canManageWorkspace}
                    placeholder={t(($) => $.repositories.url_placeholder)}
                    className={repoApprovalRequired && repo.status ? "pr-24 text-sm" : "text-sm"}
                  />
                  {repoApprovalRequired && repo.status && (
                    <div className="pointer-events-none absolute inset-y-0 right-1.5 flex items-center">
                      <RepoStatusBadge status={repo.status} />
                    </div>
                  )}
                </div>
                {canManageWorkspace && (
                  <Button
                    variant="ghost"
                    size="icon"
                    className="shrink-0 text-muted-foreground hover:text-destructive"
                    onClick={() => handleRemoveRepo(index)}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                )}
              </div>
            ))}

            {canManageWorkspace && (
              <div className="flex flex-wrap items-center justify-between gap-2 pt-1">
                <Button variant="outline" size="sm" onClick={handleAddRepo}>
                  <Plus className="h-3 w-3" />
                  {t(($) => $.repositories.add)}
                </Button>
                <Button
                  size="sm"
                  onClick={handleSave}
                  disabled={saving}
                >
                  <Save className="h-3 w-3" />
                  {saving ? t(($) => $.repositories.saving) : t(($) => $.repositories.save)}
                </Button>
              </div>
            )}

            {!canManageWorkspace && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.repositories.manage_hint)}
              </p>
            )}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}

function RepoStatusBadge({ status }: { status?: WorkspaceRepoStatus }) {
  const { t } = useT("settings");
  if (status === "approved") {
    return (
      <Badge variant="secondary" className="h-5 shrink-0 bg-success/10 text-success">
        {t(($) => $.repositories.status_approved)}
      </Badge>
    );
  }
  return (
    <Badge variant="secondary" className="h-5 shrink-0 bg-warning/10 text-warning">
      {t(($) => $.repositories.status_pending)}
    </Badge>
  );
}
