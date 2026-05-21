export type IntegrationProvider = "linear" | "github";

export interface Integration {
  id: string;
  workspace_id: string;
  provider: IntegrationProvider;
  enabled: boolean;
  config: LinearIntegrationConfig | GitHubIntegrationConfig;
  default_agent_id: string | null;
  webhook_secret: string | null;
  webhook_url?: string;
  created_at: string;
  updated_at: string;
}

export interface LinearIntegrationConfig {
  team_id?: string;
  project_id?: string;
  active_states?: string[];
}

export interface GitHubIntegrationConfig {
  owner?: string;
  repo?: string;
  labels?: string[];
}

export interface CreateIntegrationRequest {
  provider: IntegrationProvider;
  enabled?: boolean;
  config?: LinearIntegrationConfig | GitHubIntegrationConfig;
  default_agent_id?: string;
}

export interface UpdateIntegrationRequest {
  enabled?: boolean;
  config?: LinearIntegrationConfig | GitHubIntegrationConfig;
  default_agent_id?: string;
  webhook_secret?: string;
}

export interface ExternalIssueLink {
  id: string;
  issue_id: string;
  provider: IntegrationProvider;
  external_id: string;
  external_identifier: string | null;
  external_url: string | null;
  sync_direction: "inbound" | "outbound" | "bidirectional";
  created_at: string;
}
