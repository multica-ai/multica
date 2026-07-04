export type ApprovalStatus = "pending" | "approved" | "rejected";

export interface Approval {
  id: string;
  workspace_id: string;
  issue_id: string;
  requester_type: string;
  requester_id: string;
  approver_type: string;
  approver_id: string;
  status: ApprovalStatus;
  comment: string | null;
  decided_at: string | null;
  created_at: string;
  // Joined fields (from ListPendingApprovalsByApprover)
  issue_title?: string;
  issue_number?: number;
}

export interface CreateApprovalRequest {
  approver_type: string;
  approver_id: string;
}

export interface ApprovalDecisionRequest {
  comment?: string;
}
