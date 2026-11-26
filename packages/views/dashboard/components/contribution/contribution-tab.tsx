"use client";

import { useState } from "react";
import { BarChart3 } from "@multica/ui/icons";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  contributionSummaryOptions,
  type ContributionScope,
  type DashboardHistoryLimitParam,
} from "@multica/core/dashboard";
import { CollectionPageState } from "../../../layout/collection-page";
import { useT } from "../../../i18n";
import { Segmented, dimsForDays, type TimeRange } from "../dashboard-shared";
import { ContributionMembers } from "./contribution-members";
import { ContributionOverview } from "./contribution-overview";

type ContributionView = "overview" | "members";

/**
 * The Contribution tab of the workspace dashboard: who created, discussed and
 * delivered which issues, with people and agents ranked apart. It shares the
 * page's time range, project filter, time zone and history limit with Usage
 * and Errors.
 */
export function ContributionTab({
  days,
  projectId,
  tz,
  historyLimit,
}: {
  days: TimeRange;
  projectId: string | null;
  tz: string;
  historyLimit: DashboardHistoryLimitParam;
}) {
  const { t } = useT("usage");
  const wsId = useWorkspaceId();
  const [view, setView] = useState<ContributionView>("overview");
  const scope: ContributionScope = { days, projectId, tz, historyLimit };
  const { data: summary } = useQuery(contributionSummaryOptions(wsId, scope));

  let body: React.ReactNode;
  if (!summary) {
    body = (
      <div className="space-y-5">
        <Skeleton className="h-28 rounded-lg" />
        <Skeleton className="h-56 rounded-lg" />
        <Skeleton className="h-48 rounded-lg" />
      </div>
    );
  } else if (summary.kpis.total === 0 && summary.kpis.touched === 0) {
    body = (
      <CollectionPageState
        icon={BarChart3}
        title={t(($) => $.contribution.empty.title)}
        description={t(($) => $.contribution.empty.body)}
      />
    );
  } else if (view === "members") {
    body = <ContributionMembers summary={summary} scope={scope} />;
  } else {
    body = <ContributionOverview summary={summary} allowedDims={dimsForDays(days)} scope={scope} />;
  }

  return (
    <div className="space-y-5">
      <div>
        <Segmented
          label={t(($) => $.contribution.views.label)}
          value={view}
          onChange={setView}
          options={[
            { label: t(($) => $.contribution.views.overview), value: "overview" },
            { label: t(($) => $.contribution.views.members), value: "members" },
          ]}
        />
      </div>
      {body}
    </div>
  );
}
