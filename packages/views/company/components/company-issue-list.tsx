"use client";

import { useQuery } from "@tanstack/react-query";
import { issuesByAssigneeOptions } from "@multica/core/issues/queries";
import { issueColumnCategory } from "@multica/core/issues";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useIssuePeekActions } from "../../issues/surface/peek-context";
import { StatusIcon } from "../../issues/components/status-icon";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { useT } from "../../i18n";
import {
  COMPANY_ISSUE_GROUPS,
  groupCompanyIssues,
  type CompanyIssueGroup,
} from "../lib/company-issue-groups";

/**
 * The issues an agent or squad is assigned to, in the three lists the hub
 * shows. A row opens the issue in the peek panel, so the hub stays put.
 */
export function CompanyIssueList({
  wsId,
  assignee,
}: {
  wsId: string;
  assignee: { type: "squad" | "agent"; id: string };
}) {
  const { t } = useT("company");
  const groupLabel = (group: CompanyIssueGroup) => {
    if (group === "active") return t(($) => $.issues_active);
    if (group === "planned") return t(($) => $.issues_planned);
    return t(($) => $.issues_done);
  };
  const peek = useIssuePeekActions();
  const { data, isPending } = useQuery(issuesByAssigneeOptions(wsId, assignee));

  if (isPending) return <Skeleton className="h-24 w-full" />;

  const issues = data?.issues ?? [];
  if (issues.length === 0) {
    return <p className="text-sm text-muted-foreground">{t(($) => $.issues_empty)}</p>;
  }
  const groups = groupCompanyIssues(issues);
  const total = data?.total ?? issues.length;

  return (
    <div className="flex flex-col gap-5">
      {COMPANY_ISSUE_GROUPS.map((group) =>
        groups[group].length === 0 ? null : (
          <section key={group} aria-label={groupLabel(group)} className="flex flex-col gap-1">
            <h3 className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
              {groupLabel(group)}
              <span className="tabular-nums">{groups[group].length}</span>
            </h3>
            <ul className="flex flex-col">
              {groups[group].map((issue) => (
                <li key={issue.id}>
                  <button
                    type="button"
                    onClick={() => peek?.open(issue.id)}
                    className="flex w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent/60"
                  >
                    <StatusIcon
                      status={issue.status}
                      category={issueColumnCategory(issue)}
                      className="h-3.5 w-3.5 shrink-0"
                    />
                    <span className="shrink-0 font-mono text-xs text-muted-foreground">{issue.identifier}</span>
                    <span className="min-w-0 flex-1 truncate">{issue.title}</span>
                    <PriorityIcon priority={issue.priority} className="h-3.5 w-3.5 shrink-0" />
                  </button>
                </li>
              ))}
            </ul>
          </section>
        ),
      )}
      {total > issues.length ? (
        <p className="text-xs text-muted-foreground">
          {t(($) => $.issues_truncated, { shown: issues.length, total })}
        </p>
      ) : null}
    </div>
  );
}
