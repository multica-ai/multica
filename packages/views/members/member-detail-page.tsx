"use client";

import { ArrowLeft, Wallet, Crown, Shield, User as UserIcon, UserMinus } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useState } from "react";
import { ActorAvatar } from "../common/actor-avatar";
import type { MemberRole, MemberWithUser } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Badge } from "@multica/ui/components/ui/badge";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
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
import { useNavigation } from "../navigation";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace, paths } from "@multica/core/paths";
import {
  memberListOptions,
  memberUsageOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import { AppLink } from "../navigation";

const roleConfig: Record<MemberRole, { label: string; icon: typeof Crown }> = {
  owner: { label: "Owner", icon: Crown },
  admin: { label: "Admin", icon: Shield },
  member: { label: "Member", icon: UserIcon },
};

function formatCents(cents: number): string {
  if (!Number.isFinite(cents) || cents <= 0) return "$0.00";
  return `$${(cents / 100).toFixed(2)}`;
}

export function MemberDetailPage({ memberId }: { memberId: string }) {
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const member = members.find((m) => m.id === memberId);
  const { data: usage } = useQuery({
    ...memberUsageOptions(wsId, memberId),
    enabled: !!member,
  });

  const [busy, setBusy] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState(false);
  const navigation = useNavigation();

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";
  const isOwner = currentMember?.role === "owner";
  const isSelf = member?.user_id === user?.id;
  const canEditRole = canManage && !isSelf && (member?.role !== "owner" || isOwner);
  const canRemove = canManage && !isSelf && (member?.role !== "owner" || isOwner);

  if (!workspace) return null;

  if (!member) {
    return (
      <div className="mx-auto max-w-2xl p-6">
        <AppLink
          href={`${paths.workspace(workspace.slug).settings()}?tab=members`}
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="h-4 w-4" /> Back to Members
        </AppLink>
        <p className="mt-4 text-sm text-muted-foreground">Member not found in this workspace.</p>
      </div>
    );
  }

  const rc = roleConfig[member.role];
  const RoleIcon = rc.icon;

  const handleBudgetChange = async (enabled: boolean) => {
    setBusy(true);
    try {
      const updated = await api.setMemberBudgetEnforcement(workspace.id, member.id, enabled);
      qc.setQueryData<MemberWithUser[]>(workspaceKeys.members(wsId), (prev) =>
        prev?.map((m) => (m.id === member.id ? { ...m, ...updated } : m)) ?? prev,
      );
      toast.success(
        enabled ? "Budget cap re-enabled" : "Budget cap bypassed for this member",
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update toggle");
    } finally {
      setBusy(false);
    }
  };

  const handleRoleChange = async (role: MemberRole) => {
    if (role === member.role) return;
    setBusy(true);
    try {
      const updated = await api.updateMember(workspace.id, member.id, { role });
      qc.setQueryData<MemberWithUser[]>(workspaceKeys.members(wsId), (prev) =>
        prev?.map((m) => (m.id === member.id ? { ...m, ...updated } : m)) ?? prev,
      );
      toast.success("Role updated");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update role");
    } finally {
      setBusy(false);
    }
  };

  const handleRemove = async () => {
    setBusy(true);
    try {
      await api.deleteMember(workspace.id, member.id);
      qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
      toast.success(`${member.name} removed from workspace`);
      navigation.push(`${paths.workspace(workspace.slug).settings()}?tab=members`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to remove member");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto max-w-3xl space-y-6 p-6">
      <AppLink
        href={`${paths.workspace(workspace.slug).settings()}?tab=members`}
        className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="h-4 w-4" /> Back to Members
      </AppLink>

      <header className="flex items-center gap-4">
        <ActorAvatar actorType="member" actorId={member.user_id} size={48} />
        <div className="min-w-0 flex-1">
          <h1 className="text-xl font-semibold truncate">{member.name}</h1>
          <p className="text-sm text-muted-foreground truncate">{member.email}</p>
        </div>
        <Badge variant="secondary">
          <RoleIcon className="h-3 w-3" />
          {rc.label}
        </Badge>
      </header>

      <Card>
        <CardContent className="space-y-3">
          <h2 className="text-sm font-medium">Role</h2>
          {canEditRole ? (
            <Select
              value={member.role}
              onValueChange={(v) => handleRoleChange(v as MemberRole)}
              disabled={busy}
            >
              <SelectTrigger className="w-full sm:w-64">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(Object.entries(roleConfig) as [MemberRole, (typeof roleConfig)[MemberRole]][]).map(
                  ([role, config]) => {
                    if (role === "owner" && !isOwner) return null;
                    const Icon = config.icon;
                    return (
                      <SelectItem key={role} value={role}>
                        <span className="inline-flex items-center gap-2">
                          <Icon className="h-3.5 w-3.5" />
                          {config.label}
                        </span>
                      </SelectItem>
                    );
                  },
                )}
              </SelectContent>
            </Select>
          ) : (
            <p className="text-sm text-muted-foreground">
              {isSelf
                ? "You cannot change your own role from this page."
                : "Only an owner can change this member's role."}
            </p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardContent>
          <div className="flex items-start gap-4">
            <Wallet className="mt-0.5 h-5 w-5 text-muted-foreground" />
            <div className="flex-1">
              <div className="flex items-center justify-between gap-4">
                <h2 className="text-sm font-medium">Budget cap enforcement</h2>
                <Switch
                  checked={member.budget_enforcement_enabled}
                  disabled={!canManage || busy}
                  onCheckedChange={handleBudgetChange}
                />
              </div>
              <p className="mt-1 text-xs text-muted-foreground">
                When ON, this member's tasks are subject to the daily/monthly user
                cap. Workspace and per-agent caps always apply regardless.
              </p>
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-3">
          <h2 className="text-sm font-medium">Spend</h2>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <div className="text-xs text-muted-foreground">Today</div>
              <div className="text-lg font-semibold tabular-nums">
                {formatCents(usage?.daily_cents ?? 0)}
              </div>
            </div>
            <div>
              <div className="text-xs text-muted-foreground">This month</div>
              <div className="text-lg font-semibold tabular-nums">
                {formatCents(usage?.monthly_cents ?? 0)}
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {canManage && (
        <MemberProjectsCard wsId={wsId} memberId={memberId} memberName={member.name} />
      )}

      {canRemove && (
        <Card>
          <CardContent className="space-y-3">
            <h2 className="text-sm font-medium text-destructive">Danger zone</h2>
            <div className="flex items-center justify-between gap-4">
              <p className="text-xs text-muted-foreground">
                Remove {member.name} from this workspace. They will lose access immediately.
              </p>
              <Button
                variant="destructive"
                size="sm"
                disabled={busy}
                onClick={() => setConfirmRemove(true)}
              >
                <UserMinus className="h-3.5 w-3.5" />
                Remove
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      <AlertDialog open={confirmRemove} onOpenChange={(v) => !v && setConfirmRemove(false)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Remove {member.name}?</AlertDialogTitle>
            <AlertDialogDescription>
              {member.name} will lose access to {workspace.name}. They can be re-invited later.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={async () => {
                setConfirmRemove(false);
                await handleRemove();
              }}
            >
              Remove
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/**
 * MemberProjectsCard — lists the restricted projects this workspace member
 * is a project_member of. Hidden when the result is empty AND the request
 * succeeded; shown with a clear empty state when populated. Open projects
 * are deliberately omitted — they're noise in an open-by-default workspace.
 */
function MemberProjectsCard({
  wsId,
  memberId,
  memberName,
}: {
  wsId: string;
  memberId: string;
  memberName: string;
}) {
  const { data, isLoading } = useQuery({
    queryKey: ["workspace", wsId, "members", memberId, "projects"],
    queryFn: () => api.listMemberProjects(wsId, memberId),
  });

  const projects = data?.projects ?? [];

  return (
    <Card data-testid="member-projects-card">
      <CardContent className="space-y-3">
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-medium">Restricted projects</h2>
          {!isLoading && (
            <span className="text-xs text-muted-foreground">
              {projects.length} {projects.length === 1 ? "project" : "projects"}
            </span>
          )}
        </div>

        {isLoading ? (
          <p className="text-xs text-muted-foreground">Loading…</p>
        ) : projects.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            {memberName} is not in any restricted projects. Open projects are
            visible to every workspace member and aren&rsquo;t listed here.
          </p>
        ) : (
          <ul className="rounded-md border border-border divide-y">
            {projects.map((p) => (
              <li
                key={p.id}
                data-testid="member-project-row"
                className="flex items-center gap-3 px-3 py-2 text-sm"
              >
                <span className="size-1.5 shrink-0 rounded-full bg-muted-foreground/70" />
                <span className="shrink-0 w-5 text-center text-base">
                  {p.icon || "📁"}
                </span>
                <span className="flex-1 min-w-0 truncate">{p.title}</span>
                <span className="text-[11px] text-muted-foreground">
                  added {new Date(p.added_at).toLocaleDateString()}
                </span>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
