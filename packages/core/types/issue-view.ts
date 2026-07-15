import type { IssueViewDefinition } from "../issues/stores/view-store";

export type IssueViewScopeType = "workspace" | "project" | "my";
export type IssueViewVisibility = "private" | "workspace";

export interface IssueView {
  id: string;
  workspace_id: string;
  creator_id: string;
  name: string;
  icon: string | null;
  color: string | null;
  scope_type: IssueViewScopeType;
  scope_id: string | null;
  visibility: IssueViewVisibility;
  definition: IssueViewDefinition;
  position: number;
  can_edit: boolean;
  created_at: string;
  updated_at: string;
}

export interface ListIssueViewsResponse {
  views: IssueView[];
  default_view_id: string | null;
}

export interface IssueViewScopeInput {
  scope_type: IssueViewScopeType;
  scope_id?: string | null;
}

export interface CreateIssueViewRequest extends IssueViewScopeInput {
  name: string;
  icon?: string | null;
  color?: string | null;
  visibility: IssueViewVisibility;
  definition: IssueViewDefinition;
}

export interface UpdateIssueViewRequest {
  name?: string;
  icon?: string | null;
  color?: string | null;
  visibility?: IssueViewVisibility;
  definition?: IssueViewDefinition;
}

export interface DuplicateIssueViewRequest {
  name?: string;
  visibility?: IssueViewVisibility;
}

export interface SetDefaultIssueViewRequest extends IssueViewScopeInput {
  view_id: string | null;
}
