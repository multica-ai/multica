"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { issueDetailOptions } from "@multica/core/issues/queries";
import type { Proposal } from "@multica/core/company";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Squad } from "@multica/core/types";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { ReadonlyContent } from "../../editor/readonly-content";
import { useT } from "../../i18n";
import { useDecisionQueue } from "../lib/decision-queue";
import { CompanyProposalDecision } from "./company-proposal-decision";
import {
  CompanyProposalHistory,
  CompanyProposalKindLabel,
  CompanyProposalKindSummary,
  CompanyProposalRelated,
  CompanyProposalStatus,
} from "./company-proposal-details";

/**
 * The proposals waiting for the boss. The list names each one. Proposals put
 * aside with Decide later stay below the queue, and proposals the boss sent back
 * for changes sit under Waiting on the squad, so nothing leaves the full list.
 * The selected one shows its body, its state, what is related, its revision
 * history and the decision. Only an owner or admin can decide, so others read.
 */
export function CompanyDecisionsDialog({
  open,
  proposals,
  squads,
  canDecide,
  focusId = null,
  onClose,
}: {
  open: boolean;
  /**
   * Pending proposals, including those put aside, and proposals revising. A
   * proposal linked from the chat app may also be here in any state.
   */
  proposals: readonly Proposal[];
  squads: readonly Squad[];
  canDecide: boolean;
  /** A proposal to select when the dialog opens from a link. */
  focusId?: string | null;
  onClose: () => void;
}) {
  const { t } = useT("issues");
  const { needsDecision, decidedLater, waiting } = useDecisionQueue(proposals);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  useEffect(() => {
    if (focusId) setSelectedId(focusId);
  }, [focusId]);
  const listed = [...needsDecision, ...decidedLater, ...waiting];
  // A linked proposal outside the queue (decided since) is still shown, with its state.
  const selected =
    listed.find((proposal) => proposal.id === selectedId) ??
    proposals.find((proposal) => proposal.id === selectedId) ??
    listed[0] ??
    null;

  const renderItem = (proposal: Proposal) => {
    const current = proposal.id === selected?.id;
    return (
      <li key={proposal.id}>
        <button
          type="button"
          aria-current={current ? "true" : undefined}
          onClick={() => setSelectedId(proposal.id)}
          className={cn(
            "flex w-full min-w-0 flex-col items-start gap-0.5 rounded-md px-2 py-1.5 text-left hover:bg-accent",
            current && "bg-accent font-medium",
          )}
        >
          <span className="w-full truncate text-body">{proposal.title}</span>
          <span className="text-caption text-muted-foreground">
            {squads.find((squad) => squad.id === proposal.squad_id)?.name ?? ""}
          </span>
        </button>
      </li>
    );
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="flex max-h-[min(88dvh,48rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl">
        <header className="shrink-0 border-b px-4 py-3 pr-12">
          <DialogTitle className="text-heading">{t(($) => $.company.decisions_title)}</DialogTitle>
        </header>
        {selected ? (
          <div className="flex min-h-0 flex-1 flex-col md:flex-row">
            <div className="max-h-40 shrink-0 overflow-y-auto border-b p-2 md:max-h-none md:w-64 md:border-r md:border-b-0">
              {needsDecision.length > 0 ? (
                <ul aria-label={t(($) => $.company.decisions_title)}>{needsDecision.map(renderItem)}</ul>
              ) : (
                <p className="px-2 py-1.5 text-caption text-muted-foreground">{t(($) => $.company.decisions_empty)}</p>
              )}
              {decidedLater.length > 0 ? (
                <section className="mt-2">
                  <h3 className="px-2 pb-1 text-caption font-medium text-muted-foreground">
                    {t(($) => $.company.decisions_later_title)}
                  </h3>
                  <ul aria-label={t(($) => $.company.decisions_later_title)}>{decidedLater.map(renderItem)}</ul>
                </section>
              ) : null}
              {waiting.length > 0 ? (
                <section className="mt-2">
                  <h3 className="px-2 pb-1 text-caption font-medium text-muted-foreground">
                    {t(($) => $.company.decisions_waiting_title)}
                  </h3>
                  <ul aria-label={t(($) => $.company.decisions_waiting_title)}>{waiting.map(renderItem)}</ul>
                </section>
              ) : null}
            </div>
            <ProposalDetail key={selected.id} proposal={selected} squads={squads} canDecide={canDecide} />
          </div>
        ) : (
          <p className="px-4 py-8 text-center text-body text-muted-foreground">{t(($) => $.company.decisions_empty)}</p>
        )}
      </DialogContent>
    </Dialog>
  );
}

function ProposalDetail({
  proposal,
  squads,
  canDecide,
}: {
  proposal: Proposal;
  squads: readonly Squad[];
  canDecide: boolean;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: issue } = useQuery(issueDetailOptions(wsId, proposal.issue_id));

  const from = squads.find((squad) => squad.id === proposal.squad_id);
  const handoff = squads.find((squad) => squad.id === proposal.handoff_squad_id);

  return (
    <section className="flex min-h-0 min-w-0 flex-1 flex-col">
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 py-3">
        <div className="flex flex-wrap items-center gap-2 text-caption text-muted-foreground">
          {proposal.issue_identifier ? <span>{proposal.issue_identifier}</span> : null}
          {from ? <span>{t(($) => $.company.decision_from, { name: from.name })}</span> : null}
          {handoff ? <span>{t(($) => $.company.decision_handoff, { name: handoff.name })}</span> : null}
          {proposal.urgent ? <span className="font-medium text-destructive">{t(($) => $.company.decision_urgent)}</span> : null}
          <CompanyProposalKindLabel kind={proposal.kind} />
        </div>
        <h3 className="text-heading">{proposal.title}</h3>
        <CompanyProposalStatus proposal={proposal} squadName={from?.name ?? null} />
        <CompanyProposalKindSummary proposal={proposal} />
        {issue?.description ? <ReadonlyContent content={issue.description} /> : null}
        <CompanyProposalRelated related={proposal.related} />
        <CompanyProposalHistory proposal={proposal} />
      </div>
      <CompanyProposalDecision
        proposal={proposal}
        handoffName={handoff?.name ?? null}
        canDecide={canDecide}
      />
    </section>
  );
}
