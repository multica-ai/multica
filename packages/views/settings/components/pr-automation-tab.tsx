"use client";

import { useState } from "react";
import { Plus, Trash2, GitPullRequest, Loader2, Power, PowerOff } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  memberListOptions,
  repoBindingListOptions,
} from "@multica/core/workspace/queries";
import {
  useCreateRepoBinding,
  useDeleteRepoBinding,
  useUpdateRepoBinding,
} from "@multica/core/workspace/mutations";
import type { WorkspaceRepoBinding } from "@multica/core/types";

const REPO_FORMAT_RE = /^[\w.-]+\/[\w.-]+$/;

export function PRAutomationTab() {
  const user = useAuthStore((s) => s.user);
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: bindings = [], isLoading } = useQuery(repoBindingListOptions(wsId));
  const createMut = useCreateRepoBinding(wsId);
  const updateMut = useUpdateRepoBinding(wsId);
  const deleteMut = useDeleteRepoBinding(wsId);

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const [showForm, setShowForm] = useState(false);
  const [repoFullName, setRepoFullName] = useState("");
  const [installationId, setInstallationId] = useState("");
  const [crBotUsername, setCrBotUsername] = useState("");

  const resetForm = () => {
    setRepoFullName("");
    setInstallationId("");
    setCrBotUsername("");
    setShowForm(false);
  };

  const handleCreate = async () => {
    const repo = repoFullName.trim();
    const inst = installationId.trim();
    if (!REPO_FORMAT_RE.test(repo)) {
      toast.error("Repository must be in 'owner/repo' format");
      return;
    }
    const instNum = Number(inst);
    if (!Number.isInteger(instNum) || instNum <= 0) {
      toast.error("Installation ID must be a positive integer");
      return;
    }
    try {
      await createMut.mutateAsync({
        repo_full_name: repo,
        installation_id: instNum,
        cr_bot_username: crBotUsername.trim() || undefined,
      });
      toast.success("Repository binding added");
      resetForm();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to add binding");
    }
  };

  const handleToggleActive = async (b: WorkspaceRepoBinding) => {
    try {
      await updateMut.mutateAsync({
        bindingId: b.id,
        data: { active: !b.active },
      });
      toast.success(b.active ? "Binding paused" : "Binding activated");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update binding");
    }
  };

  const handleDelete = async (b: WorkspaceRepoBinding) => {
    if (!confirm(`Remove binding for ${b.repo_full_name}? GitHub events from this repo will stop updating Multica issues.`)) {
      return;
    }
    try {
      await deleteMut.mutateAsync(b.id);
      toast.success("Binding removed");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to remove binding");
    }
  };

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold flex items-center gap-2">
          <GitPullRequest className="h-4 w-4" />
          PR Automation
        </h2>

        <Card>
          <CardContent className="space-y-3">
            <p className="text-xs text-muted-foreground">
              Bind GitHub repositories to this workspace so pull-request events
              (open, review, merge) automatically move the linked Multica
              issue between <span className="font-medium">Backlog → In Review → Staged → Done</span>.
              Issues are matched by identifier (e.g. <code>MUL-50</code>) found
              in the PR branch, body, or title.
            </p>

            {isLoading && (
              <div className="flex items-center gap-2 py-3 text-sm text-muted-foreground">
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                Loading bindings…
              </div>
            )}

            {!isLoading && bindings.length === 0 && !showForm && (
              <div className="rounded-md border border-dashed py-6 text-center text-sm text-muted-foreground">
                No repository bindings yet.
              </div>
            )}

            {bindings.length > 0 && (
              <ul className="divide-y rounded-md border">
                {bindings.map((b) => (
                  <li
                    key={b.id}
                    data-testid={`repo-binding-${b.id}`}
                    className="flex items-center justify-between gap-3 px-3 py-2"
                  >
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="font-mono text-sm">
                          {b.repo_full_name}
                        </span>
                        {!b.active && (
                          <span className="text-[10px] uppercase tracking-wide text-muted-foreground bg-muted px-1.5 py-0.5 rounded">
                            paused
                          </span>
                        )}
                      </div>
                      <div className="text-xs text-muted-foreground truncate">
                        Installation {b.installation_id} · CodeRabbit bot{" "}
                        <code>{b.cr_bot_username}</code>
                      </div>
                    </div>
                    {canManage && (
                      <div className="flex items-center gap-1 shrink-0">
                        <Button
                          variant="ghost"
                          size="icon"
                          title={b.active ? "Pause" : "Activate"}
                          onClick={() => handleToggleActive(b)}
                          disabled={updateMut.isPending}
                        >
                          {b.active ? (
                            <PowerOff className="h-3.5 w-3.5" />
                          ) : (
                            <Power className="h-3.5 w-3.5" />
                          )}
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="text-muted-foreground hover:text-destructive"
                          title="Remove"
                          onClick={() => handleDelete(b)}
                          disabled={deleteMut.isPending}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    )}
                  </li>
                ))}
              </ul>
            )}

            {canManage && showForm && (
              <div className="space-y-2 rounded-md border p-3">
                <Input
                  type="text"
                  placeholder="owner/repo (e.g. zeyad-farrag/multica)"
                  value={repoFullName}
                  onChange={(e) => setRepoFullName(e.target.value)}
                  className="text-sm"
                />
                <Input
                  type="text"
                  placeholder="GitHub App installation ID (e.g. 127217055)"
                  value={installationId}
                  onChange={(e) => setInstallationId(e.target.value)}
                  inputMode="numeric"
                  className="text-sm"
                />
                <Input
                  type="text"
                  placeholder="CodeRabbit bot username (default: coderabbitai[bot])"
                  value={crBotUsername}
                  onChange={(e) => setCrBotUsername(e.target.value)}
                  className="text-sm"
                />
                <div className="flex justify-end gap-2 pt-1">
                  <Button variant="outline" size="sm" onClick={resetForm}>
                    Cancel
                  </Button>
                  <Button
                    size="sm"
                    onClick={handleCreate}
                    disabled={createMut.isPending}
                  >
                    {createMut.isPending ? "Adding…" : "Add binding"}
                  </Button>
                </div>
              </div>
            )}

            {canManage && !showForm && (
              <div className="flex justify-end">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setShowForm(true)}
                >
                  <Plus className="h-3 w-3" />
                  Add binding
                </Button>
              </div>
            )}

            {!canManage && (
              <p className="text-xs text-muted-foreground">
                Only admins and owners can manage repository bindings.
              </p>
            )}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
