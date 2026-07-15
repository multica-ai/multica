"use client";

import { useQuery } from "@tanstack/react-query";
import { useFlagValue } from "@multica/cerebro-feature-flags";
import { useCurrentMember } from "@multica/core/permissions";
import { approvalsOriginOptions } from "../../core/queries";
import type { Approval, ApprovalOriginFilter } from "../../core/types";
import { InlineApprovalCard } from "./inline-approval-card";

export interface InlineApprovalCardsProps {
  wsId: string;
  /** Server-side scope shared by all cards in one timeline. */
  origin: ApprovalOriginFilter;
  /** Optional client-side match for the exact turn or comment. */
  match?: Partial<Record<keyof ApprovalOriginFilter, string | null>>;
}

function matchesOrigin(
  approval: Approval,
  match: Partial<Record<keyof ApprovalOriginFilter, string | null>>,
): boolean {
  return Object.entries(match).every(([key, value]) => {
    if (value === undefined) return true;
    return approval[key as keyof Approval] === value;
  });
}

function EnabledInlineApprovalCards({ wsId, origin, match = {} }: InlineApprovalCardsProps) {
  const { role } = useCurrentMember(wsId);
  const query = useQuery(
    approvalsOriginOptions(wsId, {
      status: null,
      origin,
    }),
  );

  if (query.isLoading) return null;

  const approvals = (query.data?.approvals ?? []).filter((approval) =>
    matchesOrigin(approval, match),
  );
  if (approvals.length === 0) return null;

  return (
    <div className="space-y-2" data-inline-approvals>
      {approvals.map((approval) => (
        <InlineApprovalCard
          key={approval.id}
          approval={approval}
          canDecide={role === "owner" || role === "admin"}
        />
      ))}
    </div>
  );
}

export function InlineApprovalCards(props: InlineApprovalCardsProps) {
  const enabled = useFlagValue("cerebro_approvals");
  if (!enabled) return null;
  return <EnabledInlineApprovalCards {...props} />;
}
