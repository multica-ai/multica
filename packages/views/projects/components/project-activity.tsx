"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import type { ProjectActivityEntry } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueRouteRef, useWorkspacePaths } from "@multica/core/paths";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { projectActivityOptions } from "@multica/core/projects/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink } from "../../navigation";
import { formatActivity } from "../../issues/components/activity-text";
import { StatusIcon } from "../../issues/components/status-icon";
import { useStatusLabel } from "../../issues/utils/status-label";
import { useLocale, useT, useTimeAgo } from "../../i18n";

function useProjectActivity(projectId: string) {
  const wsId = useWorkspaceId();
  return useInfiniteQuery(projectActivityOptions(wsId, projectId));
}

/** What changed on this project's issues, newest first. */
export function ProjectActivityFeed({
  projectId,
  limit,
  dense = false,
}: {
  projectId: string;
  /** Show only the newest entries, without paging (the sidebar's digest). */
  limit?: number;
  dense?: boolean;
}) {
  const { t } = useT("projects");
  const { data, isPending, hasNextPage, fetchNextPage, isFetchingNextPage } = useProjectActivity(projectId);

  if (isPending) {
    return (
      <div className="space-y-2">
        {Array.from({ length: limit ?? 4 }, (_, i) => (
          <Skeleton key={i} className="h-8 w-full" />
        ))}
      </div>
    );
  }

  const all = data?.pages.flatMap((page) => page.entries) ?? [];
  const entries = limit ? all.slice(0, limit) : all;
  if (entries.length === 0) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.detail.activity.empty)}</p>;
  }

  return (
    <div>
      <ol className={cn("flex flex-col", dense ? "gap-2.5" : "gap-4")} data-testid="project-activity">
        {entries.map((entry) => (
          <ActivityRow key={entry.id} entry={entry} dense={dense} />
        ))}
      </ol>
      {!limit && hasNextPage && (
        <Button
          variant="ghost"
          size="sm"
          className="mt-4 text-muted-foreground"
          disabled={isFetchingNextPage}
          onClick={() => void fetchNextPage()}
        >
          {t(($) => $.detail.activity.load_more)}
        </Button>
      )}
    </div>
  );
}

function ActivityRow({ entry, dense }: { entry: ProjectActivityEntry; dense: boolean }) {
  const { t } = useT("issues");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const resolveStatusLabel = useStatusLabel(wsId);
  const statuses = useIssueStatuses(wsId);
  const actor = getActorName(entry.actor_type, entry.actor_id);
  const text = formatActivity(entry, t, locale, getActorName, resolveStatusLabel);

  return (
    <li className="flex gap-2.5">
      <ActorAvatar actorType={entry.actor_type} actorId={entry.actor_id} size="sm" />
      <div className="min-w-0 flex-1">
        <p className={cn("text-muted-foreground", dense ? "text-caption" : "text-body")}>
          <span className="font-medium text-foreground">{actor}</span> {text}
        </p>
        <div className="mt-0.5 flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground">
          <AppLink
            href={paths.issueDetail(issueRouteRef({ id: entry.issue_id, identifier: entry.issue_identifier }))}
            className="flex min-w-0 items-center gap-1.5 hover:text-foreground"
          >
            <StatusIcon
              status={entry.issue_status}
              category={statuses.categoryOf(entry.issue_status)}
              color={statuses.colorOf(entry.issue_status)}
              icon={statuses.iconOf(entry.issue_status)}
              className="size-3.5 shrink-0"
            />
            <span className="shrink-0 tabular-nums">{entry.issue_identifier}</span>
            <span className="min-w-0 truncate">{entry.issue_title}</span>
          </AppLink>
          <span className="ml-auto shrink-0 tabular-nums" title={new Date(entry.created_at).toLocaleString(locale)}>
            {timeAgo(entry.created_at)}
          </span>
        </div>
      </div>
    </li>
  );
}

/** The Activity tab: the project's full issue history as one readable column. */
export function ProjectActivityPage({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-3xl px-8 pb-16 pt-8">
        <h2 className="text-label font-medium">{t(($) => $.detail.tab_activity)}</h2>
        <p className="mb-6 mt-1 text-caption text-muted-foreground">{t(($) => $.detail.activity.hint)}</p>
        <ProjectActivityFeed projectId={projectId} />
      </div>
    </div>
  );
}
