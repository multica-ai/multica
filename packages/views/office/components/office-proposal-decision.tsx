"use client";

import { useState } from "react";
import { toast } from "sonner";
import { useDecideProposal, type Proposal } from "@multica/core/office";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * The note and the Approve or Reject pair for one pending proposal. Shared by
 * the decisions dialog and the brief. Renders nothing for a viewer who cannot
 * decide, so the caller only has to say who may.
 */
export function OfficeProposalDecision({
  proposal,
  handoffName,
  canDecide,
  className,
}: {
  proposal: Proposal;
  /** The squad the approval would hand a project to, when it has one. */
  handoffName?: string | null;
  canDecide: boolean;
  className?: string;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const decide = useDecideProposal(wsId);
  const [note, setNote] = useState("");

  if (!canDecide) return null;

  const decideWith = (decision: "approve" | "reject") =>
    decide.mutate(
      { id: proposal.id, decision, note: note.trim() || undefined },
      {
        onSuccess: () =>
          toast.success(
            decision === "approve"
              ? t(($) => $.office.decision_approved, { identifier: proposal.issue_identifier })
              : t(($) => $.office.decision_rejected, { identifier: proposal.issue_identifier }),
          ),
        onError: () => toast.error(t(($) => $.office.decision_failed)),
      },
    );

  return (
    <div className={cn("shrink-0 space-y-2 border-t px-4 py-3", className)}>
      <label className="flex flex-col gap-1 text-caption text-muted-foreground">
        {t(($) => $.office.decision_note)}
        <Textarea
          value={note}
          onChange={(event) => setNote(event.target.value)}
          rows={2}
          className="text-body text-foreground"
        />
      </label>
      <div className="flex flex-wrap justify-end gap-2">
        <Button type="button" variant="outline" disabled={decide.isPending} onClick={() => decideWith("reject")}>
          {t(($) => $.office.decision_reject)}
        </Button>
        <Button type="button" disabled={decide.isPending} aria-busy={decide.isPending} onClick={() => decideWith("approve")}>
          {handoffName
            ? t(($) => $.office.decision_approve_handoff, { name: handoffName })
            : t(($) => $.office.decision_approve)}
        </Button>
      </div>
    </div>
  );
}
