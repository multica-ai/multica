"use client";

import { AgentDetailPage } from "../../agents/components/agent-detail-page";
import { useT } from "../../i18n";
import { CompanyIssueList } from "./company-issue-list";

/** One agent in the company hub: its full detail, then the work it holds. */
export function CompanyAgentDetail({
  wsId,
  agentId,
  onArchived,
}: {
  wsId: string;
  agentId: string;
  onArchived: () => void;
}) {
  const { t } = useT("company");
  return (
    <div className="flex flex-col gap-6">
      <AgentDetailPage agentId={agentId} onArchived={onArchived} />
      <section aria-label={t(($) => $.work)} className="flex flex-col gap-2">
        <h3 className="text-sm font-medium">{t(($) => $.work)}</h3>
        <CompanyIssueList wsId={wsId} assignee={{ type: "agent", id: agentId }} />
      </section>
    </div>
  );
}
