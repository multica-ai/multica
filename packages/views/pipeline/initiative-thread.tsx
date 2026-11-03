"use client";

import { useT } from "../i18n";
import { IssueDetail } from "../issues/components/issue-detail";

/**
 * The backing issue's thread: its activity and comments, shown with the issue's
 * peek view, which the board's side peek and the Office agent panel already use.
 * The comment list and composer are IssueDetail's own; nothing is re-implemented
 * here. IssueDetail's properties sidebar is left out, as the peek has none.
 */
export function InitiativeThread({ issueId }: { issueId: string }) {
  const { t } = useT("pipeline");
  return (
    <section aria-labelledby="initiative-thread" className="flex flex-col gap-2">
      <h3 id="initiative-thread" className="text-caption font-medium text-muted-foreground">
        {t(($) => $.panel.thread)}
      </h3>
      <div className="flex h-[32rem] min-h-0 flex-col overflow-hidden rounded-lg border">
        <IssueDetail key={issueId} issueId={issueId} variant="peek" defaultSidebarOpen={false} />
      </div>
    </section>
  );
}
