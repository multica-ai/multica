"use client";

import { memo, useCallback, useEffect, useMemo, useState } from "react";
import { Accordion } from "@base-ui/react/accordion";
import { Virtuoso } from "react-virtuoso";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronRight,
  CircleCheck,
  CircleDashed,
  CircleDot,
  TriangleAlert,
  type IconComponent,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { issuePullRequestsOptions, queueDecidingPullRequest } from "@multica/core/github";
import type { Issue, IssuePullRequestState, IssueTableGroupDescriptor, Project } from "@multica/core/types";
import { ListRow, type ChildProgress } from "./list-row";
import { ListLoadMoreFooter } from "./list-load-more-footer";
import { PullRequestInlineSummary } from "./pull-request-list";
import { useIssuePeekActions } from "../surface/peek-context";
import type { IssueGroupBranches, IssueGroupPageState } from "../surface/use-issue-group-branches";
import { VirtuosoSeed, VIRTUOSO_SEED_COUNT } from "../../common/virtuoso-seed";
import { useRestoredScrollRef } from "../../platform";
import { useT } from "../../i18n";

const LIST_ROW_HEIGHT = "var(--issue-row-height)";
const EMPTY_PROGRESS_MAP = new Map<string, ChildProgress>();
const EMPTY_ROWS: Issue[] = [];
const EMPTY_PAGE: IssueGroupPageState = {
  total: 0, loaded: 0, hasMore: false, isLoading: false,
  isFetching: false, isError: false, loadMore: () => {}, retry: () => {},
};

const GROUP_ICON: Record<IssuePullRequestState, { icon: IconComponent; className: string }> = {
  ready: { icon: CircleCheck, className: "text-emerald-600 dark:text-emerald-400" },
  needs_attention: { icon: TriangleAlert, className: "text-amber-600 dark:text-amber-400" },
  checking: { icon: CircleDashed, className: "text-muted-foreground" },
  none: { icon: CircleDot, className: "text-muted-foreground" },
};

function groupState(descriptor: IssueTableGroupDescriptor): IssuePullRequestState {
  return descriptor.value.kind === "pull_request_state" ? descriptor.value.value : "none";
}

/**
 * The Awaiting merge list: issues grouped by whether their open PRs can be
 * merged, in the server's order (ready, needs attention, checking). Rows are
 * read-only here — no drag, no selection — and carry the PR that decides
 * their group.
 */
function PullRequestQueueViewImpl({
  groupBranches,
  childProgressMap = EMPTY_PROGRESS_MAP,
  projectMap,
}: {
  groupBranches: IssueGroupBranches;
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
}) {
  const { t } = useT("issues");
  const peek = useIssuePeekActions();
  const { descriptors, rowsByKey, pagination } = groupBranches;
  const [collapsed, setCollapsed] = useState<string[]>([]);
  const expanded = useMemo(
    () => descriptors.map((descriptor) => descriptor.key).filter((key) => !collapsed.includes(key)),
    [collapsed, descriptors],
  );

  useEffect(() => {
    peek?.publishColumns([expanded.flatMap((key) => (rowsByKey[key] ?? EMPTY_ROWS).map((issue) => issue.id))]);
  }, [expanded, peek, rowsByKey]);
  useEffect(() => () => peek?.publishColumns(null), [peek]);

  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);
  const [rowHeight, setRowHeight] = useState<number>();
  const restoreScrollRef = useRestoredScrollRef("list");
  const attachScroller = useCallback(
    (el: HTMLDivElement | null) => {
      const row = el?.querySelector<HTMLElement>('[data-slot="issue-list-row"]');
      const height = row ? Number.parseFloat(getComputedStyle(row).height) : 0;
      setRowHeight(height > 0 ? height : undefined);
      setScrollEl(el);
      restoreScrollRef(el);
    },
    [restoreScrollRef],
  );

  const label = (state: IssuePullRequestState) =>
    t(($) => $.views.awaiting_merge[`group_${state}`]);

  return (
    <div ref={attachScroller} data-tab-scroll-root="list" className="flex-1 min-h-0 overflow-y-auto p-2 pt-0">
      <Accordion.Root
        multiple
        className="space-y-1"
        value={expanded}
        onValueChange={(value: string[]) =>
          setCollapsed(descriptors.map((descriptor) => descriptor.key).filter((key) => !value.includes(key)))
        }
      >
        {descriptors.map((descriptor) => {
          const state = groupState(descriptor);
          return (
            <QueueGroup
              key={descriptor.key}
              groupKey={descriptor.key}
              state={state}
              label={label(state)}
              issues={rowsByKey[descriptor.key] ?? EMPTY_ROWS}
              page={pagination[descriptor.key] ?? EMPTY_PAGE}
              isExpanded={expanded.includes(descriptor.key)}
              childProgressMap={childProgressMap}
              projectMap={projectMap}
              scrollParent={scrollEl}
              rowHeight={rowHeight}
            />
          );
        })}
      </Accordion.Root>
    </div>
  );
}

