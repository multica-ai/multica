"use client";

import { useState, useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Pencil, Check, X, Search, ArrowLeft, Plus, Users, Shield, Crown, Mail } from "lucide-react";
import { useT } from "../i18n";
import {
  userListOptions,
  workspaceListOptions,
  invitationListOptions,
  useAdminCreateInvitations,
  useAdminAddUserToWorkspaces,
  useAdminRemoveUserFromWorkspace,
  useAdminUpdateUserRole,
  useAdminRevokeInvitation,
} from "@multica/core/admin";
import type { AdminUser, AdminUserWorkspace, MemberRole, AdminWorkspaceSummary, AdminPendingInvitation } from "@multica/core/types";
import { Input } from "@multica/ui/input";
import { Button } from "@multica/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { AppLink } from "../navigation";
import { useCurrentWorkspace, paths } from "@multica/core/paths";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { MoreHorizontal } from "lucide-react";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Label } from "@multica/ui/components/ui/label";

type TabId = "users" | "invitations";

const ROLE_CONFIG: Record<MemberRole, { label: string; icon: typeof Crown; color: string }> = {
  owner: { label: "Owner", icon: Crown, color: "text-amber-500" },
  admin: { label: "Admin", icon: Shield, color: "text-blue-500" },
  member: { label: "Member", icon: Users, color: "text-muted-foreground" },
};

function WorkspaceBadge({ workspace }: { workspace: AdminUserWorkspace }) {
  const rc = ROLE_CONFIG[workspace.role];
  const RoleIcon = rc.icon;
  return (
    <Badge variant="outline" className="gap-1 text-xs">
      <RoleIcon className={`h-3 w-3 ${rc.color}`} />
      <span className="max-w-24 truncate">{workspace.workspace_name}</span>
      <span className="text-muted-foreground">{workspace.role}</span>
    </Badge>
  );
}

