"use client";

import { Check, Loader2, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@multica/ui/components/ui/card";
import type { Approval } from "../../core/types";
import { useApproveApproval, useRejectApproval } from "../../core/mutations";
import { StatusBadge } from "./status-badge";

export interface InlineApprovalCardProps {
  approval: Approval | null;
  loading?: boolean;
  canDecide?: boolean;
}

export function InlineApprovalCard({
  approval,
  loading = false,
  canDecide = false,
}: InlineApprovalCardProps) {
  const approve = useApproveApproval();
  const reject = useRejectApproval();

  if (loading) {
    return (
      <Card size="sm" aria-live="polite">
        <CardContent className="flex items-center gap-2 text-muted-foreground">
          <Loader2 className="size-4 animate-spin" /> Loading approval…
        </CardContent>
      </Card>
    );
  }
  if (!approval) return null;

  const pending = approval.status === "pending";
  const deciding = approve.isPending || reject.isPending;

  return (
    <Card size="sm" data-approval-id={approval.id}>
      <CardHeader>
        <CardTitle>{approval.capability || "Approval requested"}</CardTitle>
        <CardDescription>
          {approval.reason || "An agent needs a human decision before continuing."}
        </CardDescription>
        <CardAction>
          <StatusBadge status={approval.status} />
        </CardAction>
      </CardHeader>
      {(approval.requester_name || pending) && (
        <CardContent className="space-y-3">
          {approval.requester_name && (
            <p className="text-xs text-muted-foreground">
              Requested by {approval.requester_name}
            </p>
          )}
          {pending && canDecide && (
            <div className="flex flex-wrap items-center gap-2">
              <Button
                size="sm"
                disabled={deciding}
                onClick={() => approve.mutate({ id: approval.id })}
              >
                <Check className="size-4" /> Approve
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={deciding}
                onClick={() => reject.mutate({ id: approval.id })}
              >
                <X className="size-4" /> Reject
              </Button>
              {deciding && <span className="text-xs text-muted-foreground">Deciding…</span>}
            </div>
          )}
        </CardContent>
      )}
    </Card>
  );
}
