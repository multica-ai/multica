"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { Save, Plus, Trash2, FolderGit2, FolderOpen } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceStore } from "@multica/core/workspace";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects";
import { api } from "@multica/core/api";
import type { WorkspaceRepo, WorkspaceRepoType } from "@multica/core/types";

// Stable client-generated ids for brand-new rows. Server will accept as-is.
function newRepoId(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  // Fallback for older browsers / SSR.
  return "tmp-" + Math.random().toString(36).slice(2, 10);
}

function makeEmptyRepo(type: WorkspaceRepoType): WorkspaceRepo {
  return {
    id: newRepoId(),
    name: "",
    type,
    url: type === "github" ? "" : undefined,
    local_path: type === "local" ? "" : undefined,
    description: "",
  };
}

export function RepositoriesTab() {
  const user = useAuthStore((s) => s.user);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const updateWorkspace = useWorkspaceStore((s) => s.updateWorkspace);

  const [repos, setRepos] = useState<WorkspaceRepo[]>(workspace?.repos ?? []);
  const [saving, setSaving] = useState(false);

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManageWorkspace =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  // Map repoId → projects that link to it, so each row can show its linked
  // projects as chips. Read-only: click-through navigates to the project.
  const projectsByRepo = useMemo(() => {
    const m = new Map<string, { id: string; title: string }[]>();
    for (const p of projects) {
      for (const rid of p.repo_ids ?? []) {
        const list = m.get(rid) ?? [];
        list.push({ id: p.id, title: p.title });
        m.set(rid, list);
      }
    }
    return m;
  }, [projects]);

  useEffect(() => {
    setRepos(workspace?.repos ?? []);
  }, [workspace]);

  const handleSave = async () => {
    if (!workspace) return;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, { repos });
      updateWorkspace(updated);
      // Sync local state with server-normalized payload (ids, names, etc.).
      setRepos(updated.repos ?? []);
      toast.success("Repositories saved");
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : "Failed to save repositories",
      );
    } finally {
      setSaving(false);
    }
  };

  const handleAddRepo = (type: WorkspaceRepoType) => {
    setRepos([...repos, makeEmptyRepo(type)]);
  };

  const handleRemoveRepo = (index: number) => {
    setRepos(repos.filter((_, i) => i !== index));
  };

  const handleRepoChange = (
    index: number,
    patch: Partial<WorkspaceRepo>,
  ) => {
    setRepos(
      repos.map((r, i) => {
        if (i !== index) return r;
        const next = { ...r, ...patch };
        // When toggling type, clear fields that no longer apply so the
        // server doesn't reject a half-populated entry.
        if (patch.type && patch.type !== r.type) {
          next.url = patch.type === "github" ? (next.url ?? "") : undefined;
          next.local_path =
            patch.type === "local" ? (next.local_path ?? "") : undefined;
        }
        return next;
      }),
    );
  };

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">Repositories</h2>

        <Card>
          <CardContent className="space-y-4">
            <p className="text-xs text-muted-foreground">
              Repositories available to agents in this workspace. GitHub repos
              are cloned into an isolated cache; local repos create git
              worktrees directly off your on-disk <code>.git</code>, so the
              original checkout is never touched. Agents pick up the project's
              first repo as their starting directory.
            </p>

            <div className="space-y-4">
              {repos.map((repo, index) => {
                const linked = projectsByRepo.get(repo.id) ?? [];
                return (
                  <div
                    key={repo.id || index}
                    className="space-y-2 rounded-md border border-border/60 p-3"
                  >
                    <div className="flex items-start gap-2">
                      <div className="flex-1 space-y-2">
                        <div className="flex items-center gap-2">
                          <div className="flex h-8 shrink-0 overflow-hidden rounded-md border">
                            {(["github", "local"] as WorkspaceRepoType[]).map(
                              (t) => (
                                <button
                                  key={t}
                                  type="button"
                                  onClick={() =>
                                    handleRepoChange(index, { type: t })
                                  }
                                  disabled={!canManageWorkspace}
                                  className={cn(
                                    "flex shrink-0 items-center gap-1 whitespace-nowrap px-2.5 text-xs transition-colors",
                                    repo.type === t
                                      ? "bg-accent text-accent-foreground"
                                      : "bg-background hover:bg-accent/50",
                                    !canManageWorkspace &&
                                      "cursor-not-allowed opacity-60",
                                  )}
                                >
                                  {t === "github" ? (
                                    <FolderGit2 className="h-3 w-3" />
                                  ) : (
                                    <FolderOpen className="h-3 w-3" />
                                  )}
                                  {t === "github" ? "GitHub" : "Local"}
                                </button>
                              ),
                            )}
                          </div>
                          <Input
                            type="text"
                            value={repo.name}
                            onChange={(e) =>
                              handleRepoChange(index, { name: e.target.value })
                            }
                            disabled={!canManageWorkspace}
                            placeholder="Name (e.g. multica)"
                            className="min-w-0 flex-1 text-sm h-8"
                          />
                        </div>

                        {repo.type === "github" ? (
                          <Input
                            type="url"
                            value={repo.url ?? ""}
                            onChange={(e) =>
                              handleRepoChange(index, { url: e.target.value })
                            }
                            disabled={!canManageWorkspace}
                            placeholder="https://github.com/org/repo"
                            className="text-sm"
                          />
                        ) : (
                          <Input
                            type="text"
                            value={repo.local_path ?? ""}
                            onChange={(e) =>
                              handleRepoChange(index, {
                                local_path: e.target.value,
                              })
                            }
                            disabled={!canManageWorkspace}
                            placeholder="/Users/you/projects/my-repo"
                            className="text-sm font-mono"
                          />
                        )}

                        <Input
                          type="text"
                          value={repo.description}
                          onChange={(e) =>
                            handleRepoChange(index, {
                              description: e.target.value,
                            })
                          }
                          disabled={!canManageWorkspace}
                          placeholder="Description (e.g. Go backend + Next.js frontend)"
                          className="text-sm"
                        />

                        {linked.length > 0 && (
                          <div className="flex flex-wrap items-center gap-1 pt-0.5">
                            <span className="text-[11px] text-muted-foreground">
                              Linked projects:
                            </span>
                            {linked.map((p) => (
                              <Link
                                key={p.id}
                                href={`/projects/${p.id}`}
                                className="no-underline"
                              >
                                <Badge
                                  variant="secondary"
                                  className="h-5 text-[10px]"
                                >
                                  {p.title}
                                </Badge>
                              </Link>
                            ))}
                          </div>
                        )}
                      </div>
                      {canManageWorkspace && (
                        <Button
                          variant="ghost"
                          size="icon"
                          className="mt-0.5 shrink-0 text-muted-foreground hover:text-destructive"
                          onClick={() => handleRemoveRepo(index)}
                          aria-label={`Remove ${repo.name || "repository"}`}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>

            {canManageWorkspace && (
              <div className="flex items-center justify-between pt-1">
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => handleAddRepo("github")}
                  >
                    <Plus className="h-3 w-3" />
                    GitHub repo
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => handleAddRepo("local")}
                  >
                    <Plus className="h-3 w-3" />
                    Local path
                  </Button>
                </div>
                <Button size="sm" onClick={handleSave} disabled={saving}>
                  <Save className="h-3 w-3" />
                  {saving ? "Saving..." : "Save"}
                </Button>
              </div>
            )}

            {!canManageWorkspace && (
              <p className="text-xs text-muted-foreground">
                Only admins and owners can manage repositories.
              </p>
            )}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
