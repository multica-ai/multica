"use client";

import { useState } from "react";
import { Crown, Shield, User, Plus, MoreHorizontal, UserMinus, Users, Link, Copy, RefreshCw, Link2Off } from "lucide-react";
import { ActorAvatar } from "@/components/common/actor-avatar";
import type { MemberWithUser, MemberRole } from "@/shared/types";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from "@/components/ui/alert-dialog";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
} from "@/components/ui/dropdown-menu";
import { toast } from "sonner";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { useWorkspaceSettingsMutations, useInviteLinkMutations } from "@/features/settings/mutations";

const roleConfig: Record<MemberRole, { label: string; icon: typeof Crown; description: string }> = {
  owner: { label: "Owner", icon: Crown, description: "Full access, manage all settings" },
  admin: { label: "Admin", icon: Shield, description: "Manage members and settings" },
  member: { label: "Member", icon: User, description: "Create and work on issues" },
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
                  Change role
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent className="w-auto">
                  {(Object.entries(roleConfig) as [MemberRole, (typeof roleConfig)[MemberRole]][]).map(
                    ([role, config]) => {
                      if (role === "owner" && !canManageOwners) return null;
                      const Icon = config.icon;
                      return (
                        <DropdownMenuItem
                          key={role}
                          onClick={() => onRoleChange(role)}
                        >
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
  );
}

/** Invite link section — visible to admin/owner only. */
function InviteLinkSection() {
  const workspace = useWorkspaceStore((s) => s.workspace);
  const { resetInviteLink, disableInviteLink, loadInviteToken, resetting, disabling, loading } =
    useInviteLinkMutations();

  const inviteUrl = workspace?.invite_token
    ? `${window.location.origin}/invite/${workspace.invite_token}`
    : null;

  const handleCopy = () => {
    if (!inviteUrl) return;
    navigator.clipboard.writeText(inviteUrl).then(() => toast.success("Link copied"));
  };

  const handleReset = async () => {
    try {
      await resetInviteLink();
      toast.success("Invite link reset");
    } catch {
      toast.error("Failed to reset invite link");
    }
  };

  const handleDisable = async () => {
    try {
      await disableInviteLink();
      toast.success("Invite link disabled");
    } catch {
      toast.error("Failed to disable invite link");
    }
  };

  const handleLoad = async () => {
    try {
      await loadInviteToken();
    } catch {
      toast.error("Failed to load invite link");
    }
  };

  return (
    <Card>
      <CardContent className="space-y-3">
        <div className="flex items-center gap-2">
          <Link className="h-4 w-4 text-muted-foreground" />
          <h3 className="text-sm font-medium">Invite link</h3>
        </div>
        {inviteUrl === null && workspace?.invite_token === undefined ? (
          // invite_token not yet loaded — show load button
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">Load invite link status</span>
            <Button size="sm" variant="outline" onClick={handleLoad} disabled={loading}>
              {loading ? "Loading..." : "Load"}
            </Button>
          </div>
        ) : inviteUrl ? (
          <div className="space-y-2">
            <div className="flex items-center gap-2 rounded-md border bg-muted/40 px-3 py-2">
              <span className="flex-1 truncate text-xs font-mono text-muted-foreground">{inviteUrl}</span>
            </div>
            <div className="flex items-center gap-2">
              <Button size="sm" variant="outline" onClick={handleCopy}>
                <Copy className="h-3.5 w-3.5" />
                Copy
              </Button>
              <Button size="sm" variant="outline" onClick={handleReset} disabled={resetting}>
                <RefreshCw className="h-3.5 w-3.5" />
                {resetting ? "Resetting..." : "Reset"}
              </Button>
              <Button size="sm" variant="outline" onClick={handleDisable} disabled={disabling}>
                <Link2Off className="h-3.5 w-3.5" />
                {disabling ? "Disabling..." : "Disable"}
              </Button>
            </div>
          </div>
        ) : (
          // invite_token is null — link disabled
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">Invite link is disabled.</span>
            <Button size="sm" variant="outline" onClick={handleReset} disabled={resetting}>
              {resetting ? "Generating..." : "Generate link"}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export function MembersTab() {
  const user = useAuthStore((s) => s.user);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const members = useWorkspaceStore((s) => s.members);
  const { createMember, updateMember, deleteMember } = useWorkspaceSettingsMutations();

  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState<MemberRole>("member");
  const [inviteLoading, setInviteLoading] = useState(false);
  const [inviteError, setInviteError] = useState<string | null>(null);
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
    setInviteError(null);
    try {
      await createMember({
        email: inviteEmail,
        role: inviteRole,
      });
      setInviteEmail("");
      setInviteRole("member");
      toast.success("Member added");
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Failed to add member";
      // Show contextual hint when the user is not yet registered.
      if (msg.toLowerCase().includes("not registered")) {
        setInviteError("This email is not registered. Share the invite link instead.");
      } else {
        toast.error(msg);
      }
    } finally {
      setInviteLoading(false);
    }
  };

  const handleRoleChange = async (memberId: string, role: MemberRole) => {
    if (!workspace) return;
    setMemberActionId(memberId);
    try {
      await updateMember(memberId, { role });
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
          await deleteMember(member.id);
          toast.success("Member removed");
        } catch (e) {
          toast.error(e instanceof Error ? e.message : "Failed to remove member");
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
          <h2 className="text-sm font-semibold">Members ({members.length})</h2>
        </div>

        {canManageWorkspace && (
          <>
            <InviteLinkSection />

            <Card>
              <CardContent className="space-y-3">
                <div className="flex items-center gap-2">
                  <Plus className="h-4 w-4 text-muted-foreground" />
                  <h3 className="text-sm font-medium">Add registered member</h3>
                </div>
                <p className="text-xs text-muted-foreground">Enter the email of a registered user.</p>
                <div className="grid gap-3 sm:grid-cols-[1fr_120px_auto]">
                  <Input
                    type="email"
                    value={inviteEmail}
                    onChange={(e) => {
                      setInviteEmail(e.target.value);
                      setInviteError(null);
                    }}
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
                {inviteError && (
                  <p className="text-xs text-destructive">{inviteError}</p>
                )}
              </CardContent>
            </Card>
          </>
        )}

        {members.length > 0 ? (
          <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
            {members.map((m, i) => (
              <div key={m.id} className={i > 0 ? "border-t border-border/50" : ""}>
                <MemberRow
                  member={m}
                  canManage={canManageWorkspace}
                  canManageOwners={isOwner}
                  isSelf={m.user_id === user?.id}
                  busy={memberActionId === m.id}
                  onRoleChange={(role) => handleRoleChange(m.id, role)}
                  onRemove={() => handleRemoveMember(m)}
                />
              </div>
            ))}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">No members found.</p>
        )}
      </section>

      <AlertDialog open={!!confirmAction} onOpenChange={(v) => { if (!v) setConfirmAction(null); }}>
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

