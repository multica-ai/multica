"use client";

import { useState } from "react";
import {
  Crown,
  Shield,
  User as UserIcon,
  Plus,
  MoreHorizontal,
  UserMinus,
  Users as UsersIcon,
  Clock,
  X,
  Mail,
  ShieldOff,
  Wallet,
} from "lucide-react";
import { ActorAvatar } from "../common/actor-avatar";
import type { MemberWithUser, MemberRole, Invitation } from "@multica/core/types";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Badge } from "@multica/ui/components/ui/badge";
import { Switch } from "@multica/ui/components/ui/switch";
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
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
} from "@multica/ui/components/ui/dropdown-menu";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import {
  memberListOptions,
  memberUsageOptions,
  invitationListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";

const roleConfig: Record<MemberRole, { label: string; icon: typeof Crown; description: string }> = {
  owner: { label: "Owner", icon: Crown, description: "Full access, manage all settings" },
  admin: { label: "Admin", icon: Shield, description: "Manage members and settings" },
  member: { label: "Member", icon: UserIcon, description: "Create and work on issues" },
};

function formatCents(cents: number): string {
  if (!Number.isFinite(cents) || cents <= 0) return "$0.00";
  return `$${(cents / 100).toFixed(2)}`;
}

function MemberUsageCell({ workspaceId, memberId }: { workspaceId: string; memberId: string }) {
  const { data, isLoading } = useQuery(memberUsageOptions(workspaceId, memberId));
  if (isLoading) {
    return <span className="text-xs text-muted-foreground">Loading…</span>;
  }
  if (!data) {
    return <span className="text-xs text-muted-foreground">—</span>;
  }
  return (
    <div className="flex flex-col text-right text-xs">
      <span className="font-medium tabular-nums">{formatCents(data.daily_cents)} today</span>
      <span className="text-muted-foreground tabular-nums">
        {formatCents(data.monthly_cents)} this month
      </span>
    </div>
  );
}

function MemberRow({
  member,
  workspaceId,
  canManage,
  canManageOwners,
  isSelf,
  busy,
  onRoleChange,
  onRemove,
  onToggleScope,
  onToggleBudget,
}: {
  member: MemberWithUser;
  workspaceId: string;
  canManage: boolean;
  canManageOwners: boolean;
  isSelf: boolean;
  busy: boolean;
  onRoleChange: (role: MemberRole) => void;
  onRemove: () => void;
  onToggleScope: (enabled: boolean) => void;
  onToggleBudget: (enabled: boolean) => void;
}) {
  const rc = roleConfig[member.role];
  const RoleIcon = rc.icon;
  const canEditRole = canManage && !isSelf && (member.role !== "owner" || canManageOwners);
  const canRemove = canManage && !isSelf && (member.role !== "owner" || canManageOwners);
  const showMenu = canEditRole || canRemove;

  return (
    <div className="grid grid-cols-[auto_minmax(0,1fr)_auto_auto_auto_auto] items-center gap-3 px-4 py-3">
      <ActorAvatar actorType="member" actorId={member.user_id} size={32} />
      <div className="min-w-0">
        <div className="text-sm font-medium truncate">{member.name}</div>
        <div className="text-xs text-muted-foreground truncate">{member.email}</div>
      </div>

      <div className="flex flex-col items-center gap-1" title="Scope-spærring (JEH-324). Sluk for at give brugerens tasks 1-times bredt token i stedet for stramt task-scope-token.">
        <div className="flex items-center gap-1.5 text-[10px] uppercase tracking-wide text-muted-foreground">
          <ShieldOff className="h-3 w-3" />
          Scope
        </div>
        <Switch
          checked={member.scope_enforcement_enabled}
          disabled={!canManage || busy}
          onCheckedChange={onToggleScope}
        />
      </div>

      <div className="flex flex-col items-center gap-1" title="Budget-loft (JEH-327). Sluk for at undtage brugerens tasks fra dagligt/månedligt forbrugsloft.">
        <div className="flex items-center gap-1.5 text-[10px] uppercase tracking-wide text-muted-foreground">
          <Wallet className="h-3 w-3" />
          Budget
        </div>
        <Switch
          checked={member.budget_enforcement_enabled}
          disabled={!canManage || busy}
          onCheckedChange={onToggleBudget}
        />
      </div>

      <MemberUsageCell workspaceId={workspaceId} memberId={member.id} />

      <div className="flex items-center gap-2">
        {showMenu && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="icon-sm" disabled={busy}>
                  <MoreHorizontal className="h-4 w-4 text-muted-foreground" />
                </Button>
              }
            />
            <DropdownMenuContent align="end" className="w-auto">
              {canEditRole && (
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>
                    <Shield className="h-3.5 w-3.5" />
                    Change role
                  </DropdownMenuSubTrigger>
                  <DropdownMenuSubContent className="w-auto">
                    {(Object.entries(roleConfig) as [MemberRole, (typeof roleConfig)[MemberRole]][]).map(
                      ([role, config]) => {
                        if (role === "owner" && !canManageOwners) return null;
                        const Icon = config.icon;
                        return (
                          <DropdownMenuItem key={role} onClick={() => onRoleChange(role)}>
                            <Icon className="h-3.5 w-3.5" />
                            <div className="flex flex-col">
                              <span>{config.label}</span>
                              <span className="text-xs text-muted-foreground font-normal">
                                {config.description}
                              </span>
                            </div>
                            {member.role === role && (
                              <span className="ml-auto text-xs text-muted-foreground">&#10003;</span>
                            )}
                          </DropdownMenuItem>
                        );
                      },
                    )}
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
              )}
              {canEditRole && canRemove && <DropdownMenuSeparator />}
              {canRemove && (
                <DropdownMenuItem variant="destructive" onClick={onRemove}>
                  <UserMinus className="h-3.5 w-3.5" />
                  Remove from workspace
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
        <Badge variant="secondary">
          <RoleIcon className="h-3 w-3" />
          {rc.label}
        </Badge>
      </div>
    </div>
  );
}

function InvitationRow({
  invitation,
  canManage,
  onRevoke,
  busy,
}: {
  invitation: Invitation;
  canManage: boolean;
  onRevoke: () => void;
  busy: boolean;
}) {
  const rc = roleConfig[invitation.role];
  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <div className="flex h-8 w-8 items-center justify-center rounded-full bg-muted">
        <Mail className="h-4 w-4 text-muted-foreground" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium truncate">{invitation.invitee_email}</div>
        <div className="flex items-center gap-1 text-xs text-muted-foreground">
          <Clock className="h-3 w-3" />
          <span>Pending</span>
        </div>
      </div>
      {canManage && (
        <Button
          variant="ghost"
          size="icon-sm"
          disabled={busy}
          onClick={onRevoke}
          title="Revoke invitation"
        >
          <X className="h-4 w-4 text-muted-foreground" />
        </Button>
      )}
      <Badge variant="outline">{rc.label}</Badge>
    </div>
  );
}

export function UsersPage() {
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: invitations = [] } = useQuery(invitationListOptions(wsId));

  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState<MemberRole>("member");
  const [inviteLoading, setInviteLoading] = useState(false);
  const [memberActionId, setMemberActionId] = useState<string | null>(null);
  const [invitationActionId, setInvitationActionId] = useState<string | null>(null);
  const [confirmAction, setConfirmAction] = useState<{
    title: string;
    description: string;
    variant?: "destructive";
    onConfirm: () => Promise<void>;
  } | null>(null);

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";
  const isOwner = currentMember?.role === "owner";

  const handleInviteMember = async () => {
    if (!workspace) return;
    setInviteLoading(true);
    try {
      await api.createMember(workspace.id, { email: inviteEmail, role: inviteRole });
      setInviteEmail("");
      setInviteRole("member");
      qc.invalidateQueries({ queryKey: workspaceKeys.invitations(wsId) });
      toast.success("Invitation sent");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to send invitation");
    } finally {
      setInviteLoading(false);
    }
  };

  const handleRevokeInvitation = (invitation: Invitation) => {
    if (!workspace) return;
    setConfirmAction({
      title: "Revoke invitation",
      description: `Revoke the invitation to ${invitation.invitee_email}?`,
      variant: "destructive",
      onConfirm: async () => {
        setInvitationActionId(invitation.id);
        try {
          await api.revokeInvitation(workspace.id, invitation.id);
          qc.invalidateQueries({ queryKey: workspaceKeys.invitations(wsId) });
          toast.success("Invitation revoked");
        } catch (e) {
          toast.error(e instanceof Error ? e.message : "Failed to revoke invitation");
        } finally {
          setInvitationActionId(null);
        }
      },
    });
  };

  const handleRoleChange = async (memberId: string, role: MemberRole) => {
    if (!workspace) return;
    setMemberActionId(memberId);
    try {
      await api.updateMember(workspace.id, memberId, { role });
      qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
      toast.success("Role updated");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update member");
    } finally {
      setMemberActionId(null);
    }
  };

  const handleRemoveMember = (member: MemberWithUser) => {
    if (!workspace) return;
    setConfirmAction({
      title: `Remove ${member.name}`,
      description: `Remove ${member.name} from ${workspace.name}? They will lose access to this workspace.`,
      variant: "destructive",
      onConfirm: async () => {
        setMemberActionId(member.id);
        try {
          await api.deleteMember(workspace.id, member.id);
          qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
          toast.success("Member removed");
        } catch (e) {
          toast.error(e instanceof Error ? e.message : "Failed to remove member");
        } finally {
          setMemberActionId(null);
        }
      },
    });
  };

  const handleToggleScope = (member: MemberWithUser, enabled: boolean) => {
    if (!workspace) return;
    const apply = async () => {
      setMemberActionId(member.id);
      try {
        await api.setMemberScopeEnforcement(workspace.id, member.id, enabled);
        qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
        toast.success(
          enabled
            ? "Scope enforcement enabled"
            : "Scope enforcement disabled — wider 1h token granted",
        );
      } catch (e) {
        toast.error(e instanceof Error ? e.message : "Failed to update toggle");
      } finally {
        setMemberActionId(null);
      }
    };
    if (!enabled) {
      setConfirmAction({
        title: `Disable scope enforcement for ${member.name}?`,
        description:
          "Their agent tasks will receive a 1-hour wider-scope token instead of a strict per-task token. Use only for trusted members.",
        variant: "destructive",
        onConfirm: apply,
      });
      return;
    }
    void apply();
  };

  const handleToggleBudget = (member: MemberWithUser, enabled: boolean) => {
    if (!workspace) return;
    setMemberActionId(member.id);
    api
      .setMemberBudgetEnforcement(workspace.id, member.id, enabled)
      .then(() => {
        qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
        toast.success(
          enabled ? "Budget cap re-enabled" : "Budget cap bypassed for this member",
        );
      })
      .catch((e: unknown) => {
        toast.error(e instanceof Error ? e.message : "Failed to update toggle");
      })
      .finally(() => setMemberActionId(null));
  };

  if (!workspace) return null;

  return (
    <div className="mx-auto max-w-6xl space-y-8 p-6">
      <header className="space-y-2">
        <div className="flex items-center gap-2">
          <UsersIcon className="h-5 w-5 text-muted-foreground" />
          <h1 className="text-xl font-semibold">Users</h1>
        </div>
        <p className="text-sm text-muted-foreground">
          Manage members, roles, per-user enforcement toggles and current spend.
        </p>
      </header>

      {canManage && (
        <Card>
          <CardContent className="space-y-3">
            <div className="flex items-center gap-2">
              <Plus className="h-4 w-4 text-muted-foreground" />
              <h2 className="text-sm font-medium">Invite member</h2>
            </div>
            <div className="grid gap-3 sm:grid-cols-[1fr_120px_auto]">
              <Input
                type="email"
                value={inviteEmail}
                onChange={(e) => setInviteEmail(e.target.value)}
                placeholder="user@company.com"
                onKeyDown={(e) => {
                  if (e.key === "Enter" && inviteEmail.trim()) handleInviteMember();
                }}
              />
              <Select value={inviteRole} onValueChange={(value) => setInviteRole(value as MemberRole)}>
                <SelectTrigger size="sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="member">Member</SelectItem>
                  <SelectItem value="admin">Admin</SelectItem>
                </SelectContent>
              </Select>
              <Button onClick={handleInviteMember} disabled={inviteLoading || !inviteEmail.trim()}>
                {inviteLoading ? "Inviting..." : "Invite"}
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <UsersIcon className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">Members ({members.length})</h2>
        </div>
        {members.length > 0 ? (
          <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
            {members.map((m, i) => (
              <div key={m.id} className={i > 0 ? "border-t border-border/50" : ""}>
                <MemberRow
                  member={m}
                  workspaceId={workspace.id}
                  canManage={canManage}
                  canManageOwners={isOwner ?? false}
                  isSelf={m.user_id === user?.id}
                  busy={memberActionId === m.id}
                  onRoleChange={(role) => handleRoleChange(m.id, role)}
                  onRemove={() => handleRemoveMember(m)}
                  onToggleScope={(enabled) => handleToggleScope(m, enabled)}
                  onToggleBudget={(enabled) => handleToggleBudget(m, enabled)}
                />
              </div>
            ))}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">No members found.</p>
        )}
      </section>

      {invitations.length > 0 && (
        <section className="space-y-4">
          <div className="flex items-center gap-2">
            <Clock className="h-4 w-4 text-muted-foreground" />
            <h2 className="text-sm font-semibold">Pending invitations ({invitations.length})</h2>
          </div>
          <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
            {invitations.map((inv, i) => (
              <div key={inv.id} className={i > 0 ? "border-t border-border/50" : ""}>
                <InvitationRow
                  invitation={inv}
                  canManage={canManage}
                  onRevoke={() => handleRevokeInvitation(inv)}
                  busy={invitationActionId === inv.id}
                />
              </div>
            ))}
          </div>
        </section>
      )}

      <AlertDialog
        open={!!confirmAction}
        onOpenChange={(v) => {
          if (!v) setConfirmAction(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirmAction?.title}</AlertDialogTitle>
            <AlertDialogDescription>{confirmAction?.description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant={confirmAction?.variant === "destructive" ? "destructive" : "default"}
              onClick={async () => {
                await confirmAction?.onConfirm();
                setConfirmAction(null);
              }}
            >
              Confirm
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
