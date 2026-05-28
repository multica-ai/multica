export interface FeishuProjectIntegration {
  id?: string;
  workspace_id?: string;
  project_name: string;
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
  assign_open_items_to_owner_agent: boolean;
  business_line_field_key: string;
  business_line_field_name: string;
  last_synced_at: string | null;
  last_error: string | null;
  created_at?: string;
  updated_at?: string;
}

export interface UpdateFeishuProjectIntegrationRequest {
  project_name: string;
  project_key?: string;
  plugin_id: string;
  plugin_secret?: string;
  actor_user_key?: string | null;
  enabled: boolean;
  sync_story: boolean;
  sync_issue: boolean;
  mql_filter: string;
  status_mapping: Record<string, string>;
  reverse_status_mapping: Record<string, string>;
  assign_open_items_to_owner_agent: boolean;
  business_line_field_key?: string;
  business_line_field_name?: string;
}

export interface FeishuProjectSyncResponse {
  status: "idle" | "running" | "succeeded" | "failed";
  run?: FeishuProjectSyncRun | null;
  summary: {
    created: number;
    updated: number;
    skipped: number;
    errors: number;
  };
  error?: string;
}

export interface FeishuProjectSyncRequest {
  work_item_id?: string;
}

export interface FeishuProjectSyncRun {
  id: string;
  status: "running" | "succeeded" | "failed";
  trigger: string;
  created: number;
  updated: number;
  skipped: number;
  errors: number;
  processed: number;
  total: number;
  current_page: number;
  current_type: string;
  error: string | null;
  started_at: string | null;
  finished_at: string | null;
}

export interface FeishuProjectStatusOption {
  key: string;
  name: string;
}

export interface FeishuProjectStatusOptionsResponse {
  statuses: FeishuProjectStatusOption[];
}

export interface FeishuProjectFieldMeta {
  key: string;
  name: string;
  type: string;
}

export interface FeishuProjectFieldsResponse {
  fields: FeishuProjectFieldMeta[];
}

export interface FeishuProjectBusinessLineNode {
  id: string;
  name: string;
  parent_id?: string;
  parent_name?: string;
  children?: FeishuProjectBusinessLineNode[];
}

export interface FeishuProjectBusinessLinesResponse {
  business_lines: FeishuProjectBusinessLineNode[];
}

export interface FeishuProjectRoute {
  id: string;
  project_id: string;
  business_line_id: string;
  business_line_name: string;
  parent_business_line_id?: string;
  parent_business_line_name?: string;
  fallback_agent_id?: string | null;
  created_at?: string;
  updated_at?: string;
}

export interface FeishuProjectRoutesResponse {
  routes: FeishuProjectRoute[];
}

export interface FeishuProjectRouteInput {
  project_id: string;
  business_line_id: string;
  business_line_name: string;
  parent_business_line_id?: string;
  parent_business_line_name?: string;
  fallback_agent_id?: string | null;
}

export interface ReplaceFeishuProjectRoutesRequest {
  routes: FeishuProjectRouteInput[];
}
