"use client";

import { useState } from "react";
import { Crown, Shield, User, Plus, MoreHorizontal, UserMinus, Users } from "lucide-react";
import { ActorAvatar } from "@multica/views/common/actor-avatar";
import type { MemberWithUser, MemberRole } from "@multica/core/types";
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
import { useAuthStore } from "@/platform/auth";
import { useWorkspaceStore } from "@/platform/workspace";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { api } from "@/platform/api";
import { useTranslations } from "next-intl";

type RoleConfig = { label: string; icon: typeof Crown; description: string };

function MemberRow({
  member,
  roleConfig,
  canManage,
  canManageOwners,
  isSelf,
  busy,
  onRoleChange,
  onRemove,
  changeRoleLabel,
  removeLabel,
}: {
  member: MemberWithUser;
  roleConfig: Record<MemberRole, RoleConfig>;
  canManage: boolean;
  canManageOwners: boolean;
  isSelf: boolean;
  busy: boolean;
  onRoleChange: (role: MemberRole) => void;
  onRemove: () => void;
  changeRoleLabel: string;
  removeLabel: string;
}) {
  const rc = roleConfig[member.role];
  const RoleIcon = rc.icon;
  const canEditRole = canManage && !isSelf && (member.role !== "owner" || canManageOwners);
  const canRemove = canManage && !isSelf && (member.role !== "owner" || canManageOwners);
  const showMenu = canEditRole || canRemove;

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <ActorAvatar actorType="member" actorId={member.user_id} size={32} />
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
                  {changeRoleLabel}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent className="w-auto">
                  {(Object.entries(roleConfig) as [MemberRole, RoleConfig][]).map(
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
                    }
                  )}
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            )}
            {canEditRole && canRemove && <DropdownMenuSeparator />}
            {canRemove && (
              <DropdownMenuItem variant="destructive" onClick={onRemove}>
                <UserMinus className="h-3.5 w-3.5" />
                {removeLabel}
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

export function MembersTab() {
  const user = useAuthStore((s) => s.user);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const t = useTranslations("members");

  const roleConfig: Record<MemberRole, RoleConfig> = {
    owner: { label: t("roles.owner"), icon: Crown, description: t("roleDescriptions.owner") },
    admin: { label: t("roles.admin"), icon: Shield, description: t("roleDescriptions.admin") },
    member: { label: t("roles.member"), icon: User, description: t("roleDescriptions.member") },
  };

  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState<MemberRole>("member");
  const [inviteLoading, setInviteLoading] = useState(false);
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

  const handleAddMember = async () => {
    if (!workspace) return;
    setInviteLoading(true);
    try {
      await api.createMember(workspace.id, { email: inviteEmail, role: inviteRole });
      setInviteEmail("");
      setInviteRole("member");
      qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("failedInvite"));
    } finally {
      setInviteLoading(false);
    }
  };

  const handleRoleChange = async (memberId: string, role: MemberRole) => {
    if (!workspace) return;
    setMemberActionId(memberId);
    try {
      await api.updateMember(workspace.id, memberId, { role });
      qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("failedUpdateRole"));
    } finally {
      setMemberActionId(null);
    }
  };

  const handleRemoveMember = (member: MemberWithUser) => {
    if (!workspace) return;
    setConfirmAction({
      title: t("removeMember"),
      description: t("removeDescription"),
      variant: "destructive",
      onConfirm: async () => {
        setMemberActionId(member.id);
        try {
          await api.deleteMember(workspace.id, member.id);
          qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
          toast.success(t("memberRemoved"));
        } catch (e) {
          toast.error(e instanceof Error ? e.message : t("failedRemove"));
        } finally {
          setMemberActionId(null);
        }
      },
    });
  };

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <Users className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">{t("title")} ({members.length})</h2>
        </div>

        {canManageWorkspace && (
          <Card>
            <CardContent className="space-y-3">
              <div className="flex items-center gap-2">
                <Plus className="h-4 w-4 text-muted-foreground" />
                <h3 className="text-sm font-medium">{t("inviteByEmail")}</h3>
              </div>
              <div className="grid gap-3 sm:grid-cols-[1fr_120px_auto]">
                <Input
                  type="email"
                  value={inviteEmail}
                  onChange={(e) => setInviteEmail(e.target.value)}
                  placeholder={t("emailPlaceholder")}
                />
                <Select value={inviteRole} onValueChange={(value) => setInviteRole(value as MemberRole)}>
                  <SelectTrigger size="sm"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="member">{t("roles.member")}</SelectItem>
                    <SelectItem value="admin">{t("roles.admin")}</SelectItem>
                    {isOwner && <SelectItem value="owner">{t("roles.owner")}</SelectItem>}
                  </SelectContent>
                </Select>
                <Button onClick={handleAddMember} disabled={inviteLoading || !inviteEmail.trim()}>
                  {inviteLoading ? t("adding") : t("add")}
                </Button>
              </div>
            </CardContent>
          </Card>
        )}

        {members.length > 0 ? (
          <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
            {members.map((m, i) => (
              <div key={m.id} className={i > 0 ? "border-t border-border/50" : ""}>
                <MemberRow
                  member={m}
                  roleConfig={roleConfig}
                  canManage={canManageWorkspace}
                  canManageOwners={isOwner}
                  isSelf={m.user_id === user?.id}
                  busy={memberActionId === m.id}
                  onRoleChange={(role) => handleRoleChange(m.id, role)}
                  onRemove={() => handleRemoveMember(m)}
                  changeRoleLabel={t("changeRole")}
                  removeLabel={t("remove")}
                />
              </div>
            ))}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">{t("noMembersFound")}</p>
        )}
      </section>

      <AlertDialog open={!!confirmAction} onOpenChange={(v) => { if (!v) setConfirmAction(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirmAction?.title}</AlertDialogTitle>
            <AlertDialogDescription>{confirmAction?.description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant={confirmAction?.variant === "destructive" ? "destructive" : "default"}
              onClick={async () => {
                await confirmAction?.onConfirm();
                setConfirmAction(null);
              }}
            >
              {t("confirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
