"use client";

import { useMemo, useState } from "react";
import { Crown, Shield, User, Plus, MoreHorizontal, UserMinus } from "lucide-react";
import { ActorAvatar } from "../../common/actor-avatar";
import type { MemberWithUser, MemberRole, KnownUser } from "@multica/core/types";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Badge } from "@multica/ui/components/ui/badge";
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
  addableUserListOptions,
  memberListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import { useT } from "../../i18n";
import { SettingsCard, SettingsSection, SettingsTab } from "./settings-layout";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";

const ROLE_ICONS: Record<MemberRole, typeof Crown> = {
  owner: Crown,
  admin: Shield,
  member: User,
};

function useRoleLabels() {
  const { t } = useT("settings");
  return {
    owner: {
      label: t(($) => $.members.roles.owner.label),
      description: t(($) => $.members.roles.owner.description),
      icon: ROLE_ICONS.owner,
    },
    admin: {
      label: t(($) => $.members.roles.admin.label),
      description: t(($) => $.members.roles.admin.description),
      icon: ROLE_ICONS.admin,
    },
    member: {
      label: t(($) => $.members.roles.member.label),
      description: t(($) => $.members.roles.member.description),
      icon: ROLE_ICONS.member,
    },
  } as const;
}

function MemberRow({
  member,
  canManage,
  canManageOwners,
  ownerCount,
  isSelf,
  busy,
  onRoleChange,
  onRemove,
}: {
  member: MemberWithUser;
  canManage: boolean;
  canManageOwners: boolean;
  ownerCount: number;
  isSelf: boolean;
  busy: boolean;
  onRoleChange: (role: MemberRole) => void;
  onRemove: () => void;
}) {
  const { t } = useT("settings");
  const roleConfig = useRoleLabels();
  const rc = roleConfig[member.role];
  const RoleIcon = rc.icon;
  const canEditRole = canManage && !isSelf && (member.role !== "owner" || canManageOwners);
  const canRemove = canManage && !isSelf && (member.role !== "owner" || canManageOwners);
  const isLastOwner = member.role === "owner" && ownerCount <= 1;
  const showMenu = canEditRole || canRemove;

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <ActorAvatar actorType="member" actorId={member.user_id} size="lg" />
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium truncate">{member.name}</div>
        <div className="text-xs text-muted-foreground truncate">{member.email}</div>
      </div>
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
                  {t(($) => $.members.change_role)}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent className="w-auto">
                  {(Object.entries(roleConfig) as [MemberRole, (typeof roleConfig)[MemberRole]][]).map(
                    ([role, config]) => {
                      if (role === "owner" && !canManageOwners) return null;
                      const Icon = config.icon;
                      const wouldDemoteLastOwner =
                        isLastOwner && role !== "owner";
                      return (
                        <DropdownMenuItem
                          key={role}
                          onClick={() =>
                            wouldDemoteLastOwner ? undefined : onRoleChange(role)
                          }
                          disabled={wouldDemoteLastOwner}
                          title={
                            wouldDemoteLastOwner
                              ? t(($) => $.members.cannot_demote_last_owner_title)
                              : undefined
                          }
                        >
                          <Icon className="h-3.5 w-3.5" />
                          <div className="flex flex-col">
                            <span>{config.label}</span>
                            <span className="text-xs text-muted-foreground font-normal">
                              {wouldDemoteLastOwner
                                ? t(($) => $.members.cannot_demote_last_owner)
                                : config.description}
                            </span>
                          </div>
                          {member.role === role && (
                            <span className="ml-auto text-xs text-muted-foreground">{"✓"}</span>
                          )}
                        </DropdownMenuItem>
                      );
                    }
                  )}
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            )}
            {canEditRole && canRemove && <DropdownMenuSeparator />}
            {canRemove && (
              <DropdownMenuItem variant="destructive" onClick={onRemove}>
                <UserMinus className="h-3.5 w-3.5" />
                {t(($) => $.members.remove_action)}
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
  );
}

