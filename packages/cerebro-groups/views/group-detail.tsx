"use client";

// JEH-1067 (Bundle B) — Group detail page. Reachable from the Groups list in
// Settings → Groups. Shows the four sub-sections (Members, Agents, Runtimes,
// Capabilities) as a vertical stack on its own route rather than inlined into
// the settings tab. The Agents section additionally surfaces the runtime each
// agent runs on as a `Kører på: <runtime>` badge, with an inline "tilføj
// runtime" CTA when the group's runtime allowlist doesn't yet include that
// runtime. Picking an agent whose runtime is missing prompts the admin to
// "Tilføj begge" via a confirmation dialog (server still does two writes).

import { useMemo, useState } from "react";
import {
  AlertTriangle,
  ArrowLeft,
  Bot,
  Cpu,
  Plus,
  Trash2,
  UserPlus,
  Users,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Agent, RuntimeDevice } from "@multica/core/types";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from "@multica/ui/components/ui/alert-dialog";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  useDeleteCerebroGroup,
  useUpdateCerebroGroup,
  useAddCerebroGroupMember,
  useRemoveCerebroGroupMember,
  useSetCerebroGroupCapability,
  useRemoveCerebroGroupCapability,
  useAddCerebroGroupRuntime,
  useRemoveCerebroGroupRuntime,
  useAddCerebroGroupAgent,
  useRemoveCerebroGroupAgent,
} from "../mutations";
import {
  groupListOptions,
  groupMembersOptions,
  groupCapabilitiesOptions,
  groupRuntimesOptions,
  groupAgentsOptions,
} from "../queries";

function initialsOf(name: string): string {
  return (
    name
      .trim()
      .split(/\s+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((p) => p[0]?.toUpperCase() ?? "")
      .join("") || "?"
  );
}

export interface GroupDetailViewProps {
  groupId: string;
  /**
   * Navigate back to the Groups list (typically `/<slug>/settings?tab=groups`).
   * Passed in by the app layer so cerebro-groups stays decoupled from
   * `@multica/views/navigation` — avoiding a cycle because views already
   * depends on cerebro-groups via the settings-page CEREBRO-PATCH.
   */
  onBack: () => void;
}

export function GroupDetailView({ groupId, onBack }: GroupDetailViewProps) {
  const enabled = useFeatureFlag("cerebro_groups_enabled");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: groups = [], isPending } = useQuery(groupListOptions(wsId));

  const group = groups.find((g) => g.id === groupId);
  const currentMember = members.find((m) => m.user_id === user?.id);
  const isAdmin =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  if (!enabled) return null;

  if (isPending) {
    return (
      <div className="p-6 text-sm text-muted-foreground">Loading group…</div>
    );
  }

  if (!group) {
    return (
      <div className="p-6 space-y-3" data-testid="group-detail-missing">
        <BackButton onBack={onBack} />
        <div className="rounded-md border border-dashed border-border p-6 text-sm text-muted-foreground">
          Group not found. It may have been deleted or you no longer have
          access.
        </div>
      </div>
    );
  }

  return (
    <div
      className="mx-auto w-full max-w-3xl space-y-6 p-4 md:p-6"
      data-testid="group-detail"
    >
      <BackButton onBack={onBack} />

      <GroupHeader group={group} isAdmin={isAdmin} onDeleted={onBack} />

      <MembersSection groupId={groupId} isAdmin={isAdmin} />

      <AgentsSection groupId={groupId} isAdmin={isAdmin} />

      <RuntimesSection groupId={groupId} isAdmin={isAdmin} />

      <CapabilitiesSection groupId={groupId} isAdmin={isAdmin} />
    </div>
  );
}

function BackButton({ onBack }: { onBack: () => void }) {
  return (
    <button
      type="button"
      onClick={onBack}
      data-testid="group-detail-back"
      className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground"
    >
      <ArrowLeft className="size-3.5" /> Back to Groups
    </button>
  );
}

