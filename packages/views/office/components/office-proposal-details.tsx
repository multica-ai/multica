"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  isProposalSnoozed,
  proposalRevisionsOptions,
  type Proposal,
  type ProposalRelated,
  type ProposalState,
} from "@multica/core/office";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { ChevronDown, ChevronRight } from "@multica/ui/icons";
import { ReadonlyContent } from "../../editor/readonly-content";
import { useT, useTimeAgo } from "../../i18n";
import { AppLink } from "../../navigation";


/** A snooze date as the server stores it (midnight UTC), shown on the UTC day it names. */
export function formatSnoozeDate(iso: string): string {
  const date = new Date(iso);
  return Number.isNaN(date.getTime())
    ? ""
    : date.toLocaleDateString(undefined, { month: "short", day: "numeric", timeZone: "UTC" });
}

/**
 * A proposal's state in words: Revising with the squad that has it, the note the
 * boss sent it back with, Expired, Decide later until a date, and the outcome of
 * a settled proposal. Pending and unknown states show nothing extra.
 */
export function OfficeProposalStatus({ proposal, squadName }: { proposal: Proposal; squadName?: string | null }) {
  const { t } = useT("issues");
  const stateLabel = useProposalStateLabel();
  const snoozed = proposal.state === "pending" && isProposalSnoozed(proposal, Date.now());
  const label = stateLabel(proposal.state);

  if (!label && !snoozed) return null;

  return (
    <div className="flex flex-col gap-1">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-caption text-muted-foreground">
        {label ? <span className="font-medium text-foreground">{label}</span> : null}
        {proposal.state === "revising" && squadName ? (
          <span>{t(($) => $.office.decision_revising_by, { name: squadName })}</span>
        ) : null}
        {snoozed && proposal.snoozed_until ? (
          <span>{t(($) => $.office.decision_snoozed_until, { date: formatSnoozeDate(proposal.snoozed_until) })}</span>
        ) : null}
      </div>
      {proposal.state === "revising" && proposal.decision_note ? (
        <div className="flex flex-col gap-0.5">
          <span className="text-caption text-muted-foreground">{t(($) => $.office.decision_changes_asked)}</span>
          <p className="whitespace-pre-line text-body">{proposal.decision_note}</p>
        </div>
      ) : null}
    </div>
  );
}

/** The related earlier proposals and decisions. Nothing is shown when none are related. */
export function OfficeProposalRelated({ related }: { related: readonly ProposalRelated[] }) {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  const stateLabel = useProposalStateLabel();
  if (related.length === 0) return null;

  return (
    <section className="flex flex-col gap-1">
      <h4 className="text-caption font-medium text-muted-foreground">{t(($) => $.office.decision_related)}</h4>
      <ul className="flex flex-col gap-1">
        {related.map((entry, index) => {
          const label = entry.kind === "proposal" && entry.state ? stateLabel(entry.state) : null;
          return (
            <li key={`${entry.kind}:${entry.id}:${index}`} className="flex min-w-0 items-center gap-2 text-body">
              {entry.issue_identifier ? (
                <AppLink
                  href={paths.issueDetail(entry.issue_identifier)}
                  className="shrink-0 text-caption text-muted-foreground hover:text-foreground hover:underline"
                >
                  {entry.issue_identifier}
                </AppLink>
              ) : null}
              <span className="min-w-0 flex-1 truncate">{entry.title}</span>
              {label ? <span className="shrink-0 text-caption text-muted-foreground">{label}</span> : null}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

/** The revision history behind a disclosure. The revisions are fetched only once it is opened. */
export function OfficeProposalHistory({ proposal }: { proposal: Proposal }) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);

  return (
    <section className="flex flex-col gap-2">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className="flex w-fit items-center gap-1 text-caption font-medium text-muted-foreground hover:text-foreground"
      >
        {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
        {t(($) => $.office.decision_history)}
      </button>
      {open ? <RevisionList proposalId={proposal.id} /> : null}
    </section>
  );
}

function RevisionList({ proposalId }: { proposalId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const { data: revisions, isPending, isError } = useQuery(proposalRevisionsOptions(wsId, proposalId));

  if (isPending) return <p className="text-caption text-muted-foreground">{t(($) => $.office.decision_history_loading)}</p>;
  if (isError) return <p className="text-caption text-destructive">{t(($) => $.office.decision_history_failed)}</p>;
  if (!revisions || revisions.length === 0) return null;

  return (
    <ol className="flex flex-col gap-3">
      {revisions.map((revision) => (
        <li key={revision.revision} className="flex flex-col gap-1 border-l-2 pl-3">
          <span className="text-caption text-muted-foreground">
            {t(($) => $.office.decision_revision_title, { revision: revision.revision })}
            {" · "}
            {getActorName(revision.created_by_type, revision.created_by_id)}
            {" · "}
            {timeAgo(revision.created_at)}
          </span>
          <ReadonlyContent content={revision.body} />
        </li>
      ))}
    </ol>
  );
}

/** The status wording for a state, or null for pending (which has none) and unknown states. */
function useProposalStateLabel(): (state: ProposalState) => string | null {
  const { t } = useT("issues");
  return (state) => {
    switch (state) {
      case "approved":
        return t(($) => $.office.decision_state_approved);
      case "rejected":
        return t(($) => $.office.decision_state_rejected);
      case "revising":
        return t(($) => $.office.decision_state_revising);
      case "expired":
        return t(($) => $.office.decision_state_expired);
      default:
        return null;
    }
  };
}
