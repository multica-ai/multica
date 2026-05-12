import { z } from "zod";

// Mirrors groupResponse in server/internal/cerebro/groups/types.go.
export interface CerebroGroup {
  id: string;
  workspace_id: string;
  name: string;
  description: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

// Mirrors groupMemberResponse in server/internal/cerebro/groups/types.go.
export interface CerebroGroupMember {
  group_id: string;
  user_id: string;
  added_by: string | null;
  added_at: string;
  user_name: string;
  user_email: string;
  user_avatar_url: string | null;
}

export interface CreateCerebroGroupRequest {
  name: string;
  description?: string | null;
}

export interface UpdateCerebroGroupRequest {
  name?: string;
  description?: string | null;
}

// Lenient schemas — every field nullable / defaulted so an older or newer
// backend cannot white-screen the settings page. See CLAUDE.md "API
// Response Compatibility".
export const cerebroGroupSchema: z.ZodType<CerebroGroup> = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  description: z.string().nullable().default(null),
  created_by: z.string().nullable().default(null),
  created_at: z.string(),
  updated_at: z.string(),
});

export const cerebroGroupListSchema = z.array(cerebroGroupSchema);

export const cerebroGroupMemberSchema: z.ZodType<CerebroGroupMember> = z.object({
  group_id: z.string(),
  user_id: z.string(),
  added_by: z.string().nullable().default(null),
  added_at: z.string(),
  user_name: z.string(),
  user_email: z.string(),
  user_avatar_url: z.string().nullable().default(null),
});

export const cerebroGroupMemberListSchema = z.array(cerebroGroupMemberSchema);

// JEH-1009 — permission model rows. Mirror server/internal/cerebro/grouppermissions
// response shapes. All non-key fields are nullable so an older or newer backend
// can drift without white-screening.

export type CerebroGroupCapability = "create_runtime" | "create_agent";

export interface CerebroGroupCapabilityRow {
  group_id: string;
  capability: string;
  granted_by: string | null;
  granted_at: string;
}

export interface CerebroGroupRuntimeAccess {
  group_id: string;
  runtime_id: string;
  granted_by: string | null;
  granted_at: string;
}

export interface CerebroGroupAgentAccess {
  group_id: string;
  agent_id: string;
  granted_by: string | null;
  granted_at: string;
}

export interface CerebroProjectGroupMember {
  project_id: string;
  group_id: string;
  added_by: string | null;
  added_at: string;
}

export const cerebroGroupCapabilityRowSchema: z.ZodType<CerebroGroupCapabilityRow> =
  z.object({
    group_id: z.string(),
    capability: z.string(),
    granted_by: z.string().nullable().default(null),
    granted_at: z.string(),
  });

export const cerebroGroupCapabilityListSchema = z.array(
  cerebroGroupCapabilityRowSchema,
);

export const cerebroGroupRuntimeAccessSchema: z.ZodType<CerebroGroupRuntimeAccess> =
  z.object({
    group_id: z.string(),
    runtime_id: z.string(),
    granted_by: z.string().nullable().default(null),
    granted_at: z.string(),
  });

export const cerebroGroupRuntimeAccessListSchema = z.array(
  cerebroGroupRuntimeAccessSchema,
);

export const cerebroGroupAgentAccessSchema: z.ZodType<CerebroGroupAgentAccess> =
  z.object({
    group_id: z.string(),
    agent_id: z.string(),
    granted_by: z.string().nullable().default(null),
    granted_at: z.string(),
  });

export const cerebroGroupAgentAccessListSchema = z.array(
  cerebroGroupAgentAccessSchema,
);

export const cerebroProjectGroupMemberSchema: z.ZodType<CerebroProjectGroupMember> =
  z.object({
    project_id: z.string(),
    group_id: z.string(),
    added_by: z.string().nullable().default(null),
    added_at: z.string(),
  });

export const cerebroProjectGroupMemberListSchema = z.array(
  cerebroProjectGroupMemberSchema,
);
