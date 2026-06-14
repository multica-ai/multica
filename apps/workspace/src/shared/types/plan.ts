import type { Issue } from "./issue";

export type IssueTypeLoadProfile = "deep_work" | "light_work" | "recovery" | "neutral";

export interface IssueType {
  id: string;
  workspace_id: string;
  key: string;
  name: string;
  description: string;
  color: string;
  icon: string;
  load_profile: IssueTypeLoadProfile;
  is_system: boolean;
  archived_at: string | null;
  position: number;
  created_at: string;
  updated_at: string;
}

export type PlanItemStatus = "planned" | "in_progress" | "progressed" | "done" | "skipped";

export interface PlanItem {
  id: string;
  workspace_id: string;
  user_id: string;
  plan_id: string;
  issue_id: string | null;
  issue_type_id: string | null;
  suggested_issue_type_id: string | null;
  title_snapshot: string;
  note: string;
  position: number;
  estimated_minutes: number | null;
  actual_seconds: number;
  status: PlanItemStatus;
  status_reason: string | null;
  source: "manual" | "generated" | "carry_over";
  completed_at: string | null;
  skipped_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface Plan {
  id: string;
  workspace_id: string;
  user_id: string;
  plan_date: string;
  status: string;
  energy_level: number | null;
  energy_note: string | null;
  recovery_need: boolean;
  capacity_minutes: number | null;
  capacity_note: string | null;
  items: PlanItem[];
  created_at: string;
  updated_at: string;
}

export interface UpsertPlanRequest {
  date: string;
  energy_level?: number | null;
  energy_note?: string | null;
  recovery_need?: boolean;
  capacity_minutes?: number | null;
  capacity_note?: string | null;
}

export interface CreatePlanItemRequest {
  issue_id?: string | null;
  suggested_issue_type_id?: string | null;
  title: string;
  note?: string | null;
  estimated_minutes?: number | null;
}

export interface UpdatePlanItemRequest {
  title?: string;
  note?: string;
  estimated_minutes?: number | null;
  status?: PlanItemStatus;
  status_reason?: string | null;
  suggested_issue_type_id?: string | null;
}

export interface PlanCandidatesResponse {
  issues: Issue[];
}