function GroupHeader({
  group,
  isAdmin,
  onDeleted,
}: {
  group: { id: string; name: string; description: string | null };
  isAdmin: boolean;
  onDeleted: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(group.name);
  const [description, setDescription] = useState(group.description ?? "");
  const [confirmDelete, setConfirmDelete] = useState(false);

  const update = useUpdateCerebroGroup();
  const remove = useDeleteCerebroGroup();

  const handleSave = async () => {
    const trimmed = name.trim();
    if (!trimmed) {
      toast.error("Group name is required");
      return;
    }
    try {
      await update.mutateAsync({
        id: group.id,
        body: { name: trimmed, description: description.trim() },
      });
      setEditing(false);
      toast.success("Group updated");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update group");
    }
  };

  const handleDelete = async () => {
    try {
      await remove.mutateAsync(group.id);
      toast.success(`Group "${group.name}" deleted`);
      setConfirmDelete(false);
      onDeleted();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to delete group");
    }
  };

  return (
    <div
      className="rounded-md border border-border p-5"
      data-testid="group-header"
    >
      {editing ? (
        <div className="space-y-3">
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            aria-label="Edit group name"
            className="text-base font-medium"
          />
          <Textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            aria-label="Edit group description"
            placeholder="Optional description"
            rows={2}
          />
          <div className="flex justify-end gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setName(group.name);
                setDescription(group.description ?? "");
                setEditing(false);
              }}
              disabled={update.isPending}
            >
              Cancel
            </Button>
            <Button size="sm" onClick={handleSave} disabled={update.isPending}>
              {update.isPending ? "Saving…" : "Save"}
            </Button>
          </div>
        </div>
      ) : (
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <Users className="size-5 text-muted-foreground shrink-0" />
              <h1 className="text-lg font-semibold truncate">{group.name}</h1>
            </div>
            {group.description && (
              <p className="mt-2 text-sm text-muted-foreground">
                {group.description}
              </p>
            )}
          </div>
          {isAdmin && (
            <div className="flex shrink-0 gap-1">
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setEditing(true)}
              >
                Edit
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={() => setConfirmDelete(true)}
                aria-label={`Delete ${group.name}`}
              >
                <Trash2 className="size-4 text-muted-foreground" />
              </Button>
            </div>
          )}
        </div>
      )}

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete group "{group.name}"?</AlertDialogTitle>
            <AlertDialogDescription>
              Members of this group will lose any access that was granted
              through it. This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={remove.isPending}>
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault();
                void handleDelete();
              }}
              disabled={remove.isPending}
            >
              {remove.isPending ? "Deleting…" : "Delete group"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function SectionShell({
  title,
  icon,
  action,
  count,
  children,
  testId,
}: {
  title: string;
  icon: React.ReactNode;
  action?: React.ReactNode;
  count: number;
  children: React.ReactNode;
  testId: string;
}) {
  return (
    <section
      className="rounded-md border border-border"
      data-testid={testId}
    >
      <header className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
        <div className="flex items-center gap-2 text-sm font-medium">
          {icon}
          <span>{title}</span>
          <span className="text-muted-foreground">({count})</span>
        </div>
        {action}
      </header>
      <div className="p-4">{children}</div>
    </section>
  );
}

function MembersSection({
  groupId,
  isAdmin,
}: {
  groupId: string;
  isAdmin: boolean;
}) {
  const wsId = useWorkspaceId();
  const { data: workspaceMembers = [] } = useQuery(memberListOptions(wsId));
  const { data: groupMembers = [], isPending } = useQuery(
    groupMembersOptions(wsId, groupId),
  );
  const add = useAddCerebroGroupMember(groupId);
  const remove = useRemoveCerebroGroupMember(groupId);
  const [pickerSearch, setPickerSearch] = useState("");
  const [showPicker, setShowPicker] = useState(false);

  const groupMemberIds = useMemo(
    () => new Set(groupMembers.map((m) => m.user_id)),
    [groupMembers],
  );

  const candidates = useMemo(
    () =>
      workspaceMembers
        .filter((m) => !groupMemberIds.has(m.user_id))
        .filter((m) => {
          if (pickerSearch === "") return true;
          const q = pickerSearch.toLowerCase();
          return (
            m.name.toLowerCase().includes(q) ||
            m.email.toLowerCase().includes(q)
          );
        }),
    [workspaceMembers, groupMemberIds, pickerSearch],
  );

  const handleAdd = async (userId: string) => {
    try {
      await add.mutateAsync(userId);
      toast.success("Member added");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to add member");
    }
  };

  const handleRemove = async (userId: string) => {
    try {
      await remove.mutateAsync(userId);
      toast.success("Member removed");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to remove member");
    }
  };

  return (
    <SectionShell
      title="Members"
      icon={<Users className="size-4 text-muted-foreground" />}
      count={groupMembers.length}
      testId="members-section"
      action={
        isAdmin ? (
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowPicker((s) => !s)}
          >
            <UserPlus className="size-4 mr-1.5" />
            Add member
          </Button>
        ) : null
      }
    >
      {isPending ? (
        <div className="text-xs text-muted-foreground">Loading members…</div>
      ) : groupMembers.length === 0 ? (
        <div className="text-xs text-muted-foreground py-1">
          No members yet.
        </div>
      ) : (
        <ul className="divide-y divide-border/50 rounded-md border border-border/50">
          {groupMembers.map((m) => (
            <li
              key={m.user_id}
              className="flex items-center gap-3 px-3 py-2 text-sm"
            >
              <ActorAvatar
                name={m.user_name}
                initials={initialsOf(m.user_name)}
                avatarUrl={m.user_avatar_url}
                size={24}
              />
              <span className="flex-1 min-w-0">
                <span className="block truncate">{m.user_name}</span>
                <span className="block text-xs text-muted-foreground truncate">
                  {m.user_email}
                </span>
              </span>
              {isAdmin && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => handleRemove(m.user_id)}
                  disabled={remove.isPending}
                >
                  Remove
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}

      {showPicker && isAdmin && (
        <div
          className="mt-3 rounded-md border border-border bg-muted/30 p-3 space-y-2"
          data-testid="member-picker"
        >
          <Input
            placeholder="Search workspace members…"
            value={pickerSearch}
            onChange={(e) => setPickerSearch(e.target.value)}
            aria-label="Search workspace members to add"
          />
          <ul className="max-h-60 overflow-auto divide-y divide-border/50">
            {candidates.length === 0 && (
              <li className="text-xs text-muted-foreground py-2 px-1">
                {workspaceMembers.length === groupMembers.length
                  ? "All workspace members are already in this group."
                  : "No matches."}
              </li>
            )}
            {candidates.map((m) => (
              <li
                key={m.user_id}
                className="flex items-center gap-3 px-1 py-2 text-sm"
              >
                <ActorAvatar
                  name={m.name}
                  initials={initialsOf(m.name)}
                  avatarUrl={m.avatar_url}
                  size={20}
                />
                <span className="flex-1 min-w-0">
                  <span className="block truncate">{m.name}</span>
                  <span className="block text-xs text-muted-foreground truncate">
                    {m.email}
                  </span>
                </span>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => handleAdd(m.user_id)}
                  disabled={add.isPending}
                >
                  Add
                </Button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </SectionShell>
  );
}

function RuntimesSection({
  groupId,
  isAdmin,
}: {
  groupId: string;
  isAdmin: boolean;
}) {
  const wsId = useWorkspaceId();
  const { data: rows = [] } = useQuery(groupRuntimesOptions(wsId, groupId));
  const { data: allRuntimes = [] } = useQuery(runtimeListOptions(wsId));
  const add = useAddCerebroGroupRuntime(groupId);
  const remove = useRemoveCerebroGroupRuntime(groupId);
  const [pickerSearch, setPickerSearch] = useState("");
  const [showPicker, setShowPicker] = useState(false);

  const allowed = useMemo(
    () => new Set(rows.map((r) => r.runtime_id)),
    [rows],
  );
  const allowedRuntimes = useMemo(
    () => allRuntimes.filter((rt) => allowed.has(rt.id)),
    [allRuntimes, allowed],
  );
  const candidates = useMemo(
    () =>
      allRuntimes
        .filter((rt) => !allowed.has(rt.id))
        .filter((rt) => {
          if (pickerSearch === "") return true;
          const q = pickerSearch.toLowerCase();
          return (
            (rt.name ?? "").toLowerCase().includes(q) ||
            (rt.provider ?? "").toLowerCase().includes(q)
          );
        }),
    [allRuntimes, allowed, pickerSearch],
  );

  const handleAdd = async (id: string) => {
    try {
      await add.mutateAsync(id);
      toast.success("Runtime added");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to add runtime");
    }
  };

  const handleRemove = async (id: string) => {
    try {
      await remove.mutateAsync(id);
      toast.success("Runtime removed");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to remove runtime");
    }
  };

  return (
    <SectionShell
      title="Runtimes"
      icon={<Cpu className="size-4 text-muted-foreground" />}
      count={allowedRuntimes.length}
      testId="runtimes-section"
      action={
        isAdmin ? (
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowPicker((s) => !s)}
          >
            <Plus className="size-4 mr-1.5" />
            Add runtime
          </Button>
        ) : null
      }
    >
      {allowedRuntimes.length === 0 ? (
        <div className="text-xs text-muted-foreground py-1">
          No runtimes added. Admins can grant access from the picker.
        </div>
      ) : (
        <ul className="divide-y divide-border/50 rounded-md border border-border/50">
          {allowedRuntimes.map((rt) => (
            <li
              key={rt.id}
              className="flex items-center gap-3 px-3 py-2 text-sm"
            >
              <span className="flex-1 min-w-0">
                <span className="block truncate">
                  {rt.name || rt.provider || "Runtime"}
                </span>
                {rt.provider && (
                  <span className="block text-xs text-muted-foreground truncate">
                    {rt.provider}
                  </span>
                )}
              </span>
              {isAdmin && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => handleRemove(rt.id)}
                  disabled={remove.isPending}
                >
                  Remove
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}

      {showPicker && isAdmin && (
        <div className="mt-3 rounded-md border border-border bg-muted/30 p-3 space-y-2">
          <Input
            placeholder="Search runtimes…"
            value={pickerSearch}
            onChange={(e) => setPickerSearch(e.target.value)}
            aria-label="Search runtimes"
          />
          <ul className="max-h-60 overflow-auto divide-y divide-border/50">
            {candidates.length === 0 && (
              <li className="text-xs text-muted-foreground py-2 px-1">
                {allRuntimes.length === allowedRuntimes.length
                  ? "Every workspace runtime is already in the allowlist."
                  : "No matches."}
              </li>
            )}
            {candidates.map((rt) => (
              <li
                key={rt.id}
                className="flex items-center gap-3 px-1 py-2 text-sm"
              >
                <span className="flex-1 min-w-0">
                  <span className="block truncate">
                    {rt.name || rt.provider || "Runtime"}
                  </span>
                  {rt.provider && (
                    <span className="block text-xs text-muted-foreground truncate">
                      {rt.provider}
                    </span>
                  )}
                </span>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => handleAdd(rt.id)}
                  disabled={add.isPending}
                >
                  Add
                </Button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </SectionShell>
  );
}

function AgentsSection({
  groupId,
  isAdmin,
}: {
  groupId: string;
  isAdmin: boolean;
}) {
  const wsId = useWorkspaceId();
  const { data: rows = [] } = useQuery(groupAgentsOptions(wsId, groupId));
  const { data: allAgents = [] } = useQuery(agentListOptions(wsId));
  const { data: allRuntimes = [] } = useQuery(runtimeListOptions(wsId));
  const { data: capabilityRows = [] } = useQuery(
    groupCapabilitiesOptions(wsId, groupId),
  );
  const { data: runtimeRows = [] } = useQuery(
    groupRuntimesOptions(wsId, groupId),
  );
  const addAgent = useAddCerebroGroupAgent(groupId);
  const removeAgent = useRemoveCerebroGroupAgent(groupId);
  const addRuntime = useAddCerebroGroupRuntime(groupId);

  const [pickerSearch, setPickerSearch] = useState("");
  const [showPicker, setShowPicker] = useState(false);
  // Prompt state — when picking an agent whose runtime isn't in the group's
  // allowlist yet, queue a confirmation step instead of silently adding only
  // the agent. The admin can confirm "Tilføj begge" or cancel.
  const [pendingDual, setPendingDual] = useState<{
    agent: Agent;
    runtime: RuntimeDevice;
  } | null>(null);

  const allowed = useMemo(() => new Set(rows.map((r) => r.agent_id)), [rows]);
  const liveAgents = useMemo(
    () => allAgents.filter((a) => !a.archived_at),
    [allAgents],
  );
  const groupRuntimeIds = useMemo(
    () => new Set(runtimeRows.map((r) => r.runtime_id)),
    [runtimeRows],
  );
  const runtimeById = useMemo(() => {
    const m = new Map<string, RuntimeDevice>();
    for (const rt of allRuntimes) m.set(rt.id, rt);
    return m;
  }, [allRuntimes]);
  const hasCreateAgent = useMemo(
    () => capabilityRows.some((r) => r.capability === "create_agent"),
    [capabilityRows],
  );
  const allowedAgents = useMemo(
    () => liveAgents.filter((a) => allowed.has(a.id)),
    [liveAgents, allowed],
  );
  const candidates = useMemo(
    () =>
      liveAgents
        .filter((a) => !allowed.has(a.id))
        .filter((a) => hasCreateAgent || a.visibility !== "private")
        .filter((a) => {
          if (pickerSearch === "") return true;
          const q = pickerSearch.toLowerCase();
          return (
            a.name.toLowerCase().includes(q) ||
            (a.description ?? "").toLowerCase().includes(q)
          );
        }),
    [liveAgents, allowed, hasCreateAgent, pickerSearch],
  );

  const handleAddAgent = async (agentId: string) => {
    const agent = liveAgents.find((a) => a.id === agentId);
    if (!agent) return;
    const runtime = runtimeById.get(agent.runtime_id);
    if (runtime && !groupRuntimeIds.has(runtime.id)) {
      setPendingDual({ agent, runtime });
      return;
    }
    try {
      await addAgent.mutateAsync(agentId);
      toast.success("Agent added");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to add agent");
    }
  };

  const handleRemoveAgent = async (agentId: string) => {
    try {
      await removeAgent.mutateAsync(agentId);
      toast.success("Agent removed");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to remove agent");
    }
  };

  const handleAddRuntimeFromBadge = async (runtimeId: string) => {
    try {
      await addRuntime.mutateAsync(runtimeId);
      toast.success("Runtime added");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to add runtime");
    }
  };

  const handleConfirmDual = async () => {
    if (!pendingDual) return;
    try {
      await addRuntime.mutateAsync(pendingDual.runtime.id);
      await addAgent.mutateAsync(pendingDual.agent.id);
      toast.success("Agent og runtime tilføjet");
      setPendingDual(null);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : "Kunne ikke tilføje agent + runtime",
      );
    }
  };

  return (
    <SectionShell
      title="Agents"
      icon={<Bot className="size-4 text-muted-foreground" />}
      count={allowedAgents.length}
      testId="agents-section"
      action={
        isAdmin ? (
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowPicker((s) => !s)}
          >
            <Plus className="size-4 mr-1.5" />
            Add agent
          </Button>
        ) : null
      }
    >
      {allowedAgents.length === 0 ? (
        <div className="text-xs text-muted-foreground py-1">
          No agents added. Admins can grant access from the picker.
        </div>
      ) : (
        <ul className="divide-y divide-border/50 rounded-md border border-border/50">
          {allowedAgents.map((a) => {
            const runtime = runtimeById.get(a.runtime_id);
            const runtimeMissing =
              runtime != null && !groupRuntimeIds.has(runtime.id);
            return (
              <li
                key={a.id}
                className="flex items-center gap-3 px-3 py-2 text-sm"
              >
                <span className="flex-1 min-w-0">
                  <span className="flex items-center gap-1.5">
                    <span className="truncate">{a.name}</span>
                    {a.visibility === "private" && (
                      <Tooltip>
                        <TooltipTrigger
                          render={
                            <Badge
                              variant="outline"
                              className="shrink-0 text-[10px] uppercase tracking-wide"
                            >
                              Private
                            </Badge>
                          }
                        />
                        <TooltipContent>
                          Private agent — only its owner and workspace admins
                          can see it.
                        </TooltipContent>
                      </Tooltip>
                    )}
                  </span>
                  <span className="mt-0.5 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                    {runtime ? (
                      <>
                        <Badge
                          variant="outline"
                          className="text-[10px]"
                          data-testid="agent-runtime-badge"
                        >
                          Kører på: {runtime.name || runtime.provider}
                        </Badge>
                        {runtimeMissing && isAdmin && (
                          <>
                            <Tooltip>
                              <TooltipTrigger
                                render={
                                  <Badge
                                    variant="destructive"
                                    className="text-[10px]"
                                    data-testid="agent-runtime-missing-badge"
                                    aria-label={`Mangler runtime ${runtime.name || runtime.provider} i gruppen`}
                                  >
                                    <AlertTriangle className="mr-1 size-3" />
                                    Mangler runtime {runtime.name || runtime.provider} i gruppen
                                  </Badge>
                                }
                              />
                              <TooltipContent>
                                Gruppen har ikke runtime "{runtime.name || runtime.provider}" på allowlist'en, så medlemmerne kan ikke trigge denne agent.
                              </TooltipContent>
                            </Tooltip>
                            <Button
                              variant="link"
                              size="sm"
                              className="h-auto p-0 text-xs"
                              data-testid="agent-runtime-quick-add"
                              onClick={() =>
                                void handleAddRuntimeFromBadge(runtime.id)
                              }
                              disabled={addRuntime.isPending}
                            >
                              Tilføj runtime
                            </Button>
                          </>
                        )}
                      </>
                    ) : (
                      <span>Ingen runtime registreret</span>
                    )}
                  </span>
                </span>
                {isAdmin && (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleRemoveAgent(a.id)}
                    disabled={removeAgent.isPending}
                  >
                    Remove
                  </Button>
                )}
              </li>
            );
          })}
        </ul>
      )}

      {showPicker && isAdmin && (
        <div className="mt-3 rounded-md border border-border bg-muted/30 p-3 space-y-2">
          <Input
            placeholder="Search agents…"
            value={pickerSearch}
            onChange={(e) => setPickerSearch(e.target.value)}
            aria-label="Search agents"
          />
          <ul className="max-h-60 overflow-auto divide-y divide-border/50">
            {candidates.length === 0 && (
              <li className="text-xs text-muted-foreground py-2 px-1">
                No matches.
              </li>
            )}
            {candidates.map((a) => {
              const runtime = runtimeById.get(a.runtime_id);
              return (
                <li
                  key={a.id}
                  className="flex items-center gap-3 px-1 py-2 text-sm"
                >
                  <span className="flex-1 min-w-0">
                    <span className="block truncate">{a.name}</span>
                    <span className="block text-xs text-muted-foreground truncate">
                      {runtime
                        ? `Kører på: ${runtime.name || runtime.provider}`
                        : "Ingen runtime"}
                    </span>
                  </span>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => handleAddAgent(a.id)}
                    disabled={addAgent.isPending}
                  >
                    Add
                  </Button>
                </li>
              );
            })}
          </ul>
        </div>
      )}

      <AlertDialog
        open={pendingDual != null}
        onOpenChange={(open) => {
          if (!open) setPendingDual(null);
        }}
      >
        <AlertDialogContent data-testid="add-runtime-prompt">
          <AlertDialogHeader>
            <AlertDialogTitle>
              Denne agent kræver runtime {pendingDual?.runtime.name || pendingDual?.runtime.provider}
            </AlertDialogTitle>
            <AlertDialogDescription>
              Agenten "{pendingDual?.agent.name}" kører på en runtime der ikke er
              i gruppens allowlist. Hvis du tilføjer agenten alene, kan
              gruppens medlemmer stadig ikke trigge den. Tilføj begge?
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel
              disabled={addRuntime.isPending || addAgent.isPending}
            >
              Annullér
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault();
                void handleConfirmDual();
              }}
              disabled={addRuntime.isPending || addAgent.isPending}
            >
              {addRuntime.isPending || addAgent.isPending
                ? "Tilføjer…"
                : "Tilføj begge"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SectionShell>
  );
}

function CapabilitiesSection({
  groupId,
  isAdmin,
}: {
  groupId: string;
  isAdmin: boolean;
}) {
  const wsId = useWorkspaceId();
  const { data: rows = [] } = useQuery(
    groupCapabilitiesOptions(wsId, groupId),
  );
  const grant = useSetCerebroGroupCapability(groupId);
  const revoke = useRemoveCerebroGroupCapability(groupId);

  const has = (cap: string) => rows.some((r) => r.capability === cap);

  const onToggle = async (cap: string, next: boolean) => {
    try {
      if (next) {
        await grant.mutateAsync(cap);
      } else {
        await revoke.mutateAsync(cap);
      }
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : "Failed to update capability",
      );
    }
  };

  return (
    <section
      className="rounded-md border border-border"
      data-testid="capabilities-section"
    >
      <header className="border-b border-border px-4 py-3 text-sm font-medium">
        Capabilities
      </header>
      <div className="space-y-4 p-4">
        <CapabilityRow
          label="Create runtimes"
          tooltip="Members i denne gruppe kan oprette egne runtimes. De får automatisk adgang til runtimes de selv har oprettet, uanset andre group-permissions."
          checked={has("create_runtime")}
          disabled={!isAdmin || grant.isPending || revoke.isPending}
          onChange={(next) => onToggle("create_runtime", next)}
          testId="capability-create-runtime"
        />
        <CapabilityRow
          label="Create agents"
          tooltip="Members i denne gruppe kan oprette egne agenter. De får automatisk adgang til agenter de selv har oprettet, uanset andre group-permissions."
          checked={has("create_agent")}
          disabled={!isAdmin || grant.isPending || revoke.isPending}
          onChange={(next) => onToggle("create_agent", next)}
          testId="capability-create-agent"
        />
      </div>
    </section>
  );
}

function CapabilityRow({
  label,
  tooltip,
  checked,
  disabled,
  onChange,
  testId,
}: {
  label: string;
  tooltip: string;
  checked: boolean;
  disabled: boolean;
  onChange: (next: boolean) => void;
  testId: string;
}) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="min-w-0 flex-1">
        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type="button"
                className="cursor-help text-left text-sm font-medium"
                data-testid={testId}
              >
                {label}
              </button>
            }
          />
          <TooltipContent>{tooltip}</TooltipContent>
        </Tooltip>
        <p className="mt-0.5 text-xs text-muted-foreground">{tooltip}</p>
      </div>
      <Switch
        checked={checked}
        disabled={disabled}
        onCheckedChange={onChange}
        aria-label={label}
      />
    </div>
  );
}