function UserRow({
  user,
  onOpenEdit,
}: {
  user: AdminUser;
  onOpenEdit: (user: AdminUser) => void;
}) {
  const { t } = useT("admin");
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(user.name);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const handleSave = async () => {
    const trimmed = name.trim();
    if (!trimmed) {
      setError(t(($) => $.rename_empty_error));
      return;
    }
    setSaving(true);
    setError("");
    try {
      await fetch(`/api/admin/users/${user.id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: trimmed }),
      });
      setEditing(false);
      toast.success(t(($) => $.rename_success));
    } catch {
      setError(t(($) => $.rename_error));
    } finally {
      setSaving(false);
    }
  };

  const handleCancel = useCallback(() => {
    setName(user.name);
    setError("");
    setEditing(false);
  }, [user.name]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") handleSave();
    if (e.key === "Escape") handleCancel();
  };

  return (
    <div className="flex items-start gap-4 px-4 py-3">
      <div className="min-w-0 flex-1">
        {editing ? (
          <div>
            <Input
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={t(($) => $.rename_placeholder)}
              className="h-7 text-sm"
              aria-label={t(($) => $.rename_placeholder)}
            />
            {error && <p className="mt-1 text-xs text-destructive">{error}</p>}
          </div>
        ) : (
          <div className="text-sm font-medium truncate">{user.name}</div>
        )}
        <div className="text-xs text-muted-foreground truncate">{user.email}</div>
        {user.workspaces.length > 0 && (
          <div className="mt-1.5 flex flex-wrap gap-1">
            {user.workspaces.map((ws) => (
              <WorkspaceBadge key={ws.workspace_id} workspace={ws} />
            ))}
          </div>
        )}
      </div>
      <div className="flex items-center gap-1 shrink-0">
        {editing ? (
          <>
            <Button
              size="sm"
              variant="ghost"
              className="h-7 w-7 p-0"
              onClick={handleSave}
              disabled={saving}
              aria-label={t(($) => $.rename_save)}
            >
              <Check className="h-3.5 w-3.5" />
            </Button>
            <Button
              size="sm"
              variant="ghost"
              className="h-7 w-7 p-0"
              onClick={handleCancel}
              disabled={saving}
              aria-label={t(($) => $.rename_cancel)}
            >
              <X className="h-3.5 w-3.5" />
            </Button>
          </>
        ) : (
          <>
            <Button
              size="sm"
              variant="ghost"
              className="h-7 w-7 p-0"
              onClick={() => setEditing(true)}
              aria-label={t(($) => $.rename_button)}
            >
              <Pencil className="h-3.5 w-3.5" />
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button size="sm" variant="ghost" className="h-7 w-7 p-0">
                    <MoreHorizontal className="h-3.5 w-3.5" />
                  </Button>
                }
              />
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => onOpenEdit(user)}>
                  <Users className="mr-2 h-4 w-4" />
                  {t(($) => $.edit_workspaces)}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        )}
      </div>
    </div>
  );
}

function InvitationRow({
  invitation,
  onRevoke,
}: {
  invitation: AdminPendingInvitation;
  onRevoke: (invitation: AdminPendingInvitation) => void;
}) {
  const { t } = useT("admin");
  const rc = ROLE_CONFIG[invitation.role as MemberRole];
  const RoleIcon = rc?.icon ?? Users;

  const expiresAt = new Date(invitation.expires_at);
  const expiresIn = Math.ceil((expiresAt.getTime() - Date.now()) / (1000 * 60 * 60 * 24));
  const expiresText = expiresIn > 0
    ? `${expiresIn}d`
    : t(($) => $.invitations_expires);

  return (
    <div className="flex items-center gap-4 px-4 py-3">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted">
        <Mail className="h-4 w-4 text-muted-foreground" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium">{invitation.invitee_email}</span>
          {invitation.invitee_name && (
            <span className="text-xs text-muted-foreground">({invitation.invitee_name})</span>
          )}
        </div>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          <span>
            {t(($) => $.invitations_invited_by)} {invitation.inviter_name}
          </span>
          <Badge variant="outline" className="gap-1 text-xs">
            <RoleIcon className={`h-3 w-3 ${rc?.color ?? "text-muted-foreground"}`} />
            {invitation.workspace_name}
          </Badge>
          <span>{expiresText}</span>
        </div>
      </div>
      <Button
        size="sm"
        variant="ghost"
        className="h-7 w-7 p-0 text-destructive"
        onClick={() => onRevoke(invitation)}
        aria-label={t(($) => $.invitations_revoke_confirm_action)}
      >
        <X className="h-3.5 w-3.5" />
      </Button>
    </div>
  );
}

function InviteDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("admin");
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [role, setRole] = useState<MemberRole>("member");
  const [selectedWorkspaces, setSelectedWorkspaces] = useState<string[]>([]);

  const { data: workspaces = [] } = useQuery(workspaceListOptions());
  const createInvitations = useAdminCreateInvitations();

  const handleInvite = async () => {
    if (!email.trim() || selectedWorkspaces.length === 0) return;

    try {
      const result = await createInvitations.mutateAsync({
        email: email.trim(),
        name: name.trim() || undefined,
        role,
        workspaces: selectedWorkspaces,
      });

      if (result.user_exists) {
        toast.success(`${email} ${t(($) => $.invite_added_to_workspaces)}`);
      } else {
        toast.success(`${t(($) => $.invite_sent_to)} ${email}`);
      }
      setEmail("");
      setName("");
      setSelectedWorkspaces([]);
      onOpenChange(false);
    } catch {
      toast.error(t(($) => $.invite_error));
    }
  };

  const toggleWorkspace = (wsId: string) => {
    setSelectedWorkspaces((prev) =>
      prev.includes(wsId) ? prev.filter((id) => id !== wsId) : [...prev, wsId]
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.invite_title)}</DialogTitle>
          <DialogDescription>{t(($) => $.invite_description)}</DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="grid gap-2">
            <Label>{t(($) => $.invite_email)}</Label>
            <Input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="user@example.com"
            />
          </div>
          <div className="grid gap-2">
            <Label>{t(($) => $.invite_name_optional)}</Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t(($) => $.invite_name_placeholder)}
            />
          </div>
          <div className="grid gap-2">
            <Label>{t(($) => $.invite_role)}</Label>
            <Select value={role} onValueChange={(v) => setRole(v as MemberRole)}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="member">{t(($) => $.role_member)}</SelectItem>
                <SelectItem value="admin">{t(($) => $.role_admin)}</SelectItem>
                <SelectItem value="owner">{t(($) => $.role_owner)}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-2">
            <Label>{t(($) => $.invite_workspaces)}</Label>
            <div className="max-h-48 overflow-y-auto space-y-1 rounded-md border p-2">
              {workspaces.map((ws: AdminWorkspaceSummary) => (
                <div key={ws.id} className="flex items-center gap-2 py-1">
                  <Checkbox
                    id={`ws-${ws.id}`}
                    checked={selectedWorkspaces.includes(ws.id)}
                    onCheckedChange={() => toggleWorkspace(ws.id)}
                  />
                  <Label htmlFor={`ws-${ws.id}`} className="text-sm font-normal cursor-pointer">
                    {ws.name}
                  </Label>
                </div>
              ))}
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t(($) => $.cancel)}
          </Button>
          <Button
            onClick={handleInvite}
            disabled={!email.trim() || selectedWorkspaces.length === 0 || createInvitations.isPending}
          >
            {t(($) => $.invite_submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function EditWorkspacesDialog({
  user,
  open,
  onOpenChange,
}: {
  user: AdminUser | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("admin");
  const [selectedWorkspaces, setSelectedWorkspaces] = useState<string[]>([]);
  const [editingRole, setEditingRole] = useState<{ workspaceId: string; role: MemberRole } | null>(null);
  const [removingWorkspace, setRemovingWorkspace] = useState<{ workspaceId: string; workspaceName: string } | null>(null);

  const { data: workspaces = [] } = useQuery(workspaceListOptions());
  const addUserToWorkspaces = useAdminAddUserToWorkspaces();
  const removeUserFromWorkspace = useAdminRemoveUserFromWorkspace();
  const updateUserRole = useAdminUpdateUserRole();

  const userWorkspaceIds = user?.workspaces.map((w) => w.workspace_id) ?? [];

  const handleAdd = async () => {
    if (!user || selectedWorkspaces.length === 0) return;
    try {
      await addUserToWorkspaces.mutateAsync({
        userId: user.id,
        workspaceIds: selectedWorkspaces,
      });
      setSelectedWorkspaces([]);
      toast.success(t(($) => $.workspaces_added));
    } catch {
      toast.error(t(($) => $.workspaces_add_error));
    }
  };

  const handleRemove = async () => {
    if (!user || !removingWorkspace) return;
    try {
      await removeUserFromWorkspace.mutateAsync({
        userId: user.id,
        workspaceId: removingWorkspace.workspaceId,
      });
      setRemovingWorkspace(null);
      toast.success(t(($) => $.workspace_removed));
    } catch {
      toast.error(t(($) => $.workspace_remove_error));
    }
  };

  const handleRoleChange = async () => {
    if (!user || !editingRole) return;
    try {
      await updateUserRole.mutateAsync({
        userId: user.id,
        workspaceId: editingRole.workspaceId,
        role: editingRole.role,
      });
      setEditingRole(null);
      toast.success(t(($) => $.role_updated));
    } catch {
      toast.error(t(($) => $.role_update_error));
    }
  };

  const availableWorkspaces = workspaces.filter(
    (ws: AdminWorkspaceSummary) => !userWorkspaceIds.includes(ws.id)
  );

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{t(($) => $.edit_workspaces_title)}</DialogTitle>
            <DialogDescription>
              {user?.name} ({user?.email})
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <Label className="mb-2 block">{t(($) => $.current_workspaces)}</Label>
              {user?.workspaces.length === 0 ? (
                <p className="text-sm text-muted-foreground">{t(($) => $.no_workspaces)}</p>
              ) : (
                <div className="space-y-2">
                  {user?.workspaces.map((ws) => {
                    const rc = ROLE_CONFIG[ws.role];
                    const RoleIcon = rc.icon;
                    return (
                      <div key={ws.workspace_id} className="flex items-center justify-between gap-2 rounded-md border p-2">
                        <div className="flex items-center gap-2 min-w-0">
                          <RoleIcon className={`h-4 w-4 shrink-0 ${rc.color}`} />
                          <span className="text-sm truncate">{ws.workspace_name}</span>
                        </div>
                        <div className="flex items-center gap-1 shrink-0">
                          {editingRole?.workspaceId === ws.workspace_id ? (
                            <>
                              <Select
                                value={editingRole.role}
                                onValueChange={(v) =>
                                  setEditingRole({ ...editingRole, role: v as MemberRole })
                                }
                              >
                                <SelectTrigger className="h-7 w-24">
                                  <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                  <SelectItem value="member">{t(($) => $.role_member)}</SelectItem>
                                  <SelectItem value="admin">{t(($) => $.role_admin)}</SelectItem>
                                  <SelectItem value="owner">{t(($) => $.role_owner)}</SelectItem>
                                </SelectContent>
                              </Select>
                              <Button
                                size="sm"
                                variant="ghost"
                                className="h-7 w-7 p-0"
                                onClick={handleRoleChange}
                                disabled={updateUserRole.isPending}
                              >
                                <Check className="h-3.5 w-3.5" />
                              </Button>
                              <Button
                                size="sm"
                                variant="ghost"
                                className="h-7 w-7 p-0"
                                onClick={() => setEditingRole(null)}
                              >
                                <X className="h-3.5 w-3.5" />
                              </Button>
                            </>
                          ) : (
                            <>
                              <Badge variant="outline" className="text-xs">{ws.role}</Badge>
                              <Button
                                size="sm"
                                variant="ghost"
                                className="h-7 w-7 p-0"
                                onClick={() =>
                                  setEditingRole({ workspaceId: ws.workspace_id, role: ws.role })
                                }
                              >
                                <Pencil className="h-3.5 w-3.5" />
                              </Button>
                              <Button
                                size="sm"
                                variant="ghost"
                                className="h-7 w-7 p-0 text-destructive"
                                onClick={() =>
                                  setRemovingWorkspace({
                                    workspaceId: ws.workspace_id,
                                    workspaceName: ws.workspace_name,
                                  })
                                }
                              >
                                <X className="h-3.5 w-3.5" />
                              </Button>
                            </>
                          )}
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>

            {availableWorkspaces.length > 0 && (
              <div>
                <Label className="mb-2 block">{t(($) => $.add_to_workspaces)}</Label>
                <div className="max-h-40 overflow-y-auto space-y-1 rounded-md border p-2">
                  {availableWorkspaces.map((ws: AdminWorkspaceSummary) => (
                    <div key={ws.id} className="flex items-center gap-2 py-1">
                      <Checkbox
                        id={`add-ws-${ws.id}`}
                        checked={selectedWorkspaces.includes(ws.id)}
                        onCheckedChange={() => {
                          setSelectedWorkspaces((prev) =>
                            prev.includes(ws.id)
                              ? prev.filter((id) => id !== ws.id)
                              : [...prev, ws.id]
                          );
                        }}
                      />
                      <Label
                        htmlFor={`add-ws-${ws.id}`}
                        className="text-sm font-normal cursor-pointer"
                      >
                        {ws.name}
                      </Label>
                    </div>
                  ))}
                </div>
                {selectedWorkspaces.length > 0 && (
                  <Button
                    size="sm"
                    className="mt-2"
                    onClick={handleAdd}
                    disabled={addUserToWorkspaces.isPending}
                  >
                    <Plus className="mr-1 h-3.5 w-3.5" />
                    {t(($) => $.add_selected)}
                  </Button>
                )}
              </div>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              {t(($) => $.close)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={!!removingWorkspace} onOpenChange={() => setRemovingWorkspace(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.remove_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.remove_confirm_desc)}{" "}
              <strong>{removingWorkspace?.workspaceName}</strong>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={handleRemove} className="bg-destructive text-destructive-foreground">
              {t(($) => $.remove_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function RevokeConfirmDialog({
  invitation,
  open,
  onOpenChange,
  onConfirm,
}: {
  invitation: AdminPendingInvitation | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}) {
  const { t } = useT("admin");

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.invitations_revoke_confirm_title)}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.invitations_revoke_confirm_desc)}{" "}
            <strong>{invitation?.invitee_email}</strong>?
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t(($) => $.cancel)}</AlertDialogCancel>
          <AlertDialogAction onClick={onConfirm} className="bg-destructive text-destructive-foreground">
            {t(($) => $.invitations_revoke_confirm_action)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function InvitationsTab() {
  const { t } = useT("admin");
  const { data: invitations = [], isLoading } = useQuery(invitationListOptions());
  const revokeInvitation = useAdminRevokeInvitation();
  const [revoking, setRevoking] = useState<AdminPendingInvitation | null>(null);

  const handleRevoke = async () => {
    if (!revoking) return;
    try {
      await revokeInvitation.mutateAsync(revoking.id);
      toast.success(t(($) => $.invitations_revoke_success));
      setRevoking(null);
    } catch {
      toast.error(t(($) => $.invitations_revoke_error));
    }
  };

  if (isLoading) {
    return (
      <div className="space-y-2">
        {[1, 2, 3].map((i) => (
          <div key={i} className="h-14 rounded-xl bg-muted animate-pulse" />
        ))}
      </div>
    );
  }

  if (invitations.length === 0) {
    return (
      <p className="text-sm text-muted-foreground py-8 text-center">{t(($) => $.invitations_empty)}</p>
    );
  }

  return (
    <>
      <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
        {invitations.map((inv, i) => (
          <div key={inv.id} className={i > 0 ? "border-t border-border/50" : ""}>
            <InvitationRow invitation={inv} onRevoke={setRevoking} />
          </div>
        ))}
      </div>
      <RevokeConfirmDialog
        invitation={revoking}
        open={!!revoking}
        onOpenChange={() => setRevoking(null)}
        onConfirm={handleRevoke}
      />
    </>
  );
}

export function UserManagementPage() {
  const { t } = useT("admin");
  const workspace = useCurrentWorkspace();
  const [activeTab, setActiveTab] = useState<TabId>("users");
  const [search, setSearch] = useState("");
  const [inviteOpen, setInviteOpen] = useState(false);
  const [editingUser, setEditingUser] = useState<AdminUser | null>(null);
  const { data: users = [], isLoading: usersLoading } = useQuery(userListOptions({ search }));

  const backHref = workspace ? paths.workspace(workspace.slug).root() : "/";

  const tabs: { id: TabId; label: string }[] = [
    { id: "users", label: t(($) => $.tab_users) },
    { id: "invitations", label: t(($) => $.tab_invitations) },
  ];

  return (
    <div className="mx-auto max-w-3xl space-y-4 p-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <AppLink
            href={backHref}
            className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
          </AppLink>
          <h1 className="text-xl font-semibold">{t(($) => $.page_title)}</h1>
        </div>
        <Button size="sm" onClick={() => setInviteOpen(true)}>
          <Plus className="mr-1.5 h-4 w-4" />
          {t(($) => $.invite_button)}
        </Button>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 border-b border-border/50">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            onClick={() => setActiveTab(tab.id)}
            className={`px-3 py-2 text-sm font-medium border-b-2 transition-colors ${
              activeTab === tab.id
                ? "border-foreground text-foreground"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {activeTab === "users" && (
        <>
          <div className="relative">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              className="pl-9"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={t(($) => $.search_placeholder)}
              aria-label={t(($) => $.search_placeholder)}
            />
          </div>
          {usersLoading ? (
            <div className="space-y-2">
              {[1, 2, 3].map((i) => (
                <div key={i} className="h-14 rounded-xl bg-muted animate-pulse" />
              ))}
            </div>
          ) : users.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t(($) => $.empty)}</p>
          ) : (
            <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
              {users.map((user, i) => (
                <div key={user.id} className={i > 0 ? "border-t border-border/50" : ""}>
                  <UserRow user={user} onOpenEdit={setEditingUser} />
                </div>
              ))}
            </div>
          )}
        </>
      )}

      {activeTab === "invitations" && <InvitationsTab />}

      <InviteDialog open={inviteOpen} onOpenChange={setInviteOpen} />
      <EditWorkspacesDialog user={editingUser} open={!!editingUser} onOpenChange={(open) => !open && setEditingUser(null)} />
    </div>
  );
}
