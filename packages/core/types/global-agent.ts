import type { AgentConversationStarter } from "./agent";

/**
 * An account-level agent definition that can be enabled in any workspace the
 * owner belongs to. Enabling it creates a linked workspace agent
 * (`Agent.global_agent_id`) bound to a runtime of that workspace; the fields
 * below are synced to every linked copy, while runtime, model, skills, access
 * and secrets stay per workspace.
 */
export interface GlobalAgent {
  id: string;
  owner_id: string;
  name: string;
  description: string;
  instructions: string;
  avatar_url: string | null;
  conversation_starters: AgentConversationStarter[];
  /** One entry per workspace that holds a linked copy, enabled or not. */
  links: GlobalAgentLink[];
  created_at: string;
  updated_at: string;
}

export interface GlobalAgentLink {
  workspace_id: string;
  workspace_name: string;
  workspace_slug: string;
  agent_id: string;
  /** True when the linked copy is archived, i.e. disabled in that workspace. */
  archived: boolean;
  runtime_bound: boolean;
}

/** A runtime the caller may bind the linked agent to in one workspace. */
export interface GlobalAgentRuntimeOption {
  id: string;
  /** Already the display name (custom name when set). */
  name: string;
  provider: string;
  status: string;
  owned_by_me: boolean;
}

/** Where a global agent can be enabled: one entry per workspace of the caller. */
export interface GlobalAgentWorkspaceTarget {
  workspace_id: string;
  workspace_name: string;
  workspace_slug: string;
  /** The linked copy in this workspace, or null when never added. */
  agent: { id: string; archived: boolean; runtime_id: string } | null;
  runtimes: GlobalAgentRuntimeOption[];
  /** Empty string when the workspace has no usable runtime. */
  suggested_runtime_id: string;
}

export interface CreateGlobalAgentRequest {
  name: string;
  description?: string;
  instructions?: string;
  avatar_url?: string;
  conversation_starters?: AgentConversationStarter[];
}

export type UpdateGlobalAgentRequest = Partial<CreateGlobalAgentRequest>;

export interface EnableGlobalAgentRequest {
  workspace_id: string;
  runtime_id: string;
}
