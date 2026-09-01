import type { AgentConversationStarter } from "./agent";

export type MarketplaceTemplateSourceType = "agent" | "squad";
export type MarketplaceTemplateVisibility = "private" | "workspace" | "public";
export type MarketplaceTemplateScope = "all" | "public" | "workspace" | "private";
export type MarketplaceTemplateSort = "popular" | "recent";

export interface MarketplaceTemplateAgentPreview {
  key: string;
  name: string;
  description: string;
  role: string;
  is_leader: boolean;
}

export interface MarketplaceTemplateSkillFileSnapshot {
  path: string;
  content: string;
}

export interface MarketplaceTemplateSkillSnapshot {
  key: string;
  name: string;
  description: string;
  content: string;
  config: Record<string, unknown>;
  files: MarketplaceTemplateSkillFileSnapshot[];
}

export interface MarketplaceTemplateAgentSnapshot {
  key: string;
  name: string;
  description: string;
  instructions: string;
  conversation_starters: AgentConversationStarter[];
  max_concurrent_tasks: number;
  skill_keys: string[];
}

export interface MarketplaceTemplateSquadSnapshot {
  name: string;
  description: string;
  instructions: string;
  leader_key: string;
  members: Array<{
    agent_key: string;
    role: string;
  }>;
}

export interface MarketplaceTemplateSnapshot {
  version: number;
  source_type: MarketplaceTemplateSourceType;
  agents: MarketplaceTemplateAgentSnapshot[];
  skills: MarketplaceTemplateSkillSnapshot[];
  squad?: MarketplaceTemplateSquadSnapshot;
}

export interface MarketplaceTemplate {
  id: string;
  source_workspace_id: string;
  created_by: string;
  creator_name: string;
  source_type: MarketplaceTemplateSourceType;
  source_id: string | null;
  name: string;
  description: string;
  tags: string[];
  visibility: MarketplaceTemplateVisibility;
  image_url: string | null;
  snapshot_version: number;
  applied_count: number;
  featured_at: string | null;
  created_at: string;
  updated_at: string;
  agent_count: number;
  skill_count: number;
  preview_agents: MarketplaceTemplateAgentPreview[];
  snapshot?: MarketplaceTemplateSnapshot;
  can_manage: boolean;
}

export interface ListMarketplaceTemplatesParams {
  query?: string;
  source_type?: MarketplaceTemplateSourceType;
  scope?: MarketplaceTemplateScope;
  sort?: MarketplaceTemplateSort;
  page?: number;
  page_size?: number;
}

export interface ListMarketplaceTemplatesResponse {
  templates: MarketplaceTemplate[];
  total: number;
  page: number;
  page_size: number;
}

export interface CreateMarketplaceTemplateRequest {
  source_type: MarketplaceTemplateSourceType;
  source_id: string;
  name: string;
  description: string;
  tags?: string[];
  visibility: MarketplaceTemplateVisibility;
  image_url?: string | null;
}

export interface ApplyMarketplaceTemplateRequest {
  name?: string;
  runtime_ids: Record<string, string>;
}

export interface ApplyMarketplaceTemplateResponse {
  template_id: string;
  agent_ids: Record<string, string>;
  squad_id: string | null;
  reused_skill_ids: string[];
}
