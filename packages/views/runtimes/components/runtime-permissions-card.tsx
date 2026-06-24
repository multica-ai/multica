"use client";

import { useState, useMemo } from "react";
import { Shield, Plus, MoreHorizontal, UserMinus, Users } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime, RuntimePermissionRole, MemberWithUser } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  runtimePermissionsOptions,
  myRuntimePermissionOptions,
} from "@multica/core/runtimes/queries";
import {
  useCreateRuntimePermission,
  useUpdateRuntimePermission,
  useDeleteRuntimePermission,
} from "@multica/core/runtimes/mutations";
import { useRuntimePermissions } from "@multica/core/permissions";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
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
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";

const MANAGEABLE_ROLES: RuntimePermissionRole[] = ["admin", "operator", "viewer"];

function useRoleLabel() {
  const { t } = useT("runtimes");
  return (role: string) =>
    t(($) => ($.detail.permission_role as Record<string, string>)[role] ?? role);
}

function PermissionRow({
  permission,
  members,
  canManage,
  busy,
  onRoleChange,
  onRemove,
}: {
  permission: { id: string; user_id: string; role: RuntimePermissionRole; user_name?: string; user_email?: string };
  members: MemberWithUser[];
  canManage: boolean;
  busy: boolean;
  onRoleChange: (role: RuntimePermissionRole) => void;
  onRemove: () => void;
}) {
  const { t } = useT("runtimes");
  const roleLabel = useRoleLabel();
  const member = members.find((m) => m.user_id === permission.user_id);
  const name = permission.user_name ?? member?.name ?? permission.user_email ?? permission.user_id;
  const email = permission.user_email ?? member?.email;

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <ActorAvatar actorType="member" actorId={permission.user_id} size={32} />
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium truncate">{name}</div>
        {email && <div className="text-xs text-muted-foreground truncate">{email}</div>}
      </div>
      {canManage && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="ghost" size="icon-sm" disabled={busy}>
                <MoreHorizontal className="h-4 w-4 text-muted-foreground" />
              </Button>
            }
          />
          <DropdownMenuContent align="end">
            <DropdownMenuItem disabled>
              {t(($) => $.detail.permission_change_role)}
            </DropdownMenuItem>
            {MANAGEABLE_ROLES.map((role) => (
              <DropdownMenuItem
                key={role}
                onClick={() => onRoleChange(role)}
                disabled={busy || permission.role === role}
              >
                {roleLabel(role)}
                {permission.role === role && (
                  <span className="ml-auto text-xs text-muted-foreground">{"✓"}</span>
                )}
              </DropdownMenuItem>
            ))}
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onClick={onRemove} disabled={busy}>
              <UserMinus className="h-3.5 w-3.5" />
              {t(($) => $.detail.permission_remove_action)}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
      <Badge variant="secondary">
        <Shield className="h-3 w-3" />
        {roleLabel(permission.role || "viewer")}
      </Badge>
    </div>
  );
}

