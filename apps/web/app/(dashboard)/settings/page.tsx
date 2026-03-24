"use client";

import { useEffect, useState } from "react";
import { Settings, Users, Building2, Save, Crown, Shield, User, Plus, Trash2, LogOut } from "lucide-react";
import type { MemberWithUser, MemberRole } from "@multica/types";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { api } from "@/shared/api";

const roleConfig: Record<MemberRole, { label: string; icon: typeof Crown }> = {
  owner: { label: "Owner", icon: Crown },
  admin: { label: "Admin", icon: Shield },
  member: { label: "Member", icon: User },
};

function MemberRow({
  member,
  canManage,
  canManageOwners,
  isSelf,
  busy,
  onRoleChange,
  onRemove,
}: {
  member: MemberWithUser;
  canManage: boolean;
  canManageOwners: boolean;
  isSelf: boolean;
  busy: boolean;
  onRoleChange: (role: MemberRole) => void;
  onRemove: () => void;
}) {
  const rc = roleConfig[member.role];
  const RoleIcon = rc.icon;
  const canEditRole = canManage && (!isSelf || canManageOwners) && (member.role !== "owner" || canManageOwners);
  const canRemove = canManage && !isSelf && (member.role !== "owner" || canManageOwners);

  return (
    <div className="flex items-center gap-3 rounded-lg border px-4 py-3">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-semibold">
        {member.name
          .split(" ")
          .map((w) => w[0])
          .join("")
          .toUpperCase()
          .slice(0, 2)}
      </div>
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium">{member.name}</div>
        <div className="text-xs text-muted-foreground">{member.email}</div>
      </div>
      {canEditRole ? (
        <Select value={member.role} onValueChange={(value) => onRoleChange(value as MemberRole)} disabled={busy}>
          <SelectTrigger size="sm"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="member">Member</SelectItem>
            <SelectItem value="admin">Admin</SelectItem>
            {canManageOwners && <SelectItem value="owner">Owner</SelectItem>}
          </SelectContent>
        </Select>
      ) : (
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <RoleIcon className="h-3 w-3" />
          {rc.label}
        </div>
      )}
      {canRemove && (
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onRemove}
          disabled={busy}
          aria-label={`Remove ${member.name}`}
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      )}
    </div>
  );
}

