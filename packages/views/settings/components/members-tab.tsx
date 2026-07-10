"use client";

import { useEffect, useState } from "react";
import { Crown, Shield, User, MoreHorizontal, UserMinus, Users, Building2, Search, X, ArrowLeft } from "lucide-react";
import type { MemberWithUser, MemberRole, DeptUser, DeptDepartment } from "@multica/core/types";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Badge } from "@multica/ui/components/ui/badge";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
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
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { isActiveWorkspaceMember } from "@multica/core/workspace/members";
import { api } from "@multica/core/api";
import { useT } from "../../i18n";

const ROLE_ICONS: Record<MemberRole, typeof Crown> = {
  owner: Crown,
  admin: Shield,
  member: User,
};

type MemberStatus = NonNullable<MemberWithUser["status"]>;

function formatUnknownStatus(status: string) {
  return status
    .split("_")
    .filter(Boolean)
    .map((part) => part.slice(0, 1).toUpperCase() + part.slice(1))
    .join(" ");
}

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

function useMemberStatusLabels() {
  const { t } = useT("settings");
  return {
    active: t(($) => $.members.statuses.active),
    pending_activation: t(($) => $.members.statuses.pending_activation),
    inactive: t(($) => $.members.statuses.inactive),
  } satisfies Record<MemberStatus, string>;
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
  const statusLabels = useMemberStatusLabels();
  const rc = roleConfig[member.role];
  const RoleIcon = rc.icon;
  const canEditRole = isActiveWorkspaceMember(member) && canManage && !isSelf && (member.role !== "owner" || canManageOwners);
  const canRemove = canManage && !isSelf && (member.role !== "owner" || canManageOwners);
  const isLastOwner = member.role === "owner" && ownerCount <= 1;
  const showMenu = canEditRole || canRemove;
  const employeeId = member.employee_id || member.external_user_id;
  const displayName = employeeId ? `${member.name}(${employeeId})` : member.name;
  const organizationLabel = member.dept_path || member.dept_name;
  const detailLabel = [organizationLabel, member.position].filter(Boolean).join(" ");
  const fallbackDetail = detailLabel || member.email;
  const memberStatus = member.status ?? "active";
  const statusLabel = statusLabels[memberStatus] ?? formatUnknownStatus(memberStatus);

  return (
    <div className="flex items-center gap-3 border-b px-3 py-2.5 text-sm last:border-b-0 hover:bg-muted/60">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-medium">
        {member.name.slice(0, 1).toUpperCase()}
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{displayName}</div>
        {fallbackDetail && (
          <div className="truncate text-xs text-muted-foreground">{fallbackDetail}</div>
        )}
      </div>
      {memberStatus !== "active" && (
        <Badge variant="outline">{statusLabel}</Badge>
      )}
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

export function MembersTab() {
  const { t } = useT("settings");
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  const [memberQuery, setMemberQuery] = useState("");
  const [deptUsers, setDeptUsers] = useState<DeptUser[]>([]);
  const [deptDepartments, setDeptDepartments] = useState<DeptDepartment[]>([]);
  const [selectedDepartment, setSelectedDepartment] = useState<DeptDepartment | null>(null);
  const [selectedDeptUsers, setSelectedDeptUsers] = useState<Record<string, DeptUser>>({});
  const [hiddenSelectedDeptUserKeys, setHiddenSelectedDeptUserKeys] = useState<Record<string, true>>({});
  const [memberSearchLoading, setMemberSearchLoading] = useState(false);
  const [deptAddLoading, setDeptAddLoading] = useState(false);
  const [deptAddResult, setDeptAddResult] = useState<{ added: number; skipped: number } | null>(null);
  const [deptError, setDeptError] = useState<string | null>(null);
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

  const deptUserKey = (deptUser: DeptUser) => deptUser.universal_id || deptUser.user_id;
  const memberDeptUserKey = (member: MemberWithUser) =>
    member.external_universal_id || member.external_user_id || member.employee_id || "";
  const existingDeptUserByKey = members.reduce<Record<string, DeptUser>>((acc, member) => {
    const key = memberDeptUserKey(member);
    if (!key) return acc;
    acc[key] = {
      user_id: member.external_user_id || member.employee_id || key,
      username: member.name,
      universal_id: member.external_universal_id ?? undefined,
      dept_id: member.dept_id ?? undefined,
      dept_name: member.dept_name ?? undefined,
      dept_path: member.dept_path ?? undefined,
      position: member.position ?? undefined,
      status: 1,
    };
    return acc;
  }, {});
  const selectedDeptUserByKey = { ...existingDeptUserByKey, ...selectedDeptUsers };
  const visibleExistingDeptUserByKey = deptUsers.reduce<Record<string, DeptUser>>((acc, deptUser) => {
    const key = deptUserKey(deptUser);
    if (existingDeptUserByKey[key]) {
      acc[key] = deptUser;
    }
    return acc;
  }, {});
  const displayedSelectedDeptUserByKey = { ...visibleExistingDeptUserByKey, ...selectedDeptUsers };
  const selectedDeptUserList = Object.entries(displayedSelectedDeptUserByKey)
    .filter(([key]) => !hiddenSelectedDeptUserKeys[key])
    .map(([, deptUser]) => deptUser);
  const deptUsersToAdd = selectedDeptUserList.filter((deptUser) => !existingDeptUserByKey[deptUserKey(deptUser)]);

  useEffect(() => {
    if (!canManageWorkspace) return;
    const query = memberQuery.trim();
    setDeptError(null);
    if (query === "") {
      setDeptUsers([]);
      setDeptDepartments([]);
      setSelectedDepartment(null);
      setMemberSearchLoading(false);
      return;
    }
    let cancelled = false;
    setMemberSearchLoading(true);
    const timeout = window.setTimeout(() => {
      Promise.all([
        api.searchDeptUsers(query),
        api.searchDeptDepartments(query),
      ])
        .then(([users, departments]) => {
          if (cancelled) return;
          setDeptUsers(users);
          setDeptDepartments(departments);
          setSelectedDepartment(null);
        })
        .catch(() => {
          if (cancelled) return;
          setDeptUsers([]);
          setDeptDepartments([]);
          setDeptError(t(($) => $.members.dept_search_failed));
        })
        .finally(() => {
          if (!cancelled) setMemberSearchLoading(false);
        });
    }, 200);
    return () => {
      cancelled = true;
      window.clearTimeout(timeout);
    };
  }, [canManageWorkspace, memberQuery, t]);

  const handleSelectDepartment = async (department: DeptDepartment) => {
    setSelectedDepartment(department);
    setDeptUsers([]);
    setDeptError(null);
    setMemberSearchLoading(true);
    try {
      const users = await api.listDeptDepartmentUsers(department.dept_id);
      setDeptUsers(users);
    } catch (e) {
      setDeptError(e instanceof Error ? e.message : t(($) => $.members.dept_search_failed));
    } finally {
      setMemberSearchLoading(false);
    }
  };

  const handleBackToDepartments = () => {
    setSelectedDepartment(null);
    setDeptUsers([]);
    setDeptError(null);
    setMemberSearchLoading(false);
  };

  const toggleDeptUser = (deptUser: DeptUser) => {
    const key = deptUserKey(deptUser);
    if (existingDeptUserByKey[key]) return;
    setSelectedDeptUsers((current) => {
      const next = { ...current };
      if (next[key]) {
        delete next[key];
      } else {
        next[key] = deptUser;
        setHiddenSelectedDeptUserKeys((hidden) => {
          const { [key]: _removed, ...rest } = hidden;
          return rest;
        });
      }
      return next;
    });
  };

  const selectableDeptUsers = deptUsers.filter((deptUser) => !existingDeptUserByKey[deptUserKey(deptUser)]);
  const currentDeptUsersSelected = deptUsers.length > 0 && selectableDeptUsers.every((deptUser) => selectedDeptUserByKey[deptUserKey(deptUser)] && !hiddenSelectedDeptUserKeys[deptUserKey(deptUser)]);

  const toggleCurrentDeptUsers = () => {
    setSelectedDeptUsers((current) => {
      const next = { ...current };
      if (currentDeptUsersSelected) {
        selectableDeptUsers.forEach((deptUser) => {
          delete next[deptUserKey(deptUser)];
        });
        setHiddenSelectedDeptUserKeys((hidden) => {
          const nextHidden = { ...hidden };
          selectableDeptUsers.forEach((deptUser) => {
            nextHidden[deptUserKey(deptUser)] = true;
          });
          return nextHidden;
        });
      } else {
        selectableDeptUsers.forEach((deptUser) => {
          next[deptUserKey(deptUser)] = deptUser;
        });
        setHiddenSelectedDeptUserKeys((hidden) => {
          const nextHidden = { ...hidden };
          selectableDeptUsers.forEach((deptUser) => {
            delete nextHidden[deptUserKey(deptUser)];
          });
          return nextHidden;
        });
      }
      return next;
    });
  };

  const handleRemoveSelectedDeptUser = (deptUser: DeptUser) => {
    const key = deptUserKey(deptUser);
    if (existingDeptUserByKey[key]) return;
    setSelectedDeptUsers((current) => {
      const { [key]: _removed, ...rest } = current;
      return rest;
    });
    setHiddenSelectedDeptUserKeys((hidden) => ({ ...hidden, [key]: true }));
  };

  const handleBatchAddDeptMembers = async () => {
    if (!workspace || deptUsersToAdd.length === 0) return;
    setDeptAddLoading(true);
    setDeptAddResult(null);
    try {
      const result = await api.batchAddDeptMembers(workspace.id, {
        users: deptUsersToAdd.map((deptUser) => ({
          external_user_id: deptUser.user_id,
          external_universal_id: deptUser.universal_id ?? undefined,
        })),
      });
      setSelectedDeptUsers({});
      setHiddenSelectedDeptUserKeys({});
      setMemberQuery("");
      setDeptUsers([]);
      setDeptDepartments([]);
      setSelectedDepartment(null);
      setDeptAddResult(result);
      qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
      toast.success(t(($) => $.members.toast_dept_members_added, { added: result.added, skipped: result.skipped }));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.members.toast_dept_members_add_failed));
    } finally {
      setDeptAddLoading(false);
    }
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
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <Users className="h-4 w-4 text-muted-foreground" />
            <h2 className="text-sm font-semibold">{t(($) => $.members.section_title, { count: members.length })}</h2>
          </div>
        </div>

        {canManageWorkspace && (
          <Card className="w-full">
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between gap-3">
                <div className="flex items-center gap-2">
                  <Users className="h-4 w-4 text-muted-foreground" />
                  <h3 className="text-sm font-medium">{t(($) => $.members.dept_add_title)}</h3>
                </div>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.members.dept_selected_count, { count: selectedDeptUserList.length })}
                </p>
              </div>
              <div className="relative">
                <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  className="pl-9"
                  value={memberQuery}
                  onChange={(e) => {
                    setDeptAddResult(null);
                    setMemberQuery(e.target.value);
                  }}
                  placeholder={t(($) => $.members.dept_search_placeholder)}
                />
              </div>

              {deptAddResult && (
                <div className="rounded-md border border-primary/20 bg-primary/5 px-3 py-2 text-xs font-medium text-primary">
                  {t(($) => $.members.toast_dept_members_added, {
                    added: deptAddResult.added,
                    skipped: deptAddResult.skipped,
                  })}
                </div>
              )}

              <div className="overflow-hidden rounded-md border">
                {!selectedDepartment && (
                  <>
                    <div className="border-b bg-muted/30 px-3 py-2">
                      <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
                        <Building2 className="h-3.5 w-3.5" />
                        {t(($) => $.members.dept_results_departments)}
                      </div>
                    </div>
                    <div data-testid="dept-department-results" className="max-h-72 overflow-y-auto">
                      {deptDepartments.map((department) => (
                        <button
                          key={department.dept_id}
                          type="button"
                          className="flex w-full items-center gap-3 border-b px-3 py-2.5 text-left text-sm last:border-b-0 hover:bg-muted/60"
                          onClick={() => handleSelectDepartment(department)}
                        >
                          <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-muted">
                            <Building2 className="h-4 w-4 text-muted-foreground" />
                          </span>
                          <span className="min-w-0 flex-1">
                            <span className="block truncate font-medium">{department.dept_name}</span>
                            <span className="block truncate text-xs text-muted-foreground">
                              {department.dept_path || department.dept_id}
                            </span>
                          </span>
                          <span className="shrink-0 rounded-md border px-2 py-1 text-xs text-muted-foreground">
                            {t(($) => $.members.dept_view_members)}
                          </span>
                        </button>
                      ))}
                    </div>
                    {memberQuery.trim() && !memberSearchLoading && !deptError && deptDepartments.length === 0 && (
                      <p className="px-3 py-3 text-xs text-muted-foreground">{t(($) => $.members.dept_departments_empty)}</p>
                    )}
                  </>
                )}

                {selectedDeptUserList.length > 0 && (
                  <div data-testid="dept-selected-panel" className="border-b bg-muted/10 px-3 py-2">
                    <div className="mb-2 text-xs font-medium text-muted-foreground">
                      {t(($) => $.members.dept_selected_title)}
                    </div>
                    <div className="flex flex-wrap gap-2">
                      {selectedDeptUserList.map((deptUser) => (
                        <Badge key={deptUserKey(deptUser)} variant="secondary" className="gap-1 rounded-md pr-1">
                          <span>{deptUser.username}</span>
                          <span className="text-muted-foreground">{deptUser.user_id}</span>
                          {!existingDeptUserByKey[deptUserKey(deptUser)] && (
                            <button
                              type="button"
                              className="ml-1 inline-flex h-4 w-4 items-center justify-center rounded-sm hover:bg-muted-foreground/15"
                              aria-label={`Remove ${deptUser.username}`}
                              onClick={() => handleRemoveSelectedDeptUser(deptUser)}
                            >
                              <X className="h-3 w-3" />
                            </button>
                          )}
                        </Badge>
                      ))}
                    </div>
                  </div>
                )}

                <div className={`${selectedDepartment ? "border-b" : "border-y"} bg-muted/30 px-3 py-2`}>
                  <div className="flex items-center justify-between gap-3">
                    <div className="flex min-w-0 items-center gap-2 text-xs font-medium text-muted-foreground">
                      {selectedDepartment && (
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-sm"
                          aria-label={t(($) => $.members.dept_back_to_departments)}
                          title={t(($) => $.members.dept_back_to_departments)}
                          onClick={handleBackToDepartments}
                        >
                          <ArrowLeft className="h-3.5 w-3.5" />
                        </Button>
                      )}
                      <Users className="h-3.5 w-3.5 shrink-0" />
                      <span className="truncate">
                        {selectedDepartment
                          ? t(($) => $.members.dept_members_in_department, { name: selectedDepartment.dept_name })
                          : t(($) => $.members.dept_results_members)}
                      </span>
                    </div>
                    {selectedDepartment && deptUsers.length > 0 && (
                      <label className="flex cursor-pointer items-center gap-2 text-xs text-muted-foreground">
                        <Checkbox
                          aria-label={t(($) => $.members.dept_select_all_members)}
                          checked={currentDeptUsersSelected}
                          onCheckedChange={toggleCurrentDeptUsers}
                        />
                        {t(($) => $.members.dept_select_all_members)}
                      </label>
                    )}
                  </div>
                  {selectedDepartment && (
                    <div className="mt-1 truncate text-xs text-muted-foreground">
                      {selectedDepartment.dept_path || selectedDepartment.dept_id}
                    </div>
                  )}
                </div>

                {memberSearchLoading && (
                  <p className="px-3 py-3 text-xs text-muted-foreground">{t(($) => $.members.dept_searching)}</p>
                )}
                {deptError && <p className="px-3 py-3 text-xs text-destructive">{deptError}</p>}
                <div data-testid="dept-member-results" className="max-h-72 overflow-y-auto">
                  {deptUsers.map((deptUser) => {
                    const key = deptUserKey(deptUser);
                    const departmentLabel = deptUser.dept_path || deptUser.dept_name;
                    const detailLabel = [departmentLabel, deptUser.position].filter(Boolean).join(" ");
                    return (
                      <label
                        key={key}
                        className="flex cursor-pointer items-center gap-3 border-b px-3 py-2.5 text-sm last:border-b-0 hover:bg-muted/60"
                      >
                        <Checkbox
                          aria-label={deptUser.username}
                          checked={!!selectedDeptUserByKey[key] && !hiddenSelectedDeptUserKeys[key]}
                          disabled={!!existingDeptUserByKey[key]}
                          onCheckedChange={() => toggleDeptUser(deptUser)}
                        />
                        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-medium">
                          {deptUser.username.slice(0, 1).toUpperCase()}
                        </div>
                        <div className="min-w-0 flex-1">
                          <div className="truncate font-medium">{deptUser.username}({deptUser.user_id})</div>
                          {detailLabel && (
                            <div className="truncate text-xs text-muted-foreground">
                              {detailLabel}
                            </div>
                          )}
                        </div>
                      </label>
                    );
                  })}
                  {memberQuery.trim() && !memberSearchLoading && !deptError && deptUsers.length === 0 && (
                    <p className="px-3 py-3 text-xs text-muted-foreground">{t(($) => $.members.dept_members_empty)}</p>
                  )}
                </div>
              </div>

              <div className="flex items-center justify-start gap-3">
                <Button
                  type="button"
                  onClick={handleBatchAddDeptMembers}
                  disabled={deptAddLoading || deptUsersToAdd.length === 0}
                >
                  {deptAddLoading ? t(($) => $.members.dept_adding) : t(($) => $.members.dept_add_selected)}
                </Button>
              </div>
            </CardContent>
          </Card>
        )}

        {members.length > 0 ? (
          <div className="w-full overflow-hidden rounded-md border">
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
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">{t(($) => $.members.no_members)}</p>
        )}
      </section>

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
