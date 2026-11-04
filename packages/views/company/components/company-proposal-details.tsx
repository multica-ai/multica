"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  isProposalSnoozed,
  opportunityPayloadOf,
  proposalRevisionsOptions,
  stageGatePayloadOf,
  type Proposal,
  type ProposalKind,
  type ProposalRelated,
  type ProposalState,
} from "@multica/core/company";
import { initiativeDetailOptions } from "@multica/core/pipeline";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { ChevronDown, ChevronRight } from "@multica/ui/icons";
import { ReadonlyContent } from "../../editor/readonly-content";
import { useT, useTimeAgo } from "../../i18n";
import { AppLink } from "../../navigation";
import { humanizeKey } from "../../pipeline/key-label";
import { OrgChangeSummary } from "./company-org-change-summary";

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
export function CompanyProposalStatus({ proposal, squadName }: { proposal: Proposal; squadName?: string | null }) {
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
          <span>{t(($) => $.company.decision_revising_by, { name: squadName })}</span>
        ) : null}
        {snoozed && proposal.snoozed_until ? (
          <span>{t(($) => $.company.decision_snoozed_until, { date: formatSnoozeDate(proposal.snoozed_until) })}</span>
        ) : null}
      </div>
      {proposal.state === "revising" && proposal.decision_note ? (
        <div className="flex flex-col gap-0.5">
          <span className="text-caption text-muted-foreground">{t(($) => $.company.decision_changes_asked)}</span>
          <p className="whitespace-pre-line text-body">{proposal.decision_note}</p>
        </div>
      ) : null}
    </div>
  );
}

/**
 * The kind of a proposal, as a small label. A plain decision has none: it is
 * the question the queue has always asked. An unknown kind reads as "Other".
 */
export function CompanyProposalKindLabel({ kind }: { kind: ProposalKind }) {
  const { t } = useT("pipeline");
  if (kind === "decision") return null;
  return <span className="text-caption font-medium text-muted-foreground">{t(($) => $.kind.label[kind])}</span>;
}

/**
 * What a kind asks the boss, beyond its title: an opportunity's thesis and
 * evidence, a stage gate's artifact checklist and the leader's attestation, an
 * organisation change's diff. A decision, or a kind this client does not know,
 * shows nothing extra.
 */
export function CompanyProposalKindSummary({ proposal }: { proposal: Proposal }) {
  switch (proposal.kind) {
    case "opportunity":
      return <OpportunitySummary proposal={proposal} />;
    case "stage_gate":
      return <StageGateSummary proposal={proposal} />;
    case "org_change":
      return <OrgChangeSummary proposal={proposal} />;
    default:
      return null;
  }
}

function OpportunitySummary({ proposal }: { proposal: Proposal }) {
  const { t } = useT("pipeline");
  const paths = useWorkspacePaths();
  const payload = opportunityPayloadOf(proposal);
  return (
    <div className="flex flex-col gap-3">
      {payload.thesis ? (
        <section className="flex flex-col gap-1">
          <h4 className="text-caption font-medium text-muted-foreground">{t(($) => $.opportunity.thesis)}</h4>
          <p className="whitespace-pre-line text-body">{payload.thesis}</p>
        </section>
      ) : null}
      {payload.market ? (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.opportunity.market)}: <span className="text-foreground">{payload.market}</span>
        </p>
      ) : null}
      <section className="flex flex-col gap-1">
        <h4 className="text-caption font-medium text-muted-foreground">{t(($) => $.opportunity.evidence)}</h4>
        {payload.evidence_doc_ids.length > 0 ? (
          <ul className="flex flex-col gap-1">
            {payload.evidence_doc_ids.map((id, index) => (
              <li key={id}>
                <AppLink href={paths.knowledge()} className="text-body text-primary hover:underline">
                  {t(($) => $.opportunity.evidence_item, { index: index + 1 })}
                </AppLink>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-caption text-muted-foreground">{t(($) => $.opportunity.no_evidence)}</p>
        )}
      </section>
      {payload.target_stage ? (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.opportunity.target_stage, { stage: humanizeKey(payload.target_stage) })}
        </p>
      ) : null}
    </div>
  );
}

