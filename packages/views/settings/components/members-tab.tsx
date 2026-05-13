"use client";

import { useState } from "react";
import { Crown, Shield, User, MoreHorizontal, UserMinus, Users, Clock, X, Mail, Link, Copy } from "lucide-react";
import { ActorAvatar } from "../../common/actor-avatar";
import type { MemberWithUser, MemberRole, Invitation, InviteLink } from "@multica/core/types";
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
import { memberListOptions, invitationListOptions, inviteLinkListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import { useT } from "../../i18n";

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
  /** Total number of owners in this workspace — needed to gate demoting the
   *  last owner per `workspace.go:497-507`. */
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
  const { t } = useT("settings");
  const roleConfig = useRoleLabels();
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
          <span>{t(($) => $.members.pending_status)}</span>
        </div>
      </div>
      {canManage && (
        <Button
          variant="ghost"
          size="icon-sm"
          disabled={busy}
          onClick={onRevoke}
          title={t(($) => $.members.revoke_invitation_tooltip)}
        >
          <X className="h-4 w-4 text-muted-foreground" />
        </Button>
      )}
      <Badge variant="outline">
        {rc.label}
      </Badge>
    </div>
  );
}

function InviteLinkRow({
  inviteLink,
  inviteURL,
  canDelete,
  onDelete,
  onCopy,
  busy,
}: {
  inviteLink: InviteLink;
  inviteURL: string;
  canDelete: boolean;
  onDelete: () => void;
  onCopy: () => void;
  busy: boolean;
}) {
  const { t } = useT("settings");
  const roleConfig = useRoleLabels();
  const rc = roleConfig[inviteLink.role];
  const statusLabel: Record<InviteLink["status"], string> = {
    valid: t(($) => $.members.invite_link_status_active),
    expired: t(($) => $.members.invite_link_status_expired),
    revoked: t(($) => $.members.invite_link_status_revoked),
    used_up: t(($) => $.members.invite_link_status_used_up),
  };
  const usageLabel = t(($) => $.members.invite_link_usage, {
    used: String(inviteLink.used_count),
    max: String(inviteLink.max_uses),
  });

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <div className="flex h-8 w-8 items-center justify-center rounded-full bg-muted">
        <Link className="h-4 w-4 text-muted-foreground" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium truncate">{t(($) => $.members.invite_link_label)}</div>
        <div className="text-xs text-muted-foreground truncate">
          {inviteURL || t(($) => $.members.invite_link_unavailable)}
        </div>
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
          <span>{statusLabel[inviteLink.status]}</span>
          <span>{usageLabel}</span>
          {inviteLink.expires_at && (
            <span>
              {t(($) => $.members.invite_link_expires, {
                date: new Date(inviteLink.expires_at).toLocaleDateString(),
              })}
            </span>
          )}
        </div>
      </div>
      {inviteURL && (
        <Button
          variant="ghost"
          size="icon-sm"
          disabled={busy}
          onClick={onCopy}
          title={t(($) => $.members.invite_link_copy_tooltip)}
        >
          <Copy className="h-4 w-4 text-muted-foreground" />
        </Button>
      )}
      {canDelete && (
        <Button
          variant="ghost"
          size="icon-sm"
          disabled={busy}
          onClick={onDelete}
          title={t(($) => $.members.invite_link_delete_tooltip)}
        >
          <X className="h-4 w-4 text-muted-foreground" />
        </Button>
      )}
      <Badge variant="outline">{rc.label}</Badge>
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
  const { data: invitations = [] } = useQuery(invitationListOptions(wsId));
  const { data: inviteLinks = [] } = useQuery(inviteLinkListOptions(wsId));

  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState<MemberRole>("member");
  const [inviteLoading, setInviteLoading] = useState(false);
  const [linkRole, setLinkRole] = useState<Exclude<MemberRole, "owner">>("member");
  const [linkTTLDays, setLinkTTLDays] = useState("7");
  const [linkMaxUses, setLinkMaxUses] = useState("1");
  const [linkLoading, setLinkLoading] = useState(false);
  const [generatedLink, setGeneratedLink] = useState("");
  const [memberActionId, setMemberActionId] = useState<string | null>(null);
  const [invitationActionId, setInvitationActionId] = useState<string | null>(null);
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
  const maxUsesValue = Number(linkMaxUses.trim());
  const linkMaxUsesValid = /^\d+$/.test(linkMaxUses.trim()) && maxUsesValue >= 1 && maxUsesValue <= 100;
  const ttlHoursByDays: Record<string, number> = {
    "1": 24,
    "3": 72,
    "7": 168,
    "30": 720,
  };

  const inviteURLFor = (inviteLink: InviteLink) => {
    const value = inviteLink.invite_url || (inviteLink.token ? `/invite/${encodeURIComponent(inviteLink.token)}` : `/invite/${encodeURIComponent(inviteLink.id)}`);
    if (!value) return "";
    return value.startsWith("http") ? value : `${window.location.origin}${value}`;
  };

  const handleCopyLink = async (value: string) => {
    try {
      await navigator.clipboard.writeText(value);
      toast.success(t(($) => $.members.toast_invite_link_copied));
    } catch {
      toast.error(t(($) => $.members.toast_invite_link_copy_failed));
    }
  };

  const handleCreateInviteLink = async () => {
    if (!workspace) return;
    setLinkLoading(true);
    try {
      const inviteLink = await api.createInviteLink(workspace.id, {
        role: linkRole,
        ttl_hours: ttlHoursByDays[linkTTLDays] ?? 168,
        max_uses: maxUsesValue,
      });
      const url = inviteURLFor(inviteLink);
      if (url) {
        setGeneratedLink(url);
        await handleCopyLink(url);
      }
      qc.invalidateQueries({ queryKey: workspaceKeys.inviteLinks(wsId) });
      toast.success(t(($) => $.members.toast_invite_link_generated));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_invite_link_generate_failed));
    } finally {
      setLinkLoading(false);
    }
  };

  const handleDeleteInviteLink = (inviteLink: InviteLink) => {
    if (!workspace) return;
    setConfirmAction({
      title: t(($) => $.members.invite_link_delete_title),
      description: t(($) => $.members.invite_link_delete_description),
      variant: "destructive",
      onConfirm: async () => {
        setInvitationActionId(inviteLink.id);
        try {
          await api.revokeInviteLink(workspace.id, inviteLink.id);
          qc.invalidateQueries({ queryKey: workspaceKeys.inviteLinks(wsId) });
          toast.success(t(($) => $.members.toast_invite_link_deleted));
        } catch (e) {
          toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_invite_link_delete_failed));
        } finally {
          setInvitationActionId(null);
        }
      },
    });
  };

  const handleInviteMember = async () => {
    if (!workspace) return;
    setInviteLoading(true);
    try {
      await api.createMember(workspace.id, {
        email: inviteEmail,
        role: inviteRole,
      });
      setInviteEmail("");
      setInviteRole("member");
      qc.invalidateQueries({ queryKey: workspaceKeys.invitations(wsId) });
      toast.success(t(($) => $.members.toast_invitation_sent));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_invitation_failed));
    } finally {
      setInviteLoading(false);
    }
  };

  const handleRevokeInvitation = (invitation: Invitation) => {
    if (!workspace) return;
    setConfirmAction({
      title: t(($) => $.members.revoke_invitation_title),
      description: t(($) => $.members.revoke_invitation_description, { email: invitation.invitee_email }),
      variant: "destructive",
      onConfirm: async () => {
        setInvitationActionId(invitation.id);
        try {
          await api.revokeInvitation(workspace.id, invitation.id);
          qc.invalidateQueries({ queryKey: workspaceKeys.invitations(wsId) });
          toast.success(t(($) => $.members.toast_invitation_revoked));
        } catch (e) {
          toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_invitation_revoke_failed));
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
          qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
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
    <div className="space-y-8">
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <Users className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">{t(($) => $.members.section_title, { count: members.length })}</h2>
        </div>

        {canManageWorkspace && (
          <div className="grid gap-3">
            <Card>
              <CardContent className="space-y-3">
                <div className="flex items-center gap-2">
                  <Link className="h-4 w-4 text-muted-foreground" />
                  <h3 className="text-sm font-medium">{t(($) => $.members.invite_link_title)}</h3>
                </div>
                <div className="grid gap-3 sm:grid-cols-[120px_120px_140px_auto]">
                  <Select value={linkRole} onValueChange={(value) => setLinkRole(value as Exclude<MemberRole, "owner">)}>
                    <SelectTrigger size="sm">
                      <SelectValue>{() => roleConfig[linkRole].label}</SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="member">{roleConfig.member.label}</SelectItem>
                      <SelectItem value="admin">{roleConfig.admin.label}</SelectItem>
                    </SelectContent>
                  </Select>
                  <Select value={linkTTLDays} onValueChange={(value) => { if (value) setLinkTTLDays(value); }}>
                    <SelectTrigger size="sm"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="1">{t(($) => $.members.invite_link_ttl_1d)}</SelectItem>
                      <SelectItem value="3">{t(($) => $.members.invite_link_ttl_3d)}</SelectItem>
                      <SelectItem value="7">{t(($) => $.members.invite_link_ttl_7d)}</SelectItem>
                      <SelectItem value="30">{t(($) => $.members.invite_link_ttl_30d)}</SelectItem>
                    </SelectContent>
                  </Select>
                  <Input
                    type="number"
                    min={1}
                    max={100}
                    step={1}
                    value={linkMaxUses}
                    onChange={(e) => setLinkMaxUses(e.target.value)}
                    placeholder={t(($) => $.members.invite_link_max_uses_placeholder)}
                  />
                  <Button onClick={handleCreateInviteLink} disabled={linkLoading || !linkMaxUsesValid}>
                    {linkLoading ? t(($) => $.members.invite_link_generating) : t(($) => $.members.invite_link_generate)}
                  </Button>
                </div>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.members.invite_link_summary, { days: linkTTLDays, max: linkMaxUsesValid ? String(maxUsesValue) : "?" })}
                </p>
                {!linkMaxUsesValid && (
                  <p className="text-xs text-destructive">{t(($) => $.members.invite_link_max_uses_error)}</p>
                )}
                {generatedLink && (
                  <div className="grid gap-2 sm:grid-cols-[1fr_auto]">
                    <Input value={generatedLink} readOnly />
                    <Button variant="outline" onClick={() => handleCopyLink(generatedLink)}>
                      <Copy className="h-4 w-4" />
                      {t(($) => $.members.invite_link_copy)}
                    </Button>
                  </div>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardContent className="space-y-3">
                <div className="flex items-center gap-2">
                  <Mail className="h-4 w-4 text-muted-foreground" />
                  <h3 className="text-sm font-medium">{t(($) => $.members.invite_title)}</h3>
                </div>
                <div className="grid gap-3 sm:grid-cols-[1fr_120px_auto]">
                  <Input
                    type="email"
                    value={inviteEmail}
                    onChange={(e) => setInviteEmail(e.target.value)}
                    placeholder={t(($) => $.members.invite_email_placeholder)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" && inviteEmail.trim()) handleInviteMember();
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
                    onClick={handleInviteMember}
                    disabled={inviteLoading || !inviteEmail.trim()}
                  >
                    {inviteLoading ? t(($) => $.members.inviting) : t(($) => $.members.invite_button)}
                  </Button>
                </div>
              </CardContent>
            </Card>
          </div>
        )}

        {members.length > 0 ? (
          <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
            {members.map((m, i) => (
              <div key={m.id} className={i > 0 ? "border-t border-border/50" : ""}>
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
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">{t(($) => $.members.no_members)}</p>
        )}
      </section>

      {inviteLinks.length > 0 && (
        <section className="space-y-4">
          <div className="flex items-center gap-2">
            <Link className="h-4 w-4 text-muted-foreground" />
            <h2 className="text-sm font-semibold">{t(($) => $.members.invite_links_title, { count: inviteLinks.length })}</h2>
          </div>
          <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
            {inviteLinks.map((link, i) => (
              <div key={link.id} className={i > 0 ? "border-t border-border/50" : ""}>
                <InviteLinkRow
                  inviteLink={link}
                  inviteURL={inviteURLFor(link)}
                  canDelete={isOwner}
                  onDelete={() => handleDeleteInviteLink(link)}
                  onCopy={() => handleCopyLink(inviteURLFor(link))}
                  busy={invitationActionId === link.id}
                />
              </div>
            ))}
          </div>
        </section>
      )}

      {invitations.length > 0 && (
        <section className="space-y-4">
          <div className="flex items-center gap-2">
            <Clock className="h-4 w-4 text-muted-foreground" />
            <h2 className="text-sm font-semibold">{t(($) => $.members.pending_title, { count: invitations.length })}</h2>
          </div>
          <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
            {invitations.map((inv, i) => (
              <div key={inv.id} className={i > 0 ? "border-t border-border/50" : ""}>
                <InvitationRow
                  invitation={inv}
                  canManage={canManageWorkspace}
                  onRevoke={() => handleRevokeInvitation(inv)}
                  busy={invitationActionId === inv.id}
                />
              </div>
            ))}
          </div>
        </section>
      )}

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
    </div>
  );
}
