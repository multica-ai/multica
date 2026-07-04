import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { listApprovalsByIssueOptions } from "@multica/core/approvals/queries";
import { useCreateApproval, useApproveApproval, useRejectApproval } from "@multica/core/approvals/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Check, X, ShieldAlert, ShieldCheck, ShieldX } from "lucide-react";
import type { Approval, IssueAssigneeType } from "@multica/core/types";
import { AssigneePicker } from "./pickers";

export function ApprovalWidget({ issueId }: { issueId: string }) {
  const wsId = useWorkspaceId();
  const currentUserId = useAuthStore((s) => s.user?.id);
  const { data = [] } = useQuery(listApprovalsByIssueOptions(wsId, issueId));
  const approvals = data as Approval[];
  const [requesting, setRequesting] = useState(false);

  const createApproval = useCreateApproval();
  const approveApproval = useApproveApproval();
  const rejectApproval = useRejectApproval();

  const handleRequestApproval = async (updates: { assignee_type?: IssueAssigneeType | null, assignee_id?: string | null }) => {
    if (!updates.assignee_id || !currentUserId) return;
    await createApproval.mutateAsync({
      workspaceId: wsId,
      issueId: issueId,
      approverType: updates.assignee_type || "member",
      approverId: updates.assignee_id,
    });
    setRequesting(false);
  };

  const pendingApprovals = approvals.filter((a: Approval) => a.status === "pending");
  const decidedApprovals = approvals.filter((a: Approval) => a.status !== "pending");

  return (
    <div className="space-y-3">
      {pendingApprovals.map((approval: Approval) => {
        const isApprover = approval.approver_id === currentUserId;
        return (
          <div key={approval.id} className="flex flex-col gap-2 p-3 border rounded-md bg-yellow-50/50">
            <div className="flex items-center gap-2 text-sm font-medium text-yellow-800">
              <ShieldAlert className="h-4 w-4" />
              <span>Pending Approval</span>
            </div>
            {isApprover ? (
              <div className="flex gap-2 mt-2">
                <Button size="sm" className="bg-green-600 hover:bg-green-700 text-white" onClick={() => approveApproval.mutateAsync({ workspaceId: wsId, approvalId: approval.id, comment: "" })}>
                  <Check className="mr-1 h-3.5 w-3.5" /> Approve
                </Button>
                <Button size="sm" variant="outline" className="text-red-600 hover:bg-red-50" onClick={() => rejectApproval.mutateAsync({ workspaceId: wsId, approvalId: approval.id, comment: "" })}>
                  <X className="mr-1 h-3.5 w-3.5" /> Reject
                </Button>
              </div>
            ) : (
              <p className="text-xs text-muted-foreground">Waiting for reviewer...</p>
            )}
          </div>
        );
      })}
      
      {decidedApprovals.map((approval: Approval) => (
        <div key={approval.id} className={`flex items-center gap-2 p-2 border rounded-md text-sm ${approval.status === 'approved' ? 'bg-green-50/50 text-green-800 border-green-200' : 'bg-red-50/50 text-red-800 border-red-200'}`}>
          {approval.status === 'approved' ? <ShieldCheck className="h-4 w-4" /> : <ShieldX className="h-4 w-4" />}
          <span className="font-medium capitalize">{approval.status}</span>
        </div>
      ))}

      {!requesting && pendingApprovals.length === 0 && (
        <Button variant="outline" size="sm" className="w-full text-xs" onClick={() => setRequesting(true)}>
          Request Approval
        </Button>
      )}

      {requesting && (
        <div className="p-3 border rounded-md space-y-2">
          <p className="text-xs font-medium">Select Reviewer</p>
          <AssigneePicker assigneeType={null} assigneeId={null} onUpdate={handleRequestApproval} />
          <Button variant="ghost" size="sm" className="w-full text-xs" onClick={() => setRequesting(false)}>
            Cancel
          </Button>
        </div>
      )}
    </div>
  );
}
