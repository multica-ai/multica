export interface FeishuProjectIntegration {
  id?: string;
  workspace_id?: string;
  project_key: string;
  plugin_id: string;
  has_plugin_secret: boolean;
  actor_user_key: string | null;
  enabled: boolean;
  sync_story: boolean;
  sync_issue: boolean;
  mql_filter: string;
  status_mapping: Record<string, string>;
  reverse_status_mapping: Record<string, string>;
  last_synced_at: string | null;
  last_error: string | null;
  created_at?: string;
  updated_at?: string;
}

export interface UpdateFeishuProjectIntegrationRequest {
  project_key: string;
  plugin_id: string;
  plugin_secret?: string;
  actor_user_key?: string | null;
  enabled: boolean;
  sync_story: boolean;
  sync_issue: boolean;
  mql_filter: string;
  status_mapping: Record<string, string>;
  reverse_status_mapping: Record<string, string>;
}

export interface FeishuProjectSyncResponse {
  status: "succeeded" | "failed";
  summary: {
    created: number;
    updated: number;
    skipped: number;
    errors: number;
  };
  error?: string;
}

export interface FeishuProjectStatusOption {
  key: string;
  name: string;
}

export interface FeishuProjectStatusOptionsResponse {
  statuses: FeishuProjectStatusOption[];
}
