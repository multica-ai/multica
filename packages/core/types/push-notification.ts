/** JSON-encoded push subscription as returned by the Push API. */
export interface PushSubscriptionKeys {
  p256dh: string;
  auth: string;
}

export interface PushSubscriptionJSON {
  endpoint: string;
  expirationTime: number | null;
  keys: PushSubscriptionKeys;
}

/** Payload sent to the server to store a user's push subscription. */
export interface CreatePushSubscriptionRequest {
  subscription: PushSubscriptionJSON;
  device_name?: string;
}

/** Server representation of a stored push subscription. */
export interface PushSubscriptionRecord {
  id: string;
  user_id: string;
  workspace_id: string;
  endpoint: string;
  p256dh: string;
  auth: string;
  device_name: string;
  created_at: string;
  updated_at: string;
}

/** Response from GET /api/push/subscriptions. */
export interface ListPushSubscriptionsResponse {
  subscriptions: PushSubscriptionRecord[];
}

/** Response from GET /api/push/vapid-key. */
export interface VapidPublicKeyResponse {
  public_key: string;
}

/** Rate limit status returned alongside push operations. */
export interface PushRateLimitStatus {
  remaining: number;
  resets_at: string;
}
