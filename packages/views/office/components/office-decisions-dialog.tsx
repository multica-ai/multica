"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { issueDetailOptions } from "@multica/core/issues/queries";
import type { Proposal } from "@multica/core/office";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Squad } from "@multica/core/types";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { ReadonlyContent } from "../../editor/readonly-content";
import { useT } from "../../i18n";
import { OfficeProposalDecision } from "./office-proposal-decision";

/**
 * The proposals waiting for the boss. The list names each one; the selected
 * one shows its rendered body, who filed it, where it would hand off, and the
 * decision. Only an owner or admin can decide, so others read.
 */
export function OfficeDecisionsDialog({
  open,
  proposals,
  squads,
  canDecide,
  onClose,
}: {
  open: boolean;
  proposals: readonly Proposal[];
  squads: readonly Squad[];
  canDecide: boolean;
  onClose: () => void;
}) {
  const { t } = useT("issues");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const selected = proposals.find((proposal) => proposal.id === selectedId) ?? proposals[0] ?? null;

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="flex max-h-[min(88dvh,48rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl">
        <header className="shrink-0 border-b px-4 py-3 pr-12">
          <DialogTitle className="text-heading">{t(($) => $.office.decisions_title)}</DialogTitle>
        </header>
        {selected ? (
          <div className="flex min-h-0 flex-1 flex-col md:flex-row">
            <ul aria-label={t(($) => $.office.decisions_title)} className="max-h-40 shrink-0 overflow-y-auto border-b p-2 md:max-h-none md:w-64 md:border-r md:border-b-0">
              {proposals.map((proposal) => {
                const current = proposal.id === selected.id;
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
              })}
            </ul>
            <ProposalDetail key={selected.id} proposal={selected} squads={squads} canDecide={canDecide} />
          </div>
        ) : (
          <p className="px-4 py-8 text-center text-body text-muted-foreground">{t(($) => $.office.decisions_empty)}</p>
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
      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
        <div className="flex flex-wrap items-center gap-2 text-caption text-muted-foreground">
          {proposal.issue_identifier ? <span>{proposal.issue_identifier}</span> : null}
          {from ? <span>{t(($) => $.office.decision_from, { name: from.name })}</span> : null}
          {handoff ? <span>{t(($) => $.office.decision_handoff, { name: handoff.name })}</span> : null}
          {proposal.urgent ? <span className="font-medium text-destructive">{t(($) => $.office.decision_urgent)}</span> : null}
        </div>
        <h3 className="mt-1 text-heading">{proposal.title}</h3>
        {issue?.description ? (
          <div className="mt-3">
            <ReadonlyContent content={issue.description} />
          </div>
        ) : null}
      </div>
      <OfficeProposalDecision
        proposal={proposal}
        handoffName={handoff?.name ?? null}
        canDecide={canDecide}
      />
    </section>
  );
}
