export type NotificationChannel = "inbox" | "dingtalk";
export type NotificationEventType = "mentioned";
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