export default function SettingsPage() {
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const members = useWorkspaceStore((s) => s.members);
  const updateWorkspace = useWorkspaceStore((s) => s.updateWorkspace);
  const refreshMembers = useWorkspaceStore((s) => s.refreshMembers);
  const leaveWorkspace = useWorkspaceStore((s) => s.leaveWorkspace);
  const deleteWorkspace = useWorkspaceStore((s) => s.deleteWorkspace);

  const [name, setName] = useState(workspace?.name ?? "");
  const [description, setDescription] = useState(
    workspace?.description ?? "",
  );
  const [context, setContext] = useState(workspace?.context ?? "");
  const [profileName, setProfileName] = useState(user?.name ?? "");
  const [avatarUrl, setAvatarUrl] = useState(user?.avatar_url ?? "");
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [profileSaving, setProfileSaving] = useState(false);
  const [profileSaved, setProfileSaved] = useState(false);
  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState<MemberRole>("member");
  const [inviteLoading, setInviteLoading] = useState(false);
  const [memberActionId, setMemberActionId] = useState<string | null>(null);
  const [workspaceError, setWorkspaceError] = useState("");
  const [profileError, setProfileError] = useState("");
  const [memberError, setMemberError] = useState("");
  const currentMember = members.find((member) => member.user_id === user?.id) ?? null;
  const canManageWorkspace = currentMember?.role === "owner" || currentMember?.role === "admin";
  const isOwner = currentMember?.role === "owner";

  useEffect(() => {
    setName(workspace?.name ?? "");
    setDescription(workspace?.description ?? "");
    setContext(workspace?.context ?? "");
  }, [workspace]);

  useEffect(() => {
    setProfileName(user?.name ?? "");
    setAvatarUrl(user?.avatar_url ?? "");
  }, [user]);

  const handleSave = async () => {
    if (!workspace) return;
    setSaving(true);
    setWorkspaceError("");
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        name,
        description,
        context,
      });
      updateWorkspace(updated);
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } catch (e) {
      setWorkspaceError(e instanceof Error ? e.message : "Failed to update workspace");
    } finally {
      setSaving(false);
    }
  };

  const handleProfileSave = async () => {
    setProfileSaving(true);
    setProfileError("");
    try {
      const updated = await api.updateMe({
        name: profileName,
        avatar_url: avatarUrl || undefined,
      });
      setUser(updated);
      setProfileSaved(true);
      setTimeout(() => setProfileSaved(false), 2000);
    } catch (e) {
      setProfileError(e instanceof Error ? e.message : "Failed to update profile");
    } finally {
      setProfileSaving(false);
    }
  };

  const handleAddMember = async () => {
    if (!workspace) return;
    setInviteLoading(true);
    setMemberError("");
    try {
      await api.createMember(workspace.id, {
        email: inviteEmail,
        role: inviteRole,
      });
      setInviteEmail("");
      setInviteRole("member");
      await refreshMembers();
    } catch (e) {
      setMemberError(e instanceof Error ? e.message : "Failed to add member");
    } finally {
      setInviteLoading(false);
    }
  };

  const handleRoleChange = async (memberId: string, role: MemberRole) => {
    if (!workspace) return;
    setMemberActionId(memberId);
    setMemberError("");
    try {
      await api.updateMember(workspace.id, memberId, { role });
      await refreshMembers();
    } catch (e) {
      setMemberError(e instanceof Error ? e.message : "Failed to update member");
    } finally {
      setMemberActionId(null);
    }
  };

  const handleRemoveMember = async (member: MemberWithUser) => {
    if (!workspace) return;
    if (!window.confirm(`Remove ${member.name} from ${workspace.name}?`)) return;

    setMemberActionId(member.id);
    setMemberError("");
    try {
      await api.deleteMember(workspace.id, member.id);
      await refreshMembers();
    } catch (e) {
      setMemberError(e instanceof Error ? e.message : "Failed to remove member");
    } finally {
      setMemberActionId(null);
    }
  };

  const handleLeaveWorkspace = async () => {
    if (!workspace) return;
    if (!window.confirm(`Leave ${workspace.name}?`)) return;

    setMemberActionId("leave");
    setMemberError("");
    try {
      await leaveWorkspace(workspace.id);
    } catch (e) {
      setMemberError(e instanceof Error ? e.message : "Failed to leave workspace");
    } finally {
      setMemberActionId(null);
    }
  };

  const handleDeleteWorkspace = async () => {
    if (!workspace) return;
    if (!window.confirm(`Delete ${workspace.name}? This cannot be undone.`)) return;

    setMemberActionId("delete-workspace");
    setMemberError("");
    try {
      await deleteWorkspace(workspace.id);
    } catch (e) {
      setMemberError(e instanceof Error ? e.message : "Failed to delete workspace");
    } finally {
      setMemberActionId(null);
    }
  };

  if (!workspace) return null;

  return (
    <div className="mx-auto max-w-2xl p-6 space-y-8">
      {/* Page header */}
      <div className="flex items-center gap-2">
        <Settings className="h-5 w-5 text-muted-foreground" />
        <h1 className="text-lg font-semibold">Settings</h1>
      </div>

      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <User className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">Profile</h2>
        </div>

        <div className="space-y-3 rounded-lg border p-4">
          <div>
            <Label className="text-xs text-muted-foreground">
              Name
            </Label>
            <Input
              type="search"
              value={profileName}
              onChange={(e) => setProfileName(e.target.value)}
              className="mt-1"
            />
          </div>
          <div>
            <Label className="text-xs text-muted-foreground">
              Avatar URL
            </Label>
            <Input
              type="url"
              value={avatarUrl}
              onChange={(e) => setAvatarUrl(e.target.value)}
              placeholder="https://example.com/avatar.png"
              className="mt-1"
            />
          </div>
          {profileError && (
            <p className="text-xs text-destructive">{profileError}</p>
          )}
          <div className="flex items-center justify-end gap-2 pt-1">
            {profileSaved && (
              <span className="text-xs text-success">Saved!</span>
            )}
            <Button
              size="sm"
              onClick={handleProfileSave}
              disabled={profileSaving || !profileName.trim()}
            >
              <Save className="h-3 w-3" />
              {profileSaving ? "Updating..." : "Update Profile"}
            </Button>
          </div>
        </div>
      </section>

      {/* Workspace info */}
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <Building2 className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">Workspace</h2>
        </div>

        <div className="space-y-3 rounded-lg border p-4">
          <div>
            <Label className="text-xs text-muted-foreground">
              Name
            </Label>
            <Input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              disabled={!canManageWorkspace}
              className="mt-1"
            />
          </div>
          <div>
            <Label className="text-xs text-muted-foreground">
              Description
            </Label>
            <Textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={3}
              disabled={!canManageWorkspace}
              className="mt-1 resize-none"
              placeholder="What does this workspace focus on?"
            />
          </div>
          <div>
            <Label className="text-xs text-muted-foreground">
              Context
            </Label>
            <textarea
              value={context}
              onChange={(e) => setContext(e.target.value)}
              rows={4}
              disabled={!canManageWorkspace}
              className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring resize-none"
              placeholder="Background information and context for AI agents working in this workspace"
            />
          </div>
          <div>
            <Label className="text-xs text-muted-foreground">
              Slug
            </Label>
            <div className="mt-1 rounded-md border bg-muted/50 px-3 py-2 text-sm text-muted-foreground">
              {workspace.slug}
            </div>
          </div>
          <div className="flex items-center justify-end gap-2 pt-1">
            {workspaceError && (
              <span className="text-xs text-destructive">{workspaceError}</span>
            )}
            {saved && (
              <span className="text-xs text-success">Saved!</span>
            )}
            <Button
              size="sm"
              onClick={handleSave}
              disabled={saving || !name.trim() || !canManageWorkspace}
            >
              <Save className="h-3 w-3" />
              {saving ? "Saving..." : "Save"}
            </Button>
          </div>
          {!canManageWorkspace && (
            <p className="text-xs text-muted-foreground">
              Only admins and owners can update workspace settings.
            </p>
          )}
        </div>
      </section>

      {/* Members */}
      <section className="space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Users className="h-4 w-4 text-muted-foreground" />
            <h2 className="text-sm font-semibold">
              Members ({members.length})
            </h2>
          </div>
        </div>

        {memberError && (
          <p className="text-sm text-destructive">{memberError}</p>
        )}

        {canManageWorkspace && (
          <div className="rounded-lg border p-4 space-y-3">
            <div className="flex items-center gap-2">
              <Plus className="h-4 w-4 text-muted-foreground" />
              <h3 className="text-sm font-medium">Add member</h3>
            </div>
            <div className="grid gap-3 sm:grid-cols-[1fr_120px_auto]">
              <Input
                type="email"
                value={inviteEmail}
                onChange={(e) => setInviteEmail(e.target.value)}
                placeholder="user@company.com"
              />
              <Select value={inviteRole} onValueChange={(value) => setInviteRole(value as MemberRole)}>
                <SelectTrigger size="sm"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="member">Member</SelectItem>
                  <SelectItem value="admin">Admin</SelectItem>
                  {isOwner && <SelectItem value="owner">Owner</SelectItem>}
                </SelectContent>
              </Select>
              <Button
                onClick={handleAddMember}
                disabled={inviteLoading || !inviteEmail.trim()}
              >
                {inviteLoading ? "Adding..." : "Add"}
              </Button>
            </div>
          </div>
        )}

        <div className="space-y-2">
          {members.map((m) => (
            <MemberRow
              key={m.id}
              member={m}
              canManage={canManageWorkspace}
              canManageOwners={isOwner}
              isSelf={m.user_id === user?.id}
              busy={memberActionId === m.id}
              onRoleChange={(role) => handleRoleChange(m.id, role)}
              onRemove={() => handleRemoveMember(m)}
            />
          ))}
          {members.length === 0 && (
            <p className="text-sm text-muted-foreground">No members found.</p>
          )}
        </div>
      </section>

      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <LogOut className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">Danger Zone</h2>
        </div>

        <div className="rounded-lg border p-4 space-y-3">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <p className="text-sm font-medium">Leave workspace</p>
              <p className="text-xs text-muted-foreground">
                Remove yourself from this workspace.
              </p>
            </div>
            <Button
              variant="outline"
              size="sm"
              onClick={handleLeaveWorkspace}
              disabled={memberActionId === "leave"}
            >
              {memberActionId === "leave" ? "Leaving..." : "Leave workspace"}
            </Button>
          </div>

          {isOwner && (
            <div className="flex flex-col gap-2 border-t pt-3 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <p className="text-sm font-medium text-destructive">Delete workspace</p>
                <p className="text-xs text-muted-foreground">
                  Permanently delete this workspace and its data.
                </p>
              </div>
              <Button
                variant="destructive"
                size="sm"
                onClick={handleDeleteWorkspace}
                disabled={memberActionId === "delete-workspace"}
              >
                {memberActionId === "delete-workspace" ? "Deleting..." : "Delete workspace"}
              </Button>
            </div>
          )}
        </div>
      </section>
    </div>
  );
}
