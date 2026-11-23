import { z } from "zod";

export const PlatformSettingValueSchema = z.union([z.boolean(), z.array(z.string())]);
export type PlatformSettingValue = z.infer<typeof PlatformSettingValueSchema>;

export const PlatformSettingsSchema = z.array(z.object({
  key: z.string(),
  value: PlatformSettingValueSchema,
  source: z.string(),
  available: z.boolean(),
}));

export const PlatformWorkspaceSchema = z.object({
  id: z.string(),
  name: z.string(),
  slug: z.string(),
});

export const PlatformWorkspacePageSchema = z.object({
  items: z.array(PlatformWorkspaceSchema),
  next: z.string(),
});

export const PlatformUserPageSchema = z.object({
  items: z.array(z.object({
    id: z.string(),
    name: z.string(),
    email: z.string(),
    has_workspace: z.boolean(),
  })),
  next: z.string(),
});

export const PlatformWorkspaceMembersSchema = z.array(z.object({
  id: z.string(),
  role: z.string(),
  user_name: z.string(),
  user_email: z.string(),
}));