function QueueGroup({
  groupKey,
  state,
  label,
  issues,
  page,
  isExpanded,
  childProgressMap,
  projectMap,
  scrollParent,
  rowHeight,
}: {
  groupKey: string;
  state: IssuePullRequestState;
  label: string;
  issues: Issue[];
  page: IssueGroupPageState;
  isExpanded: boolean;
  childProgressMap: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  scrollParent: HTMLElement | null;
  rowHeight: number | undefined;
}) {
  const { icon: Icon, className: iconClassName } = GROUP_ICON[state];
  const listComponents = useMemo(
    // react-virtuoso throws if `components` is ever undefined (MUL-4474).
    () => ({ Footer: () => <QueueGroupFooter page={page} /> }),
    [page],
  );

  const computeItemKey = (_index: number, issue: Issue) => issue.id;
  const itemContent = (_index: number, issue: Issue) => (
    <ListRow
      issue={issue}
      childProgress={childProgressMap.get(issue.id)}
      project={issue.project_id ? projectMap?.get(issue.project_id) : undefined}
      trailing={<QueuePullRequestSummary issueId={issue.id} />}
      selectable={false}
    />
  );

  return (
    <Accordion.Item value={groupKey} data-testid={`pull-request-queue-group-${state}`}>
      <Accordion.Header className="group/header sticky top-0 z-10 flex h-10 items-center rounded-lg bg-muted transition-colors hover:bg-accent">
        <Accordion.Trigger className="group/trigger flex flex-1 items-center gap-2 px-3 h-full text-left outline-none cursor-pointer">
          <ChevronRight className="size-3.5 shrink-0 text-muted-foreground transition-transform group-aria-expanded/trigger:rotate-90" />
          <span className="inline-flex items-center gap-1.5 text-caption font-semibold">
            <Icon className={cn("size-3.5 shrink-0", iconClassName)} />
            {label}
          </span>
          <span className="text-caption text-muted-foreground">{page.total}</span>
        </Accordion.Trigger>
      </Accordion.Header>
      <Accordion.Panel>
        {/* A group's first page loads once its end comes into view, so an
            expanded group with nothing loaded yet still needs that end. */}
        {isExpanded && issues.length === 0
          ? <QueueGroupFooter page={page} />
          : isExpanded &&
            (scrollParent ? (
              <Virtuoso
                customScrollParent={scrollParent}
                data={issues}
                computeItemKey={computeItemKey}
                initialItemCount={Math.min(issues.length, VIRTUOSO_SEED_COUNT)}
                defaultItemHeight={rowHeight}
                increaseViewportBy={{ top: 400, bottom: 400 }}
                components={listComponents}
                itemContent={itemContent}
              />
            ) : (
              <VirtuosoSeed
                data={issues}
                itemContent={itemContent}
                computeItemKey={computeItemKey}
                estimatedItemHeight={LIST_ROW_HEIGHT}
              />
            ))}
      </Accordion.Panel>
    </Accordion.Item>
  );
}

function QueueGroupFooter({ page }: { page: IssueGroupPageState }) {
  return (
    <ListLoadMoreFooter
      hasMore={page.hasMore}
      isLoading={page.isLoading || page.isFetching}
      total={page.total}
      onLoadMore={page.loadMore}
      isError={page.isError}
      onRetry={page.retry}
    />
  );
}

/**
 * The open PR that put the row in its group, from the issue's own PR list —
 * the same cache the issue page and its realtime updates keep fresh.
 */
function QueuePullRequestSummary({ issueId }: { issueId: string }) {
  const { data } = useQuery(issuePullRequestsOptions(issueId));
  const prs = data?.pull_requests ?? [];
  const deciding = queueDecidingPullRequest(prs);
  if (!deciding) return null;
  const others = prs.filter((pr) => pr.state === "open").length - 1;
  return (
    <span
      data-testid="pull-request-queue-summary"
      className="hidden shrink-0 items-center gap-1 text-caption text-muted-foreground sm:inline-flex"
    >
      <PullRequestInlineSummary pr={deciding} />
      {others > 0 && <span className="text-micro tabular-nums">+{others}</span>}
    </span>
  );
}

export const PullRequestQueueView = memo(PullRequestQueueViewImpl);
