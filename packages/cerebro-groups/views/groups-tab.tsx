"use client";

import { useMemo, useState } from "react";
import { Plus, Trash2, UserPlus, Users, Lock } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
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

// Two-letter initials for the UI-level ActorAvatar fallback when no avatar
// URL is set on the user. Skips empty parts ("  Anne") and limits to the
// first two pieces so long names ("Anne Sofie Larsen") stay readable.
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
import {
  useCreateCerebroGroup,
  useDeleteCerebroGroup,
  useUpdateCerebroGroup,
  useAddCerebroGroupMember,
  useRemoveCerebroGroupMember,
} from "../mutations";
import {
  groupListOptions,
  groupMembersOptions,
} from "../queries";
import type { CerebroGroup } from "../types";

/**
 * Settings → Workspace → Groups. JEH-1006 PR 1: CRUD for workspace groups +
 * member management. Permissions (capability toggles, runtime/agent
 * allowlists, project access) land in PR 2-3 and slot into the placeholder
 * footer below.
 *
 * Only workspace owners/admins can mutate groups; members see a read-only
 * roster. The Cerebro feature flag `cerebro_groups_enabled` gates the whole
 * tab — disabled flag returns null so the trigger in settings-page can stay
 * hidden too.
 */
export function GroupsTab() {
  const enabled = useFeatureFlag("cerebro_groups_enabled");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: groups = [], isPending } = useQuery(groupListOptions(wsId));

  const currentMember = members.find((m) => m.user_id === user?.id);
  const isAdmin =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  if (!enabled) return null;

  return (
    <div className="space-y-6 max-w-2xl" data-testid="groups-tab">
      <div>
        <h3 className="text-sm font-semibold mb-1">Groups</h3>
        <p className="text-xs text-muted-foreground max-w-prose">
          Named collections of workspace members. Use groups to control which
          runtimes, agents, and projects a team can access. Workspace owners
          and admins always have access to everything regardless of group
          membership.
        </p>
      </div>

      {isAdmin && <CreateGroupForm />}

      {isPending ? (
        <div className="text-xs text-muted-foreground">Loading groups…</div>
      ) : groups.length === 0 ? (
        <div className="rounded-md border border-dashed border-border p-6 text-center text-xs text-muted-foreground">
          No groups yet.{" "}
          {isAdmin
            ? "Create one above to start organising workspace access."
            : "An admin can create groups in this workspace."}
        </div>
      ) : (
        <ul className="space-y-4">
          {groups.map((g) => (
            <li key={g.id}>
              <GroupCard group={g} isAdmin={isAdmin} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function CreateGroupForm() {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const create = useCreateCerebroGroup();

  const trimmed = name.trim();
  const handleSubmit = async () => {
    if (!trimmed) return;
    try {
      await create.mutateAsync({
        name: trimmed,
        description: description.trim() || null,
      });
      setName("");
      setDescription("");
      toast.success(`Group "${trimmed}" created`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to create group");
    }
  };

  return (
    <div
      className="rounded-md border border-border bg-muted/30 p-4 space-y-3"
      data-testid="create-group-form"
    >
      <div className="text-sm font-medium">New group</div>
      <Input
        placeholder="Group name (e.g. Engineering)"
        value={name}
        onChange={(e) => setName(e.target.value)}
        aria-label="Group name"
      />
      <Textarea
        placeholder="Optional description"
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        aria-label="Group description"
        rows={2}
      />
      <div className="flex justify-end">
        <Button
          size="sm"
          onClick={handleSubmit}
          disabled={!trimmed || create.isPending}
        >
          <Plus className="size-4 mr-1.5" />
          {create.isPending ? "Creating…" : "Create group"}
        </Button>
      </div>
    </div>
  );
}

function GroupCard({ group, isAdmin }: { group: CerebroGroup; isAdmin: boolean }) {
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
        body: { name: trimmed, description: description.trim() || null },
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
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to delete group");
    }
  };

  return (
    <div className="rounded-md border border-border" data-testid="group-card">
      <div className="flex items-start justify-between gap-3 p-4 border-b border-border">
        <div className="min-w-0 flex-1">
          {editing ? (
            <div className="space-y-2">
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                aria-label={`Edit ${group.name} name`}
              />
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                aria-label={`Edit ${group.name} description`}
                placeholder="Optional description"
                rows={2}
              />
              <div className="flex gap-2 justify-end">
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
                <Button
                  size="sm"
                  onClick={handleSave}
                  disabled={update.isPending}
                >
                  {update.isPending ? "Saving…" : "Save"}
                </Button>
              </div>
            </div>
          ) : (
            <>
              <div className="flex items-center gap-2">
                <Users className="size-4 text-muted-foreground shrink-0" />
                <span className="text-sm font-medium truncate">{group.name}</span>
              </div>
              {group.description && (
                <p className="mt-1 text-xs text-muted-foreground">
                  {group.description}
                </p>
              )}
            </>
          )}
        </div>
        {isAdmin && !editing && (
          <div className="flex gap-1 shrink-0">
            <Button variant="ghost" size="sm" onClick={() => setEditing(true)}>
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

      <MembersSection groupId={group.id} isAdmin={isAdmin} />

      <PermissionsPlaceholder />

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

function MembersSection({ groupId, isAdmin }: { groupId: string; isAdmin: boolean }) {
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
    <div className="p-4 border-b border-border" data-testid="members-section">
      <div className="flex items-center justify-between mb-3">
        <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Members ({groupMembers.length})
        </div>
        {isAdmin && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowPicker((s) => !s)}
          >
            <UserPlus className="size-4 mr-1.5" />
            Add member
          </Button>
        )}
      </div>

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
    </div>
  );
}

function PermissionsPlaceholder() {
  // PR 2-3 (JEH-1008 + JEH-1009) replace this with capability toggles +
  // runtime / agent allowlists + project access controls. Kept inline so the
  // UI shape is visible from PR 1 onwards.
  return (
    <div
      className="p-4 text-xs text-muted-foreground flex items-start gap-2"
      data-testid="permissions-placeholder"
    >
      <Lock className="size-4 shrink-0 mt-0.5" />
      <span>
        Permissions for runtimes, agents, and projects land in the next
        update. Until then, group membership has no enforcement effect.
      </span>
    </div>
  );
}
