"use client";

import { useQuery } from "@tanstack/react-query";
import {
  reviewsOptions,
  type Review,
  type ReviewRecommendation,
} from "@multica/core/governance";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { PAGE_GUTTER } from "../layout/page-header";
import { useT } from "../i18n";
import { AppLink } from "../navigation";

const KNOWN_CHANGES = [
  "instruction_change",
  "skill_change",
  "charter_change",
  "autonomy_change",
  "hire",
  "retire",
  "move",
  "merge_squads",
  "unretire",
  "unmerge",
  "observe",
] as const;

type KnownChange = (typeof KNOWN_CHANGES)[number];

function knownChange(type: string): KnownChange | null {
  return (KNOWN_CHANGES as readonly string[]).includes(type) ? (type as KnownChange) : null;
}

/**
 * The Reviews tab (spec 4.6): the reviews the caller may read, newest first.
 * Each review shows its rating, the scorecard figures it cites (kept in its
 * narrative by the server), and each recommendation with the proposal filed for
 * it. The server decides who sees what; an empty list is the answer for a caller
 * who sees no review.
 */
export function GovernanceReviewsTab() {
  const { t } = useT("governance");
  const wsId = useWorkspaceId();
  const { getActorName } = useActorName();
  const reviews = useQuery(reviewsOptions(wsId));

  if (reviews.isPending) {
    return (
      <div className={cn(PAGE_GUTTER, "flex flex-col gap-3 py-4")} aria-label={t(($) => $.reviews_tab.loading)}>
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-24 w-full" />
      </div>
    );
  }

  const list = reviews.data?.reviews ?? [];
  if (list.length === 0) {
    return (
      <div className={cn(PAGE_GUTTER, "py-4 text-sm text-muted-foreground")}>
        {t(($) => $.reviews_tab.empty)}
      </div>
    );
  }

  const subjectName = (review: Review) =>
    review.subject_type === "agent" ? getActorName("agent", review.subject_id) : null;

  return (
    <div className={cn(PAGE_GUTTER, "flex flex-col gap-4 py-4")}>
      {list.map((review) => (
        <article key={review.id} className="flex flex-col gap-3 rounded-lg border p-4">
          <header className="flex flex-wrap items-center gap-2">
            <span className="font-medium">
              {subjectName(review) ?? t(($) => $.reviews_tab.subject[review.subject_type])}
            </span>
            <Badge variant="outline">{t(($) => $.reviews_tab.rating[review.rating])}</Badge>
            <span className="text-xs text-muted-foreground">
              {t(($) => $.reviews_tab.period.month, { start: review.period_start })}
            </span>
          </header>
          <p className="whitespace-pre-wrap text-sm">{review.narrative}</p>
          <section className="flex flex-col gap-2">
            <h3 className="text-xs font-medium uppercase text-muted-foreground">
              {t(($) => $.reviews_tab.generic)}
            </h3>
            {review.recommendations.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t(($) => $.reviews_tab.no_recommendations)}</p>
            ) : (
              <ul className="flex flex-col gap-2">
                {review.recommendations.map((rec, index) => (
                  <RecommendationRow key={`${review.id}-${index}`} rec={rec} />
                ))}
              </ul>
            )}
          </section>
        </article>
      ))}
    </div>
  );
}

function RecommendationRow({ rec }: { rec: ReviewRecommendation }) {
  const { t } = useT("governance");
  const paths = useWorkspacePaths();
  const change = knownChange(rec.type);
  const label = change ? t(($) => $.reviews_tab.change[change]) : rec.type;
  let status: string;
  if (rec.proposal_id) {
    status = t(($) => $.reviews_tab.recommendation.filed);
  } else if (rec.refused) {
    status = t(($) => $.reviews_tab.recommendation.refused);
  } else {
    status = t(($) => $.reviews_tab.recommendation.observe);
  }
  return (
    <li className="flex flex-col gap-1 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{label}</span>
        <span className="text-muted-foreground">{status}</span>
        {rec.proposal_id && rec.proposal_issue_id ? (
          <AppLink
            href={paths.issueDetail(rec.proposal_issue_id)}
            className="font-mono text-xs text-primary hover:underline"
          >
            {t(($) => $.reviews_tab.proposal, { id: rec.proposal_id.slice(0, 8) })}
          </AppLink>
        ) : (
          rec.proposal_id && (
            <span className="font-mono text-xs text-muted-foreground">
              {t(($) => $.reviews_tab.proposal, { id: rec.proposal_id.slice(0, 8) })}
            </span>
          )
        )}
      </div>
      <p className="text-muted-foreground">{rec.rationale}</p>
      {rec.refused && <p className="text-xs text-muted-foreground">{rec.refused}</p>}
    </li>
  );
}
