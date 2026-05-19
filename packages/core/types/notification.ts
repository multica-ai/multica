export type NotificationChannel = "inbox" | "dingtalk" | "email" | "custom_webhook" | "openclaw_weixin";
export type NotificationEventType = "mentioned" | "issue_assigned" | "subscribed_issue_updated" | "task_completed" | "task_failed" | "replied";
export type ExternalAccountBindingStatus = "active" | "expired" | "revoked" | "error";

export interface ExternalAccountBinding {
  id: string;
  provider: string;
  external_user_id: string;
  display_name: string | null;
  status: ExternalAccountBindingStatus;
  metadata: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface NotificationChannelPreference {
  channel: NotificationChannel;
  event_type: NotificationEventType;
  enabled: boolean;
  binding_id: string | null;
  requires_binding: boolean;
}

export interface ListNotificationBindingsResponse {
  bindings: ExternalAccountBinding[];
}

export interface ListNotificationPreferencesResponse {
  preferences: NotificationChannelPreference[];
}

export interface UpdateNotificationPreferenceRequest {
  channel: NotificationChannel;
  event_type: NotificationEventType;
  enabled: boolean;
}

export interface NotificationWebhook {
  id: string;
  name: string;
  masked_url: string;
  enabled: boolean;
  workspace_id: string | null;
  payload_template: string;
  content_prefix: string;
  created_at: string;
  updated_at: string;
}

export interface ListNotificationWebhooksResponse {
  webhooks: NotificationWebhook[];
}

export interface CreateNotificationWebhookRequest {
  name: string;
  url: string;
  payload_template?: string;
  content_prefix?: string;
  enabled?: boolean;
}

export interface UpdateNotificationWebhookRequest {
  name?: string;
  url?: string;
  payload_template?: string;
  content_prefix?: string;
  enabled?: boolean;
}

export interface TestNotificationWebhookResponse {
  message: string;
}

export interface StartDingTalkBindingRequest {
  next_path?: string;
  redirect_uri?: string;
}

export interface StartDingTalkBindingResponse {
  auth_url: string;
}

export interface CompleteDingTalkBindingResponse {
  binding: ExternalAccountBinding;
  next_path: string | null;
}

export interface StartEmailBindingRequest {
  email: string;
}

export interface StartEmailBindingResponse {
  message: string;
}

export interface VerifyEmailBindingRequest {
  email: string;
  code: string;
}

export interface VerifyEmailBindingResponse {
  binding: ExternalAccountBinding;
}

export interface StartGoogleBindingRequest {
  next_path?: string;
  redirect_uri?: string;
}

export interface StartGoogleBindingResponse {
  auth_url: string;
}

export interface CompleteGoogleBindingResponse {
  binding: ExternalAccountBinding;
  next_path: string | null;
}

export interface BindOpenclawWeixinRequest {
  wechat_id: string;
  channel?: string;
}

export interface BindOpenclawWeixinResponse {
  id: string;
  provider: string;
  external_user_id: string;
  display_name: string | null;
  status: string;
  metadata: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}
