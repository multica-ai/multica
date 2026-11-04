"use client";

import { ArrowRight, CheckCircle2, CircleDot } from "@multica/ui/icons";
import type { BaselineRevision } from "@multica/core/api";
import { paths } from "@multica/core/paths";
import type { Issue, TimelineEntry } from "@multica/core/types";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";

export interface EngineeringDecisionTimeline {
  comment: TimelineEntry | null;
  loading: boolean;
  error: boolean;
}

interface EngineeringDecisionsProps {
  workspaceSlug: string;
  issues: readonly Issue[];
  timelines: Readonly<Record<string, EngineeringDecisionTimeline>>;
  candidates: readonly BaselineRevision[];
  reviewStage: number | null;
  onOpenCandidate: (candidateId: string) => void;
  onReview: () => void;
}

export function EngineeringDecisions({ workspaceSlug, issues, timelines, candidates, reviewStage, onOpenCandidate, onReview }: EngineeringDecisionsProps) {
  const { t } = useT("engineering");
  const count = issues.length + candidates.length + (reviewStage === null ? 0 : 1);
  return (
    <section className="border-t border-foreground pt-2" aria-label={t(($) => $.decisions.title)} data-engineering-region="decisions">
      <h2 className="text-caption font-medium uppercase tracking-wide text-muted-foreground">{t(($) => $.decisions.title)}{count ? ` · ${count}` : ""}</h2>
      {count === 0 ? <p className="border-b py-3 text-body text-muted-foreground">{t(($) => $.decisions.emptyTitle)}</p> : (
        <div>
          {issues.map((issue) => {
            const timeline = timelines[issue.id];
            return (
              <AppLink key={issue.id} href={paths.workspace(workspaceSlug).issueDetail(issue.id)} className="group grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 border-b py-3" data-decision-issue={issue.identifier}>
                <CircleDot className="size-3 text-destructive" aria-hidden="true" />
                <span className="min-w-0">
                  <span className="block truncate font-medium">{issue.title}</span>
                  <span className="block text-caption text-muted-foreground">
                    {issue.identifier} · {timeline?.loading ? t(($) => $.decisions.loading) : timeline?.error ? t(($) => $.errors.timeline) : t(($) => $.decisions.blockedHint)}
                  </span>
                </span>
                <ArrowRight className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-foreground" aria-hidden="true" />
              </AppLink>
            );
          })}
          {candidates.map((candidate) => (
            <button key={candidate.id} type="button" className="group grid w-full grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 border-b py-3 text-left" onClick={() => onOpenCandidate(candidate.id)} data-candidate-todo={candidate.id}>
              <CircleDot className="size-3 text-destructive" aria-hidden="true" />
              <span className="min-w-0">
                <span className="block truncate font-medium">{t(($) => $.decisions.baselinePending, { number: candidate.revisionNumber })}</span>
                <span className="block truncate text-caption text-muted-foreground">{candidate.changeSummary || t(($) => $.recent.noSummary)}</span>
              </span>
              <ArrowRight className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-foreground" aria-hidden="true" />
            </button>
          ))}
          {reviewStage !== null ? (
            <button type="button" className="group grid w-full grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 border-b py-3 text-left" onClick={onReview} data-review-todo>
              <CheckCircle2 className="size-3.5 text-success" aria-hidden="true" />
              <span className="min-w-0">
                <span className="block font-medium">{t(($) => $.decisions.reviewPending, { stage: reviewStage })}</span>
                <span className="block text-caption text-muted-foreground">{t(($) => $.decisions.reviewHint)}</span>
              </span>
              <ArrowRight className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-foreground" aria-hidden="true" />
            </button>
          ) : null}
        </div>
      )}
    </section>
  );
}
