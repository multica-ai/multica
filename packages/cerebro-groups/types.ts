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