function AddableUserRow({
  user,
  busy,
  onAdd,
}: {
  user: KnownUser;
  busy: boolean;
  onAdd: () => void;
}) {
  return (
    <div className="flex items-center gap-3 px-4 py-2.5">
      <ActorAvatar actorType="member" actorId={user.id} size="sm" />
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium truncate">{user.name}</div>
        <div className="text-xs text-muted-foreground truncate">{user.email}</div>
      </div>
      <Button size="sm" variant="outline" disabled={busy} onClick={onAdd}>
        Add
      </Button>
    </div>
  );
}

export function MembersTab() {
  const { t } = useT("settings");
  const roleConfig = useRoleLabels();
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: addableUsers = [] } = useQuery(addableUserListOptions(wsId));

  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState<MemberRole>("member");
  const [memberFilter, setMemberFilter] = useState("");
  const [inviteLoading, setInviteLoading] = useState(false);
  const [addingUserId, setAddingUserId] = useState<string | null>(null);
  const [memberActionId, setMemberActionId] = useState<string | null>(null);
  const [confirmAction, setConfirmAction] = useState<{
    title: string;
    description: string;
    variant?: "destructive";
    onConfirm: () => Promise<void>;
  } | null>(null);

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManageWorkspace = currentMember?.role === "owner" || currentMember?.role === "admin";
  const isOwner = currentMember?.role === "owner";
  const ownerCount = members.filter((m) => m.role === "owner").length;

  const filteredAddableUsers = useMemo(() => {
    const q = memberFilter.trim().toLowerCase();
    if (!q) return addableUsers;
    return addableUsers.filter(
      (u) =>
        u.name.toLowerCase().includes(q) ||
        u.email.toLowerCase().includes(q) ||
        matchesPinyin(u.name, q),
    );
  }, [addableUsers, memberFilter]);

  const invalidateMemberQueries = () => {
    qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
    qc.invalidateQueries({ queryKey: workspaceKeys.addableUsers(wsId) });
    qc.invalidateQueries({ queryKey: workspaceKeys.list() });
  };

  const handleAddMember = async (data: { user_id?: string; email?: string }) => {
    if (!workspace) return;
    setInviteLoading(true);
    try {
      await api.createMember(workspace.id, {
        ...data,
        role: inviteRole,
      });
      setInviteEmail("");
      invalidateMemberQueries();
      toast.success(t(($) => $.members.toast_member_added));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_member_add_failed));
    } finally {
      setInviteLoading(false);
    }
  };

  const handleAddKnownUser = async (knownUser: KnownUser) => {
    setAddingUserId(knownUser.id);
    try {
      await api.createMember(workspace!.id, {
        user_id: knownUser.id,
        role: inviteRole,
      });
      invalidateMemberQueries();
      toast.success(t(($) => $.members.toast_member_added));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_member_add_failed));
    } finally {
      setAddingUserId(null);
    }
  };

  const handleInviteByEmail = async () => {
    if (!inviteEmail.trim()) return;
    await handleAddMember({ email: inviteEmail.trim() });
  };

  const handleRoleChange = async (memberId: string, role: MemberRole) => {
    if (!workspace) return;
    setMemberActionId(memberId);
    try {
      await api.updateMember(workspace.id, memberId, { role });
      qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
      toast.success(t(($) => $.members.toast_role_updated));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_role_failed));
    } finally {
      setMemberActionId(null);
    }
  };

  const handleRemoveMember = (member: MemberWithUser) => {
    if (!workspace) return;
    setConfirmAction({
      title: t(($) => $.members.remove_member_title, { name: member.name }),
      description: t(($) => $.members.remove_member_description, { name: member.name, workspace: workspace.name }),
      variant: "destructive",
      onConfirm: async () => {
        setMemberActionId(member.id);
        try {
          await api.deleteMember(workspace.id, member.id);
          invalidateMemberQueries();
          toast.success(t(($) => $.members.toast_member_removed));
        } catch (e) {
          toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_member_remove_failed));
        } finally {
          setMemberActionId(null);
        }
      },
    });
  };

  if (!workspace) return null;

  return (
    <SettingsTab title={t(($) => $.page.tabs.members)}>
      <SettingsSection title={t(($) => $.members.section_title, { count: members.length })}>

        {canManageWorkspace && (
          <Card>
            <CardContent className="space-y-4">
              <div className="flex items-center gap-2">
                <Plus className="h-4 w-4 text-muted-foreground" />
                <h3 className="text-sm font-medium">{t(($) => $.members.add_title)}</h3>
              </div>

              {addableUsers.length > 0 && (
                <div className="space-y-2">
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.members.add_known_hint)}
                  </p>
                  <Input
                    type="search"
                    value={memberFilter}
                    onChange={(e) => setMemberFilter(e.target.value)}
                    placeholder={t(($) => $.members.add_search_placeholder)}
                  />
                  <SettingsCard>
                    {filteredAddableUsers.length === 0 ? (
                      <div className="px-4 py-3 text-sm text-muted-foreground">
                        {t(($) => $.members.add_empty)}
                      </div>
                    ) : (
                      filteredAddableUsers.map((u) => (
                        <AddableUserRow
                          key={u.id}
                          user={u}
                          busy={addingUserId === u.id || inviteLoading}
                          onAdd={() => void handleAddKnownUser(u)}
                        />
                      ))
                    )}
                  </SettingsCard>
                </div>
              )}

              <div className="space-y-2">
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.members.add_email_hint)}
                </p>
                <div className="grid gap-3 sm:grid-cols-[1fr_120px_auto]">
                  <Input
                    type="email"
                    name="invite-email"
                    autoComplete="email"
                    spellCheck={false}
                    aria-label={t(($) => $.members.invite_email_placeholder)}
                    value={inviteEmail}
                    onChange={(e) => setInviteEmail(e.target.value)}
                    placeholder={t(($) => $.members.invite_email_placeholder)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" && inviteEmail.trim()) void handleInviteByEmail();
                    }}
                  />
                  <Select value={inviteRole} onValueChange={(value) => setInviteRole(value as MemberRole)}>
                    <SelectTrigger size="sm">
                      <SelectValue>{() => roleConfig[inviteRole].label}</SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="member">{roleConfig.member.label}</SelectItem>
                      <SelectItem value="admin">{roleConfig.admin.label}</SelectItem>
                    </SelectContent>
                  </Select>
                  <Button
                    onClick={() => void handleInviteByEmail()}
                    disabled={inviteLoading || !inviteEmail.trim()}
                  >
                    {inviteLoading ? t(($) => $.members.adding) : t(($) => $.members.add_button)}
                  </Button>
                </div>
              </div>
            </CardContent>
          </Card>
        )}

        {members.length > 0 ? (
          <SettingsCard>
            {members.map((m) => (
              <div key={m.id}>
                <MemberRow
                  member={m}
                  canManage={canManageWorkspace}
                  canManageOwners={isOwner}
                  ownerCount={ownerCount}
                  isSelf={m.user_id === user?.id}
                  busy={memberActionId === m.id}
                  onRoleChange={(role) => handleRoleChange(m.id, role)}
                  onRemove={() => handleRemoveMember(m)}
                />
              </div>
            ))}
          </SettingsCard>
        ) : (
          <p className="text-sm text-muted-foreground">{t(($) => $.members.no_members)}</p>
        )}
      </SettingsSection>

      <AlertDialog open={!!confirmAction} onOpenChange={(v) => { if (!v) setConfirmAction(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirmAction?.title}</AlertDialogTitle>
            <AlertDialogDescription>{confirmAction?.description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.members.confirm_cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant={confirmAction?.variant === "destructive" ? "destructive" : "default"}
              onClick={async () => {
                await confirmAction?.onConfirm();
                setConfirmAction(null);
              }}
            >
              {t(($) => $.members.confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsTab>
  );
}