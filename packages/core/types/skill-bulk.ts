// Skill Bulk Operations - Cross-workspace skill management types

export interface SkillMatrixSkill {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
}

export interface SkillMatrixWorkspace {
  id: string;
  name: string;
  slug: string;
  skill_count: number;
}

export interface SkillMatrixResponse {
  skills: SkillMatrixSkill[];
  workspaces: SkillMatrixWorkspace[];
  matrix: boolean[][]; // [skill_index][workspace_index] = has_skill
  skill_lookup: Record<string, Record<string, string>>; // skill_name -> workspace_id -> skill_id
}

export interface SkillMatrixItem {
  skill_id: string;
  skill_name: string;
  skill_description: string;
  workspace_id: string;
  workspace_name: string;
  workspace_slug: string;
  has_skill: boolean;
  is_source: boolean;
}

export interface SyncSkillRequest {
  target_workspace_ids: string[];
  overwrite_existing: boolean;
}

export interface SyncSkillResponse {
  success_count: number;
  failed_count: number;
  failed_ids?: string[];
}

export interface BulkCopySkillsRequest {
  skill_ids: string[];
  source_workspace_id: string;
  target_workspace_ids: string[];
  overwrite_existing: boolean;
}

export interface BulkCopySkillsResponse {
  copied_count: number;
  skipped_count: number;
  failed_count: number;
  failed_ids?: string[];
}

export interface SkillWorkspaceVersion {
  workspace_id: string;
  workspace_name: string;
  skill_id: string;
  content: string;
  description: string;
  updated_at: string;
}

export interface SkillDifference {
  workspace_id_1: string;
  workspace_id_2: string;
  field: string;
  same: boolean;
}

export interface SkillComparisonResponse {
  skill_name: string;
  workspaces: SkillWorkspaceVersion[];
  differences: SkillDifference[];
}

export interface SkillWithWorkspace extends Skill {
  workspace_name?: string;
  workspace_slug?: string;
}

// Import from agent.ts for extension
import type { Skill } from './agent';
export type { Skill };
