export interface IntegrationConnectionBinding {
  provider: "github" | "slack" | "feishu" | string;
  connection_id: string;
  display_name: string;
  status: string;
  space_ids: string[];
}

export interface ListIntegrationBindingsResponse {
  connections: IntegrationConnectionBinding[];
  can_manage: boolean;
}
