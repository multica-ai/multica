"use client";

import { useQuery } from "@tanstack/react-query";
import { governanceHealthOptions, type HealthReport, type ScheduledJobStatus } from "@multica/core/governance";
import { useWorkspaceId } from "@multica/core/hooks";
import { useT } from "../i18n";
import { useTimeAgo } from "../i18n/use-time-ago";

/**
 * System health (spec 4.9), shown in the Controls tab: what the company's
 * machinery did last, what is stuck, and what is failing. A reading that could
 * not be taken is said so, never shown as healthy.
 */
export function GovernanceHealthSection() {
  const { t } = useT("governance");
  const wsId = useWorkspaceId();
  const health = useQuery(governanceHealthOptions(wsId));
  const timeAgo = useTimeAgo();

  return (
    <section className="flex flex-col gap-3" aria-labelledby="governance-health-heading">
      <h2 id="governance-health-heading" className="text-title-sm font-medium">
        {t(($) => $.health.heading)}
      </h2>

      {health.isError ? (
        <p role="alert" className="text-body text-destructive">
          {t(($) => $.health.unreadable)}
        </p>
      ) : !health.data ? null : (
        <HealthBody report={health.data} timeAgo={timeAgo} />
      )}
    </section>
  );
}

function HealthBody({ report, timeAgo }: { report: HealthReport; timeAgo: (iso: string) => string }) {
  const { t } = useT("governance");
  return (
    <div className="flex flex-col gap-4">
      {report.risks.length === 0 ? (
        <p className="text-body text-muted-foreground">{t(($) => $.health.nothing_at_risk)}</p>
      ) : (
        <ul className="flex list-disc flex-col gap-1 pl-5 text-body" aria-label={t(($) => $.health.risks_label)}>
          {report.risks.map((risk) => (
            <li key={risk}>{risk}</li>
          ))}
        </ul>
      )}

      <div className="flex flex-col gap-2">
        <h3 className="text-body font-medium">{t(($) => $.health.jobs_heading)}</h3>
        {report.jobs.length === 0 ? (
          <p className="text-body text-muted-foreground">{t(($) => $.health.no_jobs)}</p>
        ) : (
          <table className="w-full text-body">
            <thead className="text-left text-caption text-muted-foreground">
              <tr>
                <th scope="col" className="py-1 font-normal">{t(($) => $.health.col_job)}</th>
                <th scope="col" className="py-1 font-normal">{t(($) => $.health.col_last_success)}</th>
                <th scope="col" className="py-1 font-normal">{t(($) => $.health.col_state)}</th>
              </tr>
            </thead>
            <tbody>
              {report.jobs.map((job) => (
                <JobRow key={job.name} job={job} timeAgo={timeAgo} />
              ))}
            </tbody>
          </table>
        )}
      </div>

      <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-1 text-body">
        <dt className="text-muted-foreground">{t(($) => $.health.deliveries_label)}</dt>
        <dd>{t(($) => $.health.deliveries_value, { count: report.delivery_failures })}</dd>
        <dt className="text-muted-foreground">{t(($) => $.health.knowledge_label)}</dt>
        <dd>
          {report.knowledge.offered
            ? t(($) => $.health.knowledge_state[knowledgeStateKey(report.knowledge.state)])
            : t(($) => $.health.knowledge_not_offered)}
        </dd>
        <dt className="text-muted-foreground">{t(($) => $.health.budget_gate_label)}</dt>
        <dd>
          {!report.budget_gate.reported
            ? t(($) => $.health.budget_gate_not_reported)
            : report.budget_gate.cache_age_seconds == null
              ? t(($) => $.health.budget_gate_no_cache)
              : t(($) => $.health.budget_gate_age, { seconds: report.budget_gate.cache_age_seconds })}
        </dd>
      </dl>
    </div>
  );
}

function JobRow({ job, timeAgo }: { job: ScheduledJobStatus; timeAgo: (iso: string) => string }) {
  const { t } = useT("governance");
  return (
    <tr className="border-t" data-job={job.name} data-stuck={job.stuck ? "true" : "false"}>
      <td className="py-1.5">{job.label || job.name}</td>
      <td className="py-1.5 text-muted-foreground">
        {job.last_success_at ? timeAgo(job.last_success_at) : t(($) => $.health.never_succeeded)}
      </td>
      <td className="py-1.5">
        {job.stuck ? (
          <span className="text-destructive" title={job.stuck_reason}>
            {t(($) => $.health.stuck)}
          </span>
        ) : (
          <span className="text-muted-foreground">{t(($) => $.health.ok)}</span>
        )}
      </td>
    </tr>
  );
}

const KNOWN_KNOWLEDGE_STATES = ["ready", "offline", "provisioning", "failed", "disabled"] as const;

function knowledgeStateKey(state: string): (typeof KNOWN_KNOWLEDGE_STATES)[number] | "unknown" {
  return (KNOWN_KNOWLEDGE_STATES as readonly string[]).includes(state)
    ? (state as (typeof KNOWN_KNOWLEDGE_STATES)[number])
    : "unknown";
}
