/** Agent configuration template (system or personal scope). */
export interface AgentConfigTemplate {
  id: string;
  workspace_id: string;
  scope: "system" | "personal";
  name: string;
  description: string;
  config: Record<string, unknown>;
  is_default: boolean;
  created_by?: string;
  created_at: string;
  updated_at: string;
}

/** Request body for creating a config template. */
export interface CreateAgentConfigTemplateRequest {
  scope: "system" | "personal";
  name: string;
  description?: string;
  config?: Record<string, unknown>;
  is_default?: boolean;
}

/** Request body for updating a config template. */
export interface UpdateAgentConfigTemplateRequest {
  name?: string;
  description?: string;
  config?: Record<string, unknown>;
  is_default?: boolean;
}

/** Agent template binding (which templates an agent uses). */
export interface AgentTemplateBinding {
  system_template_id?: string;
  personal_template_id?: string;
  skip_system_template: boolean;
  skip_personal_template: boolean;
}

/** Request body for updating an agent's template binding. */
export interface UpdateAgentTemplateBindingRequest {
  system_template_id?: string | null;
  personal_template_id?: string | null;
  skip_system_template?: boolean;
  skip_personal_template?: boolean;
}
