"use client";

import { useQuery } from "@tanstack/react-query";
import type { InboxCreatingRow, InboxWorkingRow } from "@multica/core/inbox/sections";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { ActorAvatar } from "../../common/actor-avatar";
import { handleRowActivationKey } from "../../common/row-actions-menu";
import { useT } from "../../i18n";
import { resolveClickIntent, useIntentNavigate } from "../../navigation";
import { useTimeAgo } from "./inbox-list-item";

/**
 * An issue an agent is working on for the user. One quiet line: nothing here
 * is waiting on them, it only shows the hand-over is still in flight.
 *
 * The run carries no issue title, so the row reads the issue itself. That is
 * the same cache entry the detail pane fills when the row is opened.
 */
export function InboxWorkingItem({
  row,
  isSelected,
  onClick,
}: {
  row: InboxWorkingRow;
  isSelected: boolean;
  onClick: () => void;
}) {
  const wsId = useWorkspaceId();
  const slug = useWorkspaceSlug();
  const { getActorName } = useActorName();
  const intentNavigate = useIntentNavigate();
  const issue = useQuery(issueDetailOptions(wsId, row.issueId));
  const issueHref = slug ? paths.workspace(slug).issueDetail(row.issueId) : null;

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={(e) => {
        if (issueHref) {
          const intent = resolveClickIntent(e);
          if (intent !== "push") {
            intentNavigate(issueHref, intent);
            return;
          }
        }
        onClick();
      }}
      onKeyDown={(e) => handleRowActivationKey(e, onClick)}
      onAuxClick={(e) => {
        if (e.defaultPrevented || e.button !== 1 || !issueHref) return;
        e.preventDefault();
        intentNavigate(issueHref, "background-tab");
      }}
      className={`flex w-full cursor-default select-none items-center gap-2 rounded-md px-2 py-2 text-left outline-none transition-colors focus-visible:ring-1 focus-visible:ring-ring ${
        isSelected ? "bg-accent" : "hover:bg-accent/50"
      }`}
    >
      <ActorAvatar actorType="agent" actorId={row.agentId} size="sm" enableHoverCard />
      <span className="min-w-0 flex-1 truncate text-body text-muted-foreground">
        {issue.data ? (
          issue.data.title
        ) : issue.isPending ? (
          <Skeleton className="h-3.5 w-2/3" />
        ) : (
          // The issue cannot be read (deleted, or out of the viewer's reach):
          // the agent's name still says what the row is.
          getActorName("agent", row.agentId)
        )}
      </span>
      <WorkingState queued={row.queued} since={row.since} />
    </div>
  );
}

/**
 * A "Create with agent" whose issue is not filed yet. There is nothing to open
 * until it is, so the row only says the request is on its way; the issue's own
 * row replaces it once the agent starts on it.
 */
export function InboxCreatingItem({ row }: { row: InboxCreatingRow }) {
  const { t } = useT("inbox");
  return (
    <div className="flex w-full cursor-default select-none items-center gap-2 rounded-md px-2 py-2">
      <ActorAvatar actorType="agent" actorId={row.agentId} size="sm" enableHoverCard />
      <span className="min-w-0 flex-1 truncate text-body text-muted-foreground">
        {t(($) => $.sections.creating_issue)}
      </span>
      <WorkingState queued={row.queued} since={row.since} />
    </div>
  );
}

function WorkingState({ queued, since }: { queued: boolean; since: string }) {
  const { t } = useT("inbox");
  const timeAgo = useTimeAgo();
  if (queued) {
    return <span className="shrink-0 text-caption text-muted-foreground">{t(($) => $.sections.queued)}</span>;
  }
  return (
    <span className="flex shrink-0 items-center gap-1.5 text-caption text-muted-foreground">
      <span aria-hidden className="size-1.5 rounded-full bg-success" />
      {timeAgo(since)}
    </span>
  );
}
