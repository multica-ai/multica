import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { toast } from "sonner";
import { z } from "zod";
import { toolPolicyKeys } from "./tool-policy";

const decisionSchema = z.enum(["allow", "ask", "deny"]).catch("deny");
const roleRuleSchema = z.object({
  setting: decisionSchema,
  resource_pattern: z.string().catch(""),
  conditions: z.unknown().nullable().catch(null),
});
const rulesSchema = z.union([roleRuleSchema, z.array(roleRuleSchema)])
  .transform((value) => Array.isArray(value) ? value : [value]);
const permissionsSchema = z.record(z.string(), rulesSchema).catch({});

const roleSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  description: z.string().catch(""),
  version: z.number().int().positive().catch(1),
  permissions: permissionsSchema,
  archived_at: z.string().nullable().catch(null),
  created_by: z.string().nullable().catch(null),
  created_at: z.string(),
  updated_at: z.string(),
});

const assignmentSchema = z.object({
  role_id: z.string(),
  subject_type: z.enum(["agent", "member"]),
  subject_id: z.string(),
  subject_display_name: z.string().nullable().catch(null),
  added_by: z.string().nullable().catch(null),
  added_at: z.string(),
  expires_at: z.string().nullable().catch(null),
});

export type RoleDecision = z.infer<typeof decisionSchema>;
export type DraftRoleDecision = RoleDecision | "inherit";
export type RolePermissionRule = z.infer<typeof roleRuleSchema>;
export type PermissionRole = z.infer<typeof roleSchema>;
export type PermissionRoleAssignment = z.infer<typeof assignmentSchema>;
export type RolePermissions = Record<string, RolePermissionRule[]>;

export function withRoleDecision(
  permissions: RolePermissions,
  toolKey: string,
  decision: DraftRoleDecision,
): RolePermissions {
  const scoped = (permissions[toolKey] ?? []).filter((rule) => rule.resource_pattern !== "");
  const rules = decision === "inherit"
    ? scoped
    : [{ setting: decision, resource_pattern: "", conditions: null }, ...scoped];
  const next = { ...permissions };
  if (rules.length === 0) delete next[toolKey];
  else next[toolKey] = rules;
  return next;
}

export const permissionRoleKeys = {
  all: (workspaceId: string) => ["permission-roles", workspaceId] as const,
  list: (workspaceId: string, includeArchived: boolean) =>
    [...permissionRoleKeys.all(workspaceId), "list", includeArchived] as const,
  assignments: (workspaceId: string, roleId: string) =>
    [...permissionRoleKeys.all(workspaceId), roleId, "assignments"] as const,
};

export function permissionRolesOptions(workspaceId: string, includeArchived = false) {
  return queryOptions({
    queryKey: permissionRoleKeys.list(workspaceId, includeArchived),
    queryFn: async () => {
      const raw = await api.listCerebroRoles(workspaceId, includeArchived);
      return z.array(roleSchema).parse(raw);
    },
    enabled: !!workspaceId,
  });
}

export function permissionRoleAssignmentsOptions(workspaceId: string, roleId: string) {
  return queryOptions({
    queryKey: permissionRoleKeys.assignments(workspaceId, roleId),
    queryFn: async () => {
      const raw = await api.listCerebroRoleAssignments(roleId);
      return z.array(assignmentSchema).parse(raw);
    },
    enabled: !!workspaceId && !!roleId,
  });
}

export function useCreatePermissionRole(workspaceId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: async (body: { name: string; description: string; permissions: RolePermissions }) =>
      roleSchema.parse(await api.createCerebroRole(workspaceId, body)),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: permissionRoleKeys.all(workspaceId) });
      client.invalidateQueries({ queryKey: toolPolicyKeys.all(workspaceId) });
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "Failed to create profile"),
  });
}

export function useUpdatePermissionRole(workspaceId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, ...body }: {
      id: string;
      name?: string;
      description?: string;
      permissions?: RolePermissions;
    }) => roleSchema.parse(await api.updateCerebroRole(id, body)),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: permissionRoleKeys.all(workspaceId) });
      client.invalidateQueries({ queryKey: toolPolicyKeys.all(workspaceId) });
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "Failed to update profile"),
  });
}

export function useArchivePermissionRole(workspaceId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteCerebroRole(id),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: permissionRoleKeys.all(workspaceId) });
      client.invalidateQueries({ queryKey: toolPolicyKeys.all(workspaceId) });
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "Failed to archive profile"),
  });
}

export function useAssignPermissionRole(workspaceId: string, roleId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: async (body: {
      subject_type: "agent" | "member";
      subject_id: string;
      expires_at?: string | null;
    }) => assignmentSchema.parse(await api.assignCerebroRole(roleId, body)),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: permissionRoleKeys.assignments(workspaceId, roleId) });
      client.invalidateQueries({ queryKey: toolPolicyKeys.all(workspaceId) });
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "Failed to assign profile"),
  });
}

export function useUnassignPermissionRole(workspaceId: string, roleId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (body: { subject_type: "agent" | "member"; subject_id: string }) =>
      api.unassignCerebroRole(roleId, body.subject_type, body.subject_id),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: permissionRoleKeys.assignments(workspaceId, roleId) });
      client.invalidateQueries({ queryKey: toolPolicyKeys.all(workspaceId) });
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "Failed to remove profile assignment"),
  });
}
