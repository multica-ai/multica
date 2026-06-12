// Workspace-shared quick actions: reusable comment macros surfaced as buttons
// on the issue detail sidebar. Clicking one posts its body as a comment, which
// can kick off agent work.
export interface QuickAction {
  id: string;
  workspace_id: string;
  label: string;
  body: string;
  created_at: string;
  updated_at: string;
}

export interface CreateQuickActionRequest {
  label: string;
  body: string;
}

export interface UpdateQuickActionRequest {
  label?: string;
  body?: string;
}

export interface ListQuickActionsResponse {
  quick_actions: QuickAction[];
  total: number;
}
