import { z } from "zod";
import {
  DEFAULT_LARK_NOTIFICATION_EVENTS,
  type LarkInstallation,
  type LarkNotificationEventType,
  type LarkWorkspaceNotificationPolicy,
  type ListLarkInstallationsResponse,
} from "../types/lark";

export const LarkNotificationEventTypeSchema = z.enum([
  "issue_assigned",
  "unassigned",
  "assignee_changed",
  "mentioned",
  "new_comment",
  "reaction_added",
  "status_changed",
  "priority_changed",
  "start_date_changed",
  "due_date_changed",
  "task_failed",
  "quick_create_failed",
  "quick_create_done",
  "autopilot_paused",
  "issue_subscribed",
]);

const NotificationEventTypesSchema = z.unknown().transform((value): LarkNotificationEventType[] => {
  const parsed = z.array(LarkNotificationEventTypeSchema).safeParse(value);
  return parsed.success ? parsed.data : [...DEFAULT_LARK_NOTIFICATION_EVENTS];
});

export const LarkInstallationSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  agent_id: z.string().nullable().optional(),
  runtime_id: z.string().nullable().optional(),
  app_id: z.string(),
  tenant_key: z.string().nullable().optional(),
  bot_open_id: z.string(),
  installer_user_id: z.string(),
  status: z.string(),
  installation_kind: z.string().optional(),
  member_user_id: z.string().nullable().optional(),
  notification_event_types: NotificationEventTypesSchema,
  region: z.string().optional(),
  installed_at: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

const LarkInstallationsSchema = z.unknown().transform((value) => {
  if (!Array.isArray(value)) return [];
  return value.flatMap((installation) => {
    const parsed = LarkInstallationSchema.safeParse(installation);
    return parsed.success ? [parsed.data] : [];
  });
});

export const ListLarkInstallationsResponseSchema = z.object({
  installations: LarkInstallationsSchema,
  notification_event_types: NotificationEventTypesSchema.optional(),
  configured: z.boolean().catch(false),
  install_supported: z.boolean().optional().catch(undefined),
}).loose();

export const createEmptyLarkInstallation = (): LarkInstallation => ({
  id: "",
  workspace_id: "",
  app_id: "",
  bot_open_id: "",
  installer_user_id: "",
  status: "revoked",
  notification_event_types: [...DEFAULT_LARK_NOTIFICATION_EVENTS],
  installed_at: "",
  created_at: "",
  updated_at: "",
});

export const LarkWorkspaceNotificationPolicySchema = z.object({
  workspace_id: z.string(),
  event_types: NotificationEventTypesSchema,
  updated_at: z.string(),
}).loose();

export const createEmptyLarkWorkspaceNotificationPolicy = (): LarkWorkspaceNotificationPolicy => ({
  workspace_id: "",
  event_types: [...DEFAULT_LARK_NOTIFICATION_EVENTS],
  updated_at: "",
});

export const createEmptyListLarkInstallationsResponse = (): ListLarkInstallationsResponse => ({
  installations: [],
  configured: false,
	notification_event_types: [...DEFAULT_LARK_NOTIFICATION_EVENTS],
});
