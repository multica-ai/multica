import { z } from "zod";

// Server response: GET /api/cerebro/workspaces/{id}/auth-settings.
// Keep this schema lenient — extra unknown fields from a newer backend must
// not white-screen the settings page (API Response Compatibility rules).
export const authSettingsSchema = z.object({
  workspace_id: z.string(),
  google_signup_domains: z.array(z.string()).default([]),
  default_role: z.string().default("member"),
  default_group_id: z.string().nullable().optional(),
  google_workspace_sync_enabled: z.boolean().default(false),
  updated_at: z.string().default(""),
  updated_by_user_id: z.string().nullable().optional(),
});

export type CerebroAuthSettings = z.infer<typeof authSettingsSchema>;

export const EMPTY_AUTH_SETTINGS: CerebroAuthSettings = {
  workspace_id: "",
  google_signup_domains: [],
  default_role: "member",
  default_group_id: null,
  google_workspace_sync_enabled: false,
  updated_at: "",
  updated_by_user_id: null,
};

export const SUPPORTED_DEFAULT_ROLES = ["member", "admin", "owner"] as const;
export type CerebroAuthSettingsRole = (typeof SUPPORTED_DEFAULT_ROLES)[number];

/** Minimal group row for the default-group picker (id + name only). */
export interface CerebroGroupPick {
  id: string;
  name: string;
}

export const cerebroGroupPickListSchema = z.array(
  z.object({
    id: z.string(),
    name: z.string(),
  }),
);