export function RuntimePermissionsCard({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT("runtimes");
  const roleLabel = useRoleLabel();
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { canManagePermissions } = useRuntimePermissions(runtime, wsId);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: permissions = { permissions: [] } } = useQuery(runtimePermissionsOptions(runtime.id));
  const { data: myPermission } = useQuery(myRuntimePermissionOptions(runtime.id));

  const createPermission = useCreateRuntimePermission(runtime.id);
  const updatePermission = useUpdateRuntimePermission(runtime.id);
  const deletePermission = useDeleteRuntimePermission(runtime.id);

  const [selectedUserId, setSelectedUserId] = useState<string>("");
  const [selectedRole, setSelectedRole] = useState<RuntimePermissionRole>("operator");
  const [removingId, setRemovingId] = useState<string | null>(null);

  const permissionUserIds = useMemo(
    () => new Set(permissions.permissions.map((p) => p.user_id)),
    [permissions.permissions],
  );

  const eligibleMembers = useMemo(
    () =>
      members.filter(
        (m) => m.user_id !== user?.id && !permissionUserIds.has(m.user_id),
      ),
    [members, permissionUserIds, user?.id],
  );

  if (!canManagePermissions.allowed && permissions.permissions.length === 0) {
    return null;
  }

  const handleAdd = () => {
    if (!selectedUserId) return;
    createPermission.mutate(
      { user_id: selectedUserId, role: selectedRole },
      {
        onSuccess: () => {
          toast.success(t(($) => $.detail.permission_added));
          setSelectedUserId("");
          setSelectedRole("operator");
        },
        onError: (err) => {
          toast.error(err instanceof Error ? err.message : t(($) => $.detail.permission_add_failed));
        },
      },
    );
  };

  const handleRoleChange = (_permissionId: string, userId: string, role: RuntimePermissionRole) => {
    updatePermission.mutate(
      { userId, req: { role } },
      {
        onSuccess: () => toast.success(t(($) => $.detail.permission_updated)),
        onError: (err) =>
          toast.error(err instanceof Error ? err.message : t(($) => $.detail.permission_update_failed)),
      },
    );
  };

  const handleRemove = () => {
    if (!removingId) return;
    deletePermission.mutate(removingId, {
      onSuccess: () => {
        toast.success(t(($) => $.detail.permission_removed));
        setRemovingId(null);
      },
      onError: (err) =>
        toast.error(err instanceof Error ? err.message : t(($) => $.detail.permission_remove_failed)),
    });
  };

  return (
    <div className="rounded-lg border">
      <div className="flex items-center justify-between border-b px-4 py-2.5">
        <div className="flex items-center gap-2">
          <Users className="h-4 w-4 text-muted-foreground" />
          <span className="text-xs font-semibold">{t(($) => $.detail.permissions_title)}</span>
        </div>
        <span className="text-xs text-muted-foreground">
          {t(($) => $.detail.permissions_count, { count: permissions.permissions.length })}
        </span>
      </div>

      {canManagePermissions.allowed && (
        <div className="border-b p-4 space-y-3">
          <div className="grid gap-3 sm:grid-cols-[1fr_120px_auto]">
            <Select
              value={selectedUserId}
              onValueChange={(value) => setSelectedUserId(value ?? "")}
            >
              <SelectTrigger size="sm">
                <SelectValue>
                  {() => {
                    const m = members.find((x) => x.user_id === selectedUserId);
                    return m?.name || t(($) => $.detail.permission_select_member);
                  }}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {eligibleMembers.length === 0 && (
                  <SelectItem value="" disabled>
                    {t(($) => $.detail.permission_no_eligible_members)}
                  </SelectItem>
                )}
                {eligibleMembers.map((m) => (
                  <SelectItem key={m.user_id} value={m.user_id}>
                    {m.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              value={selectedRole}
              onValueChange={(value) => setSelectedRole(value as RuntimePermissionRole)}
            >
              <SelectTrigger size="sm">
                <SelectValue>{() => roleLabel(selectedRole)}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {MANAGEABLE_ROLES.map((role) => (
                  <SelectItem key={role} value={role}>
                    {roleLabel(role)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button
              onClick={handleAdd}
              disabled={createPermission.isPending || !selectedUserId}
            >
              {createPermission.isPending ? (
                t(($) => $.detail.permission_adding)
              ) : (
                <>
                  <Plus className="h-3.5 w-3.5 mr-1" />
                  {t(($) => $.detail.permission_add_button)}
                </>
              )}
            </Button>
          </div>
          {myPermission?.role && (
            <p className="text-xs text-muted-foreground">
              {t(($) => $.detail.permission_your_role, { role: roleLabel(myPermission.role) })}
            </p>
          )}
        </div>
      )}

      {permissions.permissions.length === 0 ? (
        <div className="px-4 py-6 text-center text-xs text-muted-foreground">
          {t(($) => $.detail.permissions_empty)}
        </div>
      ) : (
        <div className="divide-y">
          {permissions.permissions.map((p) => (
            <PermissionRow
              key={p.id}
              permission={p}
              members={members}
              canManage={canManagePermissions.allowed}
              busy={updatePermission.isPending || deletePermission.isPending}
              onRoleChange={(role) => handleRoleChange(p.id, p.user_id, role)}
              onRemove={() => setRemovingId(p.user_id)}
            />
          ))}
        </div>
      )}

      <AlertDialog open={!!removingId} onOpenChange={(v) => { if (!v) setRemovingId(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.detail.permission_remove_dialog.title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.detail.permission_remove_dialog.description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.detail.permission_remove_dialog.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={handleRemove}
              disabled={deletePermission.isPending}
            >
              {deletePermission.isPending
                ? t(($) => $.detail.permission_remove_dialog.removing)
                : t(($) => $.detail.permission_remove_dialog.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
