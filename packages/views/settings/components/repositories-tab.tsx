"use client";

import { useEffect, useState } from "react";
import { ChevronRight, Monitor, Plus, Save, Trash2, X } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { NativeSelect, NativeSelectOption } from "@multica/ui/components/ui/native-select";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import type { AgentRuntime, Workspace, WorkspaceRepo } from "@multica/core/types";

type MachinePathDraft = {
  ownerId: string;
  runtimeId: string;
  path: string;
};

function deviceNameForRuntime(runtime: AgentRuntime): string {
  return runtime.device_name || runtime.device_info.split(" · ")[0] || runtime.name;
}

export function RepositoriesTab() {
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));

  const [repos, setRepos] = useState<WorkspaceRepo[]>(workspace?.repos ?? []);
  const [saving, setSaving] = useState(false);
  const [machinePathsOpen, setMachinePathsOpen] = useState<Record<number, boolean>>({});
  const [machineDrafts, setMachineDrafts] = useState<Record<number, MachinePathDraft>>({});

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManageWorkspace = currentMember?.role === "owner" || currentMember?.role === "admin";
  const membersWithRuntimes = members.filter((member) =>
    runtimes.some((runtime) => runtime.owner_id === member.user_id),
  );

  useEffect(() => {
    setRepos(workspace?.repos ?? []);
  }, [workspace]);

  const handleSave = async () => {
    if (!workspace) return;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, { repos });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success("Repositories saved");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to save repositories");
    } finally {
      setSaving(false);
    }
  };

  const handleAddRepo = () => {
    setRepos([...repos, { url: "", description: "", local_path: "" }]);
  };

  const handleRemoveRepo = (index: number) => {
    setRepos(repos.filter((_, i) => i !== index));
  };

  const handleRepoChange = (index: number, field: keyof WorkspaceRepo, value: string) => {
    setRepos(repos.map((r, i) => (i === index ? { ...r, [field]: value } : r)));
  };

  const getMachineDraft = (index: number): MachinePathDraft => {
    const draft = machineDrafts[index];
    const ownerId = draft?.ownerId || membersWithRuntimes[0]?.user_id || "";
    const ownerRuntimes = runtimes.filter((runtime) => runtime.owner_id === ownerId);
    const runtimeId = ownerRuntimes.some((runtime) => runtime.id === draft?.runtimeId)
      ? draft?.runtimeId ?? ""
      : ownerRuntimes[0]?.id ?? "";
    return { ownerId, runtimeId, path: draft?.path ?? "" };
  };

  const updateMachineDraft = (index: number, draft: MachinePathDraft) => {
    setMachineDrafts((current) => ({ ...current, [index]: draft }));
  };

  const handleMachineOwnerChange = (index: number, ownerId: string) => {
    const runtimeId = runtimes.find((runtime) => runtime.owner_id === ownerId)?.id ?? "";
    updateMachineDraft(index, { ...getMachineDraft(index), ownerId, runtimeId });
  };

  const handleMachinePathChange = (index: number, deviceName: string, path: string | null) => {
    setRepos(repos.map((repo, i) => {
      if (i !== index) return repo;
      const nextPaths = { ...(repo.machine_paths ?? {}) };
      if (path === null) {
        delete nextPaths[deviceName];
      } else {
        nextPaths[deviceName] = path;
      }
      return {
        ...repo,
        machine_paths: Object.keys(nextPaths).length > 0 ? nextPaths : undefined,
      };
    }));
  };

  const handleAddMachinePath = (index: number) => {
    const draft = getMachineDraft(index);
    const runtime = runtimes.find((item) => item.id === draft.runtimeId);
    const deviceName = runtime ? deviceNameForRuntime(runtime) : "";
    const path = draft.path.trim();
    if (!deviceName || !path) return;

    handleMachinePathChange(index, deviceName, path);
    updateMachineDraft(index, { ...draft, path: "" });
  };

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">Repositories</h2>

        <Card>
          <CardContent className="space-y-3">
            <p className="text-xs text-muted-foreground">
              Git repositories associated with this workspace. Agents use these to clone and work on code.
            </p>

            {repos.map((repo, index) => {
              const draft = getMachineDraft(index);
              const ownerRuntimes = runtimes
                .filter((runtime) => runtime.owner_id === draft.ownerId)
                .filter((runtime, idx, arr) => arr.findIndex((r) => deviceNameForRuntime(r) === deviceNameForRuntime(runtime)) === idx);
              const machinePathEntries = Object.entries(repo.machine_paths ?? {});

              return (
                <div key={index} className="space-y-3 border-b border-border/70 pb-3 last:border-b-0 last:pb-0">
                  <div className="flex gap-2">
                    <div className="flex-1 space-y-1.5">
                      <Input
                        type="url"
                        value={repo.url ?? ""}
                        onChange={(e) => handleRepoChange(index, "url", e.target.value)}
                        disabled={!canManageWorkspace}
                        placeholder="https://git.example.com/org/repo.git"
                        className="text-sm"
                      />
                      <Input
                        type="text"
                        value={repo.local_path ?? ""}
                        onChange={(e) => handleRepoChange(index, "local_path", e.target.value)}
                        disabled={!canManageWorkspace}
                        placeholder="/home/user/projects/my-app (fallback local path, optional)"
                        className="text-sm"
                      />
                      <Input
                        type="text"
                        value={repo.description}
                        onChange={(e) => handleRepoChange(index, "description", e.target.value)}
                        disabled={!canManageWorkspace}
                        placeholder="Description (e.g. Go backend + Next.js frontend)"
                        className="text-sm"
                      />
                    </div>
                    {canManageWorkspace && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        className="mt-0.5 shrink-0 text-muted-foreground hover:text-destructive"
                        aria-label="Remove repository"
                        onClick={() => handleRemoveRepo(index)}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    )}
                  </div>

                  <div className="space-y-2">
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="h-7 px-2 text-xs text-muted-foreground"
                      onClick={() => setMachinePathsOpen((current) => ({ ...current, [index]: !current[index] }))}
                    >
                      <Monitor className="h-3.5 w-3.5" />
                      Machine paths
                      <ChevronRight className={`h-3.5 w-3.5 transition-transform ${machinePathsOpen[index] ? "rotate-90" : ""}`} />
                    </Button>

                    {machinePathsOpen[index] && (
                      <div className="space-y-2 rounded-md bg-muted/30 p-2">
                        {machinePathEntries.length > 0 && (
                          <div className="space-y-1">
                            {machinePathEntries.map(([deviceName, path]) => {
                              const runtime = runtimes.find((item) => deviceNameForRuntime(item) === deviceName);
                              const owner = runtime?.owner_id
                                ? members.find((member) => member.user_id === runtime.owner_id)
                                : null;

                              return (
                                <div key={deviceName} className="flex items-center gap-2 rounded-md bg-background px-2 py-1.5 text-xs">
                                  <div className="min-w-0 flex-1">
                                    <div className="truncate font-medium">{owner?.name ?? deviceName}</div>
                                    <div className="truncate text-muted-foreground">{deviceName} · {path}</div>
                                  </div>
                                  {canManageWorkspace && (
                                    <Button
                                      type="button"
                                      variant="ghost"
                                      size="icon"
                                      className="h-6 w-6 shrink-0 text-muted-foreground hover:text-destructive"
                                      aria-label={`Remove ${deviceName} path`}
                                      onClick={() => handleMachinePathChange(index, deviceName, null)}
                                    >
                                      <X className="h-3.5 w-3.5" />
                                    </Button>
                                  )}
                                </div>
                              );
                            })}
                          </div>
                        )}

                        {canManageWorkspace && membersWithRuntimes.length > 0 && (
                          <div className="grid gap-2 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.4fr)_auto]">
                            <NativeSelect
                              size="sm"
                              className="w-full"
                              value={draft.ownerId}
                              onChange={(e) => handleMachineOwnerChange(index, e.target.value)}
                            >
                              {membersWithRuntimes.map((member) => (
                                <NativeSelectOption key={member.user_id} value={member.user_id}>
                                  {member.name || member.email}
                                </NativeSelectOption>
                              ))}
                            </NativeSelect>
                            <NativeSelect
                              size="sm"
                              className="w-full"
                              value={draft.runtimeId}
                              onChange={(e) => updateMachineDraft(index, { ...draft, runtimeId: e.target.value })}
                            >
                              {ownerRuntimes.map((runtime) => (
                                <NativeSelectOption key={runtime.id} value={runtime.id}>
                                  {deviceNameForRuntime(runtime)}
                                </NativeSelectOption>
                              ))}
                            </NativeSelect>
                            <Input
                              value={draft.path}
                              onChange={(e) => updateMachineDraft(index, { ...draft, path: e.target.value })}
                              placeholder="Local path on this machine"
                              className="h-7 text-sm"
                            />
                            <Button
                              type="button"
                              size="sm"
                              className="h-7"
                              disabled={!draft.runtimeId || !draft.path.trim()}
                              onClick={() => handleAddMachinePath(index)}
                            >
                              <Plus className="h-3 w-3" />
                              Add
                            </Button>
                          </div>
                        )}

                        {canManageWorkspace && runtimes.length === 0 && (
                          <p className="text-xs text-muted-foreground">No runtimes registered.</p>
                        )}
                        {canManageWorkspace && runtimes.length > 0 && membersWithRuntimes.length === 0 && (
                          <p className="text-xs text-muted-foreground">No user-owned runtimes registered.</p>
                        )}
                      </div>
                    )}
                  </div>
                </div>
              );
            })}

            {canManageWorkspace && (
              <div className="flex items-center justify-between pt-1">
                <Button variant="outline" size="sm" onClick={handleAddRepo}>
                  <Plus className="h-3 w-3" />
                  Add repository
                </Button>
                <Button
                  size="sm"
                  onClick={handleSave}
                  disabled={saving}
                >
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
