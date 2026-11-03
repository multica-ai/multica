"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import {
  knowledgeEvalRunsOptions,
  knowledgeHealthOptions,
  knowledgeKindsOptions,
  useRunKnowledgeEval,
  type KnowledgeBase,
} from "@multica/core/knowledge";
import { useT } from "../i18n";
import { KnowledgePolicyList } from "./knowledge-policy-list";

/**
 * The governance tab, for owners and admins: who publishes what (the policy),
 * how the search is doing (the latest eval run), and what the Librarian's
 * routines last found.
 */
export function KnowledgeGovernance({
  wsId,
  knowledgeBases,
}: {
  wsId: string;
  knowledgeBases: KnowledgeBase[];
}) {
  const { t } = useT("knowledge");
  const kinds = useQuery(knowledgeKindsOptions(wsId));
  const runs = useQuery(knowledgeEvalRunsOptions(wsId));
  const health = useQuery(knowledgeHealthOptions(wsId));
  const runEval = useRunKnowledgeEval(wsId);

  /** The Librarian's routine names; an unknown routine shows its own key. */
  const routineName = (job: string): string => {
    switch (job) {
      case "triage":
        return t(($) => $.taxonomy.routines.triage);
      case "freshness":
        return t(($) => $.taxonomy.routines.freshness);
      case "audit":
        return t(($) => $.taxonomy.routines.audit);
      case "eval":
        return t(($) => $.taxonomy.routines.eval);
      case "taxonomy_backfill":
        return t(($) => $.taxonomy.routines.backfill);
      default:
        return job;
    }
  };

  const countLabel = (key: string): string => {
    switch (key) {
      case "review_due":
        return t(($) => $.taxonomy.routines.counts.review_due);
      case "duplicate":
        return t(($) => $.taxonomy.routines.counts.duplicate);
      case "missing_sections":
        return t(($) => $.taxonomy.routines.counts.missing_sections);
      case "missing_kind":
        return t(($) => $.taxonomy.routines.counts.missing_kind);
      case "orphaned":
        return t(($) => $.taxonomy.routines.counts.orphaned);
      case "repaired":
        return t(($) => $.taxonomy.routines.counts.repaired);
      case "new_drafts":
        return t(($) => $.taxonomy.routines.counts.new_drafts);
      default:
        return key;
    }
  };

  /** The routine's counts as one line, or "nothing to report" when all are zero. */
  const summarize = (counts: Record<string, number>): string => {
    const parts = Object.entries(counts).filter(([, n]) => n > 0);
    if (parts.length === 0) return t(($) => $.taxonomy.routines.nothing);
    return parts
      .map(([key, n]) => t(($) => $.taxonomy.routines.count, { label: countLabel(key), count: n }))
      .join(" · ");
  };

  const latest = runs.data?.runs[0];
  const runNow = () =>
    runEval.mutate(undefined, {
      onSuccess: (run) => {
        if (run.state === "offline") {
          toast.error(t(($) => $.taxonomy.retrieval.offline));
          return;
        }
        if (run.total === 0) return;
        toast.success(
          t(($) => $.taxonomy.retrieval.ran, { rate: Math.round(run.hit_rate * 100) }),
        );
      },
      onError: () => toast.error(t(($) => $.taxonomy.retrieval.run_failed)),
    });

  return (
    <div className="flex flex-col gap-8">
      <KnowledgePolicyList wsId={wsId} knowledgeBases={knowledgeBases} kinds={kinds.data?.kinds ?? []} />

      <section className="flex flex-col gap-3" aria-labelledby="knowledge-retrieval-title">
        <div className="flex min-w-0 items-center justify-between gap-2">
          <h2 id="knowledge-retrieval-title" className="text-body font-medium">
            {t(($) => $.taxonomy.retrieval.title)}
          </h2>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={runNow}
            disabled={runEval.isPending}
            aria-busy={runEval.isPending}
          >
            {runEval.isPending ? <Spinner /> : null}
            {t(($) => $.taxonomy.retrieval.run)}
          </Button>
        </div>
        {latest ? (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.taxonomy.retrieval.last_run, {
              rate: Math.round(latest.hit_rate * 100),
              hits: latest.hits,
              total: latest.total,
            })}
          </p>
        ) : (
          <p className="text-caption text-muted-foreground">{t(($) => $.taxonomy.retrieval.no_runs)}</p>
        )}
      </section>

      <section className="flex flex-col gap-3" aria-labelledby="knowledge-routines-title">
        <h2 id="knowledge-routines-title" className="text-body font-medium">
          {t(($) => $.taxonomy.routines.title)}
        </h2>
        {health.isError ? (
          <p role="alert" className="text-caption text-destructive">
            {t(($) => $.taxonomy.routines.load_error)}
          </p>
        ) : health.data && health.data.jobs.length > 0 ? (
          <ul className="flex flex-col divide-y rounded-lg border">
            {health.data.jobs.map((job) => (
              <li key={job.job} className="flex flex-wrap items-baseline justify-between gap-2 px-4 py-2.5">
                <span className="text-body">{routineName(job.job)}</span>
                <span className="text-caption text-muted-foreground">
                  {summarize(job.report.counts)}
                </span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-caption text-muted-foreground">{t(($) => $.taxonomy.routines.empty)}</p>
        )}
      </section>
    </div>
  );
}
