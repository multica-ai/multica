"use client";

import { useState } from "react";
import { toast } from "sonner";
import {
  proposalRefusalCode,
  snoozeDateBounds,
  useDecideProposal,
  type Proposal,
} from "@multica/core/office";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { formatSnoozeDate } from "./office-proposal-details";

/**
 * The decision controls for one pending proposal: a note, then Decide later,
 * Ask for changes, Reject and Approve. Shared by the decisions dialog and the
 * brief card, so both refuse and report the same way. Renders nothing unless
 * the viewer can decide and the proposal is pending: a revising or expired
 * proposal takes no decision on the server either.
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
  const [noteMissing, setNoteMissing] = useState(false);
  const [snoozing, setSnoozing] = useState(false);
  const [until, setUntil] = useState("");

  // An unknown kind is read-only: this client cannot tell what its approval would apply.
  if (!canDecide || proposal.state !== "pending" || proposal.kind === "unknown") return null;

  const bounds = snoozeDateBounds(new Date());
  const untilValid = until >= bounds.min && until <= bounds.max;
  const trimmedNote = note.trim();

  // A refusal that the squad is revising means this view is stale; the
  // mutation has already refetched the proposal, so the controls go away.
  const fail = (error: Error) => {
    const code = proposalRefusalCode(error);
    if (code === "proposal_revising") toast.error(t(($) => $.office.decision_revising_refused));
    else if (code === "snooze_out_of_range") toast.error(error.message);
    else toast.error(t(($) => $.office.decision_failed));
  };

  const approveOrReject = (decision: "approve" | "reject") =>
    decide.mutate(
      { id: proposal.id, decision, note: trimmedNote || undefined },
      {
        onSuccess: () =>
          toast.success(
            decision === "approve"
              ? t(($) => $.office.decision_approved, { identifier: proposal.issue_identifier })
              : t(($) => $.office.decision_rejected, { identifier: proposal.issue_identifier }),
          ),
        onError: fail,
      },
    );

  const askForChanges = () => {
    if (!trimmedNote) {
      setNoteMissing(true);
      return;
    }
    decide.mutate(
      { id: proposal.id, decision: "revise", note: trimmedNote },
      {
        onSuccess: () => toast.success(t(($) => $.office.decision_revise_asked, { identifier: proposal.issue_identifier })),
        onError: fail,
      },
    );
  };

  // The server reads the bare date as midnight UTC, the same instant the
  // picker's UTC bounds describe.
  const snooze = () =>
    decide.mutate(
      { id: proposal.id, decision: "snooze", until },
      {
        onSuccess: () => {
          setSnoozing(false);
          toast.success(
            t(($) => $.office.decision_snoozed, {
              identifier: proposal.issue_identifier,
              date: formatSnoozeDate(`${until}T00:00:00Z`),
            }),
          );
        },
        onError: fail,
      },
    );

  return (
    <div className={cn("shrink-0 space-y-2 border-t px-4 py-3", className)}>
      <label className="flex flex-col gap-1 text-caption text-muted-foreground">
        {t(($) => $.office.decision_note)}
        <Textarea
          value={note}
          onChange={(event) => {
            setNote(event.target.value);
            if (event.target.value.trim()) setNoteMissing(false);
          }}
          aria-invalid={noteMissing || undefined}
          rows={2}
          className="text-body text-foreground"
        />
      </label>
      {noteMissing ? (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.office.decision_changes_note_required)}
        </p>
      ) : null}
      {snoozing ? (
        <div className="flex flex-wrap items-center justify-end gap-2">
          <label className="flex items-center gap-2 text-caption text-muted-foreground">
            {t(($) => $.office.decision_snooze_date)}
            <Input
              type="date"
              value={until}
              min={bounds.min}
              max={bounds.max}
              onChange={(event) => setUntil(event.target.value)}
              className="w-auto"
            />
          </label>
          <Button type="button" variant="outline" onClick={() => setSnoozing(false)}>
            {t(($) => $.office.decision_cancel)}
          </Button>
          <Button type="button" disabled={!untilValid || decide.isPending} onClick={snooze}>
            {t(($) => $.office.decision_snooze_confirm)}
          </Button>
        </div>
      ) : null}
      <div className="flex flex-wrap justify-end gap-2">
        <Button
          type="button"
          variant="ghost"
          aria-expanded={snoozing}
          disabled={decide.isPending}
          onClick={() => setSnoozing((open) => !open)}
        >
          {t(($) => $.office.decision_snooze)}
        </Button>
        <Button type="button" variant="outline" disabled={decide.isPending} onClick={askForChanges}>
          {t(($) => $.office.decision_ask_changes)}
        </Button>
        <Button type="button" variant="outline" disabled={decide.isPending} onClick={() => approveOrReject("reject")}>
          {t(($) => $.office.decision_reject)}
        </Button>
        <Button
          type="button"
          disabled={decide.isPending}
          aria-busy={decide.isPending}
          onClick={() => approveOrReject("approve")}
        >
          {handoffName
            ? t(($) => $.office.decision_approve_handoff, { name: handoffName })
            : t(($) => $.office.decision_approve)}
        </Button>
      </div>
    </div>
  );
}
