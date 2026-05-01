import type { InboxItemType } from "./inbox";

export interface NotificationPreference {
  ntfy_url: string;
  ntfy_token: string;
  disabled_types: InboxItemType[];
}

export interface UpdateNotificationPreferenceRequest {
  ntfy_url: string;
  ntfy_token: string;
  disabled_types: InboxItemType[];
}

export interface TestNotificationPreferenceRequest {
  ntfy_url: string;
  ntfy_token: string;
}
