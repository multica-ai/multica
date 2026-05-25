// FIR-2130: roles as a first-class permission subject. A role is a named
// subject that grants attach to (subject_type='role'); members and agents are
// assigned to roles, and the permission engine expands an actor's assigned
// roles when resolving role-scoped grants.
//
// Bodies are parsed with parseWithFallback per CLAUDE.md "API Response
// Compatibility" so a drift between the deployed backend and this client never
// white-screens the Permissions page.

import { api, parseWithFallback } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useMutation, useQueryClient, queryOptions } from "@tanstack/react-query";
import { z } from "zod";

export interface CerebroRole {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface RoleAssignment {
  role_id: string;
  subject_type: string;
  subject_id: string;
  added_by: string | null;
  added_at: string;
}

const roleSchema: z.ZodType<CerebroRole> = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  description: z.string().default(""),
  created_by: z.string().nullable().default(null),
  created_at: z.string(),
  updated_at: z.string(),
});

const roleListSchema = z.array(roleSchema).default([]);

const roleAssignmentSchema: z.ZodType<RoleAssignment> = z.object({
  role_id: z.string(),
  subject_type: z.string(),
  subject_id: z.string(),
  added_by: z.string().nullable().default(null),
  added_at: z.string(),
});

const roleAssignmentListSchema = z.array(roleAssignmentSchema).default([]);

// --- API wrappers -----------------------------------------------------------

export async function fetchRoles(wsId: string): Promise<CerebroRole[]> {
  const raw = await api.listCerebroRoles<unknown>(wsId);
  return parseWithFallback(raw, roleListSchema, [], { endpoint: "listCerebroRoles" });
}

export async function createRole(
  wsId: string,
  body: { name: string; description?: string | null },
): Promise<CerebroRole | null> {
  const raw = await api.createCerebroRole<unknown>(wsId, body);
  return parseWithFallback(raw, roleSchema, null, { endpoint: "createCerebroRole" });
}

export async function updateRole(
  roleId: string,
  body: { name?: string; description?: string | null },
): Promise<CerebroRole | null> {
  const raw = await api.updateCerebroRole<unknown>(roleId, body);
  return parseWithFallback(raw, roleSchema, null, { endpoint: "updateCerebroRole" });
}

export async function deleteRole(roleId: string): Promise<void> {
  await api.deleteCerebroRole(roleId);
}

export async function fetchRoleAssignments(roleId: string): Promise<RoleAssignment[]> {
  const raw = await api.listCerebroRoleAssignments<unknown>(roleId);
  return parseWithFallback(raw, roleAssignmentListSchema, [], {
    endpoint: "listCerebroRoleAssignments",
  });
}

export async function assignRole(
  roleId: string,
  body: { subject_type: string; subject_id: string },
): Promise<RoleAssignment | null> {
  const raw = await api.assignCerebroRole<unknown>(roleId, body);
  return parseWithFallback(raw, roleAssignmentSchema, null, {
    endpoint: "assignCerebroRole",
  });
}

export async function unassignRole(
  roleId: string,
  subjectType: string,
  subjectId: string,
): Promise<void> {
  await api.unassignCerebroRole(roleId, subjectType, subjectId);
}

// --- Query options ----------------------------------------------------------

export const rolesKeys = {
  all: (wsId: string) => ["cerebro", "roles", wsId] as const,
  list: (wsId: string) => [...rolesKeys.all(wsId), "list"] as const,
  assignments: (wsId: string, roleId: string) =>
    [...rolesKeys.all(wsId), "assignments", roleId] as const,
};

export function rolesListOptions(wsId: string) {
  return queryOptions({
    queryKey: rolesKeys.list(wsId),
    queryFn: () => fetchRoles(wsId),
    enabled: !!wsId,
    staleTime: 15 * 1000,
    placeholderData: (prev) => prev,
  });
}

export function roleAssignmentsOptions(wsId: string, roleId: string | null) {
  return queryOptions({
    queryKey: rolesKeys.assignments(wsId, roleId ?? ""),
    queryFn: () => fetchRoleAssignments(roleId ?? ""),
    enabled: !!wsId && !!roleId,
    staleTime: 15 * 1000,
  });
}

// --- Mutations --------------------------------------------------------------

export function useCreateRole() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (body: { name: string; description?: string | null }) =>
      createRole(wsId, body),
    onSettled: () => qc.invalidateQueries({ queryKey: rolesKeys.all(wsId) }),
  });
}

export function useUpdateRole() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (vars: { id: string; body: { name?: string; description?: string | null } }) =>
      updateRole(vars.id, vars.body),
    onSettled: () => qc.invalidateQueries({ queryKey: rolesKeys.all(wsId) }),
  });
}

export function useDeleteRole() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => deleteRole(id),
    onSettled: () => qc.invalidateQueries({ queryKey: rolesKeys.all(wsId) }),
  });
}

export function useAssignRole() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (vars: { roleId: string; subjectType: string; subjectId: string }) =>
      assignRole(vars.roleId, { subject_type: vars.subjectType, subject_id: vars.subjectId }),
    onSettled: () => qc.invalidateQueries({ queryKey: rolesKeys.all(wsId) }),
  });
}

export function useUnassignRole() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (vars: { roleId: string; subjectType: string; subjectId: string }) =>
      unassignRole(vars.roleId, vars.subjectType, vars.subjectId),
    onSettled: () => qc.invalidateQueries({ queryKey: rolesKeys.all(wsId) }),
  });
}
