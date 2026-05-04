"use client";

import { ArrowLeft, ShieldCheck, Wallet, Crown, Shield, User as UserIcon, UserMinus } from "lucide-react";
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
  const [confirmScopeOff, setConfirmScopeOff] = useState(false);
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

  const applyScope = async (enabled: boolean) => {
    setBusy(true);
    try {
      const updated = await api.setMemberScopeEnforcement(workspace.id, member.id, enabled);
      qc.setQueryData<MemberWithUser[]>(workspaceKeys.members(wsId), (prev) =>
        prev?.map((m) => (m.id === member.id ? { ...m, ...updated } : m)) ?? prev,
      );
      toast.success(
        enabled
          ? "Scope enforcement enabled"
          : "Scope enforcement disabled — wider 1h token will be issued",
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update toggle");
    } finally {
      setBusy(false);
    }
  };

  const handleScopeChange = (enabled: boolean) => {
    if (!enabled) {
      setConfirmScopeOff(true);
      return;
    }
    void applyScope(true);
  };

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
        <CardContent className="space-y-6">
          <div className="flex items-start gap-4">
            <ShieldCheck className="mt-0.5 h-5 w-5 text-muted-foreground" />
            <div className="flex-1">
              <div className="flex items-center justify-between gap-4">
                <h2 className="text-sm font-medium">Token-scope enforcement</h2>
                <Switch
                  checked={member.scope_enforcement_enabled}
                  disabled={!canManage || busy}
                  onCheckedChange={handleScopeChange}
                />
              </div>
              <p className="mt-1 text-xs text-muted-foreground">
                When ON, agent tasks triggered by this member receive a strict 1-hour
                per-task token (default). When OFF, the token is widened to user-scope
                for that 1 hour — use only for trusted members.
              </p>
            </div>
          </div>

          <div className="border-t border-border/40 pt-6 flex items-start gap-4">
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

      <AlertDialog open={confirmScopeOff} onOpenChange={(v) => !v && setConfirmScopeOff(false)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Disable scope enforcement for {member.name}?</AlertDialogTitle>
            <AlertDialogDescription>
              Their agent tasks will receive a 1-hour wider-scope token instead of the
              strict per-task token. Use only for trusted members.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={async () => {
                setConfirmScopeOff(false);
                await applyScope(false);
              }}
            >
              Disable
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

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