function StageGateSummary({ proposal }: { proposal: Proposal }) {
  const { t } = useT("pipeline");
  const wsId = useWorkspaceId();
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const payload = stageGatePayloadOf(proposal);
  const detail = useQuery({
    ...initiativeDetailOptions(wsId, payload.initiative_id),
    enabled: !!wsId && !!payload.initiative_id,
  });

  if (!payload.initiative_id || (detail.isError && !detail.data)) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.stage_gate.unavailable)}</p>;
  }
  if (detail.isPending) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.panel.loading)}</p>;
  }
  const data = detail.data;
  if (!data || !data.initiative.id) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.stage_gate.unavailable)}</p>;
  }

  const checklist = data.artifacts.filter((artifact) => artifact.stage_key === payload.from);
  const attestation = data.events.find(
    (event) => event.kind === "attested" && event.from_stage === payload.from,
  );

  return (
    <div className="flex flex-col gap-3">
      <p className="text-body font-medium">
        {t(($) => $.stage_gate.moving, { from: humanizeKey(payload.from), to: humanizeKey(payload.to) })}
      </p>
      <section className="flex flex-col gap-1">
        <h4 className="text-caption font-medium text-muted-foreground">{t(($) => $.stage_gate.checklist)}</h4>
        {checklist.length > 0 ? (
          <ul className="flex flex-col gap-1 text-body">
            {checklist.map((artifact) => (
              <li key={artifact.kind} className="flex items-baseline justify-between gap-2">
                <span className="min-w-0 truncate">{humanizeKey(artifact.kind)}</span>
                <span className="shrink-0 text-caption text-muted-foreground">
                  {artifact.knowledge_link_id
                    ? t(($) => $.artifact.published)
                    : artifact.waived
                      ? t(($) => $.artifact.waived)
                      : t(($) => $.artifact.missing)}
                </span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-caption text-muted-foreground">{t(($) => $.stage_gate.checklist_empty)}</p>
        )}
      </section>
      <section className="flex flex-col gap-1">
        <h4 className="text-caption font-medium text-muted-foreground">{t(($) => $.stage_gate.attestation)}</h4>
        {attestation ? (
          <>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.stage_gate.attested_by, {
                name: getActorName(attestation.actor_type, attestation.actor_id ?? ""),
                time: timeAgo(attestation.created_at),
              })}
            </p>
            {attestation.note ? <p className="whitespace-pre-line text-body">{attestation.note}</p> : null}
          </>
        ) : (
          <p className="text-caption text-muted-foreground">{t(($) => $.stage_gate.not_attested)}</p>
        )}
      </section>
    </div>
  );
}

/** The related earlier proposals and decisions. Nothing is shown when none are related. */
export function CompanyProposalRelated({ related }: { related: readonly ProposalRelated[] }) {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  const stateLabel = useProposalStateLabel();
  if (related.length === 0) return null;

  return (
    <section className="flex flex-col gap-1">
      <h4 className="text-caption font-medium text-muted-foreground">{t(($) => $.company.decision_related)}</h4>
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
export function CompanyProposalHistory({ proposal }: { proposal: Proposal }) {
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
        {t(($) => $.company.decision_history)}
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

  if (isPending) return <p className="text-caption text-muted-foreground">{t(($) => $.company.decision_history_loading)}</p>;
  if (isError) return <p className="text-caption text-destructive">{t(($) => $.company.decision_history_failed)}</p>;
  if (!revisions || revisions.length === 0) return null;

  return (
    <ol className="flex flex-col gap-3">
      {revisions.map((revision) => (
        <li key={revision.revision} className="flex flex-col gap-1 border-l-2 pl-3">
          <span className="text-caption text-muted-foreground">
            {t(($) => $.company.decision_revision_title, { revision: revision.revision })}
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
        return t(($) => $.company.decision_state_approved);
      case "rejected":
        return t(($) => $.company.decision_state_rejected);
      case "revising":
        return t(($) => $.company.decision_state_revising);
      case "expired":
        return t(($) => $.company.decision_state_expired);
      default:
        return null;
    }
  };
}
