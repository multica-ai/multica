export interface WebhookEndpoint {
  id: string;
  workspace_id: string;
  url: string;
  description: string | null;
  event_types: string[];
  enabled: boolean;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface WebhookDelivery {
  id: string;
  endpoint_id: string;
  event_type: string;
  payload: Record<string, unknown>;
  status: "pending" | "delivered" | "failed";
  http_status: number | null;
  response_body: string | null;
  error_message: string | null;
  attempt: number;
  created_at: string;
  delivered_at: string | null;
}

export interface CreateWebhookEndpointRequest {
  url: string;
  description?: string;
  event_types?: string[];
  enabled?: boolean;
}

export interface UpdateWebhookEndpointRequest {
  url?: string;
  description?: string;
  event_types?: string[];
  enabled?: boolean;
}

export interface CreateWebhookEndpointResponse {
  endpoint: WebhookEndpoint;
  secret: string;
}

/** All event types that can trigger outbound webhooks. */
export const WEBHOOK_EVENT_TYPES = [
  "issue_assigned",
  "status_changed",
  "task_failed",
  "agent_comment",
  "new_comment",
  "priority_changed",
  "due_date_changed",
] as const;

export type WebhookEventType = (typeof WEBHOOK_EVENT_TYPES)[number];
