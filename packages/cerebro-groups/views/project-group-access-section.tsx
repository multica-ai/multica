"use client";

// JEH-1009 PR 4 — project-detail Group access section. Lists the cerebro
// groups currently granted access to the project and lets admins add or
// remove groups. Sits inside the project's Access tab below the existing
// `ProjectAccessTab` controls (`workspace` vs `restricted`).
//
// Non-admins see the section read-only so they understand why they can or
// cannot see the project; admins get the add/remove controls.

import { useMemo, useState } from "react";
import { Trash2, UsersRound } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { groupListOptions, projectGroupsOptions } from "../queries";
import {
  useAddCerebroProjectGroup,
  useRemoveCerebroProjectGroup,
} from "../mutations";

type Props = { projectId: string };

export function ProjectGroupAccessSection({ projectId }: Props) {
  const enabled = useFeatureFlag("cerebro_groups_enabled");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: workspaceMembers = [] } = useQuery(memberListOptions(wsId));
  const currentMember = workspaceMembers.find((m) => m.user_id === user?.id);
  const isAdmin =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data: allGroups = [] } = useQuery(groupListOptions(wsId));
  const { data: granted = [], isPending } = useQuery(
    projectGroupsOptions(wsId, projectId),
  );

  if (!enabled) return null;

  const grantedIds = new Set(granted.map((g) => g.group_id));
  const grantedGroups = allGroups.filter((g) => grantedIds.has(g.id));

  return (
    <div className="space-y-3" data-testid="project-group-access-section">
      <div>
        <h4 className="text-sm font-semibold mb-1">Group access</h4>
        <p className="text-xs text-muted-foreground max-w-prose">
          Pick the workspace groups whose members can see this project, its
          issues, and everything inside it. Workspace owners and admins keep
          access regardless of group membership.
        </p>
      </div>

      <ul className="rounded-md border border-border divide-y">
        {isPending && (
          <li className="px-4 py-3 text-xs text-muted-foreground">Loading…</li>
        )}
        {!isPending && grantedGroups.length === 0 && (
          <li className="px-4 py-3 text-xs text-muted-foreground">
            No groups have been granted access yet.
          </li>
        )}
        {grantedGroups.map((g) => (
          <li
            key={g.id}
            className="flex items-center gap-3 px-4 py-2 text-sm"
            data-testid={`project-group-access-row-${g.id}`}
          >
            <UsersRound className="size-4 text-muted-foreground shrink-0" />
            <span className="flex-1 min-w-0">
              <span className="block truncate">{g.name}</span>
              {g.description && (
                <span className="block text-xs text-muted-foreground truncate">
                  {g.description}
                </span>
              )}
            </span>
            {isAdmin && (
              <RemoveGroupButton projectId={projectId} groupId={g.id} />
            )}
          </li>
        ))}
      </ul>

      {isAdmin && (
        <AddGroupPicker
          projectId={projectId}
          excludeIds={grantedIds}
          candidates={allGroups}
        />
      )}
    </div>
  );
}

function RemoveGroupButton({
  projectId,
  groupId,
}: {
  projectId: string;
  groupId: string;
}) {
  const remove = useRemoveCerebroProjectGroup(projectId);
  const handleClick = async () => {
    try {
      await remove.mutateAsync(groupId);
      toast.success("Group removed");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to remove group");
    }
  };
  return (
    <Button
      variant="ghost"
      size="icon-sm"
      onClick={handleClick}
      disabled={remove.isPending}
      aria-label="Remove group access"
    >
      <Trash2 className="size-4 text-muted-foreground" />
    </Button>
  );
}

function AddGroupPicker({
  projectId,
  excludeIds,
  candidates,
}: {
  projectId: string;
  excludeIds: Set<string>;
  candidates: Array<{ id: string; name: string; description: string | null }>;
}) {
  const [search, setSearch] = useState("");
  const [open, setOpen] = useState(false);
  const add = useAddCerebroProjectGroup(projectId);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return candidates
      .filter((g) => !excludeIds.has(g.id))
      .filter(
        (g) =>
          q === "" ||
          g.name.toLowerCase().includes(q) ||
          (g.description ?? "").toLowerCase().includes(q),
      );
  }, [candidates, excludeIds, search]);

  const handleAdd = async (groupId: string) => {
    try {
      await add.mutateAsync(groupId);
      toast.success("Group added");
      setSearch("");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to add group");
    }
  };

  if (!open) {
    return (
      <Button
        variant="outline"
        size="sm"
        onClick={() => setOpen(true)}
        data-testid="add-group-access-button"
      >
        Add group
      </Button>
    );
  }

  return (
    <div className="rounded-md border border-border bg-muted/30 p-3 space-y-3">
      <Input
        placeholder="Search groups…"
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        aria-label="Search groups"
      />
      <ul className="max-h-60 overflow-auto divide-y border border-border rounded-md">
        {filtered.length === 0 && (
          <li className="px-3 py-2 text-xs text-muted-foreground">
            {candidates.length === 0
              ? "No groups in this workspace yet — create one under Settings → Groups."
              : "No groups match — all available groups already have access."}
          </li>
        )}
        {filtered.map((g) => (
          <li
            key={g.id}
            className="flex items-center gap-3 px-3 py-2 text-sm"
          >
            <UsersRound className="size-4 text-muted-foreground shrink-0" />
            <span className="flex-1 min-w-0 truncate">{g.name}</span>
            <Button
              size="sm"
              variant="outline"
              onClick={() => handleAdd(g.id)}
              disabled={add.isPending}
            >
              Add
            </Button>
          </li>
        ))}
      </ul>
      <div className="flex justify-end">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            setOpen(false);
            setSearch("");
          }}
        >
          Done
        </Button>
      </div>
    </div>
  );
}
