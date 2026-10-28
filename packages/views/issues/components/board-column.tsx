"use client";

import { memo, useCallback, useMemo, useState, type ReactNode } from "react";
import { Virtuoso } from "react-virtuoso";
import {
  Ban,
  ChevronDown,
  ChevronRight,
  EyeOff,
  FolderMinus,
  MoreHorizontal,
  Plus,
  UserMinus,
} from "lucide-react";
import { useDroppable } from "@dnd-kit/core";
import {
  SortableContext,
  verticalListSortingStrategy,
  type SortingStrategy,
} from "@dnd-kit/sortable";
import type {
  Issue,
  IssueAssigneeType,
  IssueStatus,
  Project,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { TruncatedTooltip } from "@multica/ui/components/common/truncated-tooltip";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import { useViewBaseline } from "../surface/view-baseline-context";
import { StatusHeading } from "./status-heading";
import { DraggableBoardCard } from "./board-card";
import type { ChildProgress } from "./list-row";
import { useT } from "../../i18n";
import { ActorAvatar } from "../../common/actor-avatar";
import { ProjectIcon } from "../../projects/components/project-icon";
import { useRestoredScrollOffset, useRestoredScrollRef } from "../../platform";
import { DeferredPopup } from "../../common/deferred-popup";
import { DeferredTooltip } from "../../common/deferred-tooltip";
import { VirtuosoSeed } from "../../common/virtuoso-seed";
import { AppLink } from "../../navigation";
import { useBoardWorkflow } from "./board-workflow-context";
import { ColumnHandoff } from "../../workflows/column-handoff";
import type { IssueCreateDefaults } from "../surface/types";
import {
  buildBoardColumnTree,
  nodeIssueId,
  visibleTreeIssueIds,
  type BoardTreeNode,
  type ParentLookup,
} from "../utils/board-hierarchy";

// Insertion-position prediction intentionally omitted. The server's
// ORDER BY uses PostgreSQL's en_US.utf8 collation (glibc), which
// cannot be faithfully replicated in JavaScript (ICU/V8). Showing an
// inaccurate indicator is worse than showing none.

export const BOARD_COL_WIDTH = 280;
export const BOARD_CARD_WIDTH = BOARD_COL_WIDTH - 16 - 8; // col(280) - col p-2(16) - droppable p-1(8)

// Board cards are ~90-140px tall, so ~10 fill a column viewport — unlike the
// generic VIRTUOSO_SEED_COUNT (30, sized for 36px list rows). The seed mounts
// synchronously per column on every surface remount, so oversizing it
// multiplies straight into tab-switch cost (columns × seed × per-card mount).
const BOARD_SEED_COUNT = 10;

// Median card height (incl. the 8px pt-2 gap) for pre-measurement scroll
// sizing only: the seed's trailing spacer and Virtuoso's defaultItemHeight
// share it so total scroll height — and the scrollbar thumb — stays steady
// across the seed → Virtuoso handoff instead of jumping when the unseeded
// rows suddenly get spaced out. Real measurements refine it afterwards.
const BOARD_CARD_ESTIMATED_HEIGHT = 110;

// Columns at or below this render every card plainly — no Virtuoso, no seed
// handoff, no estimated heights. Scroll height is then always browser-measured
// truth, so the column's scrollbar can never jump on mount, route return, or
// tab restore. Virtualization only pays for itself on large columns (Linear
// does the same per-column split: `data-virtual-cluster="false"` for small
// columns, padding-spacer virtualization for big ones). ~30 ≈ 3 viewports of
// cards; a full plain mount at that size is cheap now that per-card popups
// mount lazily. Known edge: a load-more append that crosses the threshold
// swaps the column to the virtualized path, which may adjust the scrollbar
// once — accepted, since it only happens mid-scroll at the column bottom.
const BOARD_VIRTUALIZE_THRESHOLD = 30;

// Passed to <Virtuoso components> when the column has no footer. Must be a
// STABLE object, never `undefined`: an explicit `undefined` prop overwrites
// react-virtuoso's internal `{}` default and its startup destructure of
// `EmptyPlaceholder`/`Footer` throws (MUL-4474).
const EMPTY_VIRTUOSO_COMPONENTS = {};

export interface BoardColumnGroup {
  id: string;
  title: string;
  /** Status columns carry exact built-in or custom status keys. */
  status?: IssueStatus;
  assigneeType?: IssueAssigneeType | null;
  assigneeId?: string | null;
  /** Project id for this column; null = the "No project" column. Set only
   *  when the board is grouped by project. */
  projectId?: string | null;
  /** Display-only, for the column's leading icon. Null on the "No project"
   *  column and on a project the projects query cannot resolve. */
  project?: Pick<Project, "icon"> | null;
  /** Set when the board is grouped by a select-type custom property. */
  propertyId?: string;
  /** Option id for this column; null = the "No value" column. */
  propertyOptionId?: string | null;
  propertyOptionColor?: string;
  totalCount?: number;
  createData?: IssueCreateDefaults;
}

/** Sub-issue nesting for a board column; omitted when nesting is off. */
export interface BoardColumnHierarchy {
  collapsed: ReadonlySet<string>;
  onToggle: (issueId: string) => void;
  /** Resolves a parent that sits outside this column, for its ghost header. */
  lookupParent: ParentLookup;
}

// Nested cards are not one vertical list, so list-shift animations would move
// the wrong cards. With nesting on, cards stay put while dragging; the drop
// target still resolves normally.
const noShiftStrategy: SortingStrategy = () => null;

// Past this depth sub-issues stop indenting so a 280px column stays readable.
const MAX_INDENT_DEPTH = 3;

export const BoardColumn = memo(function BoardColumn({
  group,
  issueIds,
  issueMap,
  childProgressMap,
  projectMap,
  totalCount,
  footer,
  projectId,
  onCreateIssue,
  sortLabel,
  hierarchy,
}: {
  group: BoardColumnGroup;
  issueIds: string[];
  issueMap: Map<string, Issue>;
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  totalCount?: number;
  footer?: ReactNode;
  /** When set, the per-column "+" pre-fills the project on the create form. */
  projectId?: string;
  onCreateIssue?: (defaults: IssueCreateDefaults) => void;
  sortLabel?: string | null;
  hierarchy?: BoardColumnHierarchy;
}) {
  const status = group.status;
  const wsId = useWorkspaceId();
  const { entryOf } = useIssueStatuses(wsId);
  const archived = !!status && !!entryOf(status)?.archived_at;
  const { setNodeRef, isOver: droppableIsOver } = useDroppable({ id: group.id });
  // A dragged card whose project workflow does not list this status cannot
  // land here; the column dims and says why. (MUL-7420)
  const { dropBlockedReason, dropHint } = useBoardWorkflow();
  const blockedReason = status ? dropBlockedReason(status) : null;
  const isOver = droppableIsOver && !archived && !blockedReason;
  const hint = isOver && status ? dropHint(status) : null;
  const viewStoreApi = useViewStoreApi();
  // A status fixed by the open saved view cannot be hidden from the board —
  // that would silently strip one of the view's own conditions.
  const viewBaseline = useViewBaseline();
  const statusFixedByView = !!status && viewBaseline?.status.has(status) === true;
  const { t } = useT("issues");
  const { t: tCommon } = useT("common");

  // Resolve IDs to top-level nodes. Without nesting this preserves the
  // parent-provided order; with nesting sub-issues render inside their parent.
  const entries = useMemo<BoardTreeNode[]>(
    () =>
      hierarchy
        ? buildBoardColumnTree(issueIds, issueMap, hierarchy.lookupParent)
        : issueIds.flatMap((id): BoardTreeNode[] => {
            const issue = issueMap.get(id);
            return issue
              ? [{ kind: "issue", key: id, issue, children: [], descendantCount: 0 }]
              : [];
          }),
    [hierarchy, issueIds, issueMap],
  );
  const sortableIds = useMemo(
    () => visibleTreeIssueIds(entries, hierarchy?.collapsed ?? EMPTY_SET),
    [entries, hierarchy],
  );

  // The column's scroll container is both dnd-kit's droppable and Virtuoso's
  // customScrollParent, so a merged callback ref feeds the element to both.
  // useDroppable's setNodeRef is stable across renders. Keeping the droppable
  // on the always-mounted scroll container (not on individual cards) is what
  // lets cross-column drops survive virtualization — only the cards inside
  // window in/out of the DOM.
  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);
  // Pull-based scroll restoration (MUL-4741): assign the saved offset at
  // ref-attach (the seed + estimate spacer give the column a truthful height
  // on its first commit, so the assignment sticks pre-paint) and feed the
  // same offset into the Virtuoso as its initial position.
  const scrollMementoKey = `board:${group.id}`;
  const restoredScrollTop = useRestoredScrollOffset(scrollMementoKey);
  const restoreScrollRef = useRestoredScrollRef(scrollMementoKey);
  const mergedRef = useCallback(
    (el: HTMLDivElement | null) => {
      setNodeRef(el);
      setScrollEl(el);
      restoreScrollRef(el);
    },
    [setNodeRef, restoreScrollRef],
  );
  // Infinite-scroll sentinel rides Virtuoso's Footer slot so it sits at the
  // real end of the virtualized list and its IntersectionObserver still fires
  // loadMore when scrolled to the bottom.
  const footerComponents = useMemo(
    () => (footer ? { Footer: () => <>{footer}</> } : EMPTY_VIRTUOSO_COMPONENTS),
    [footer],
  );

  const computeItemKey = (_index: number, node: BoardTreeNode) => node.key;
  const itemContent = (index: number, node: BoardTreeNode) => (
    // pt-2 on every card but the first reproduces the previous `space-y-2`
    // gap; padding (not margin) is inside Virtuoso's measured item box so its
    // height math stays correct.
    <div className={index === 0 ? undefined : "pt-2"}>
      <BoardTreeItem
        node={node}
        depth={0}
        childProgressMap={childProgressMap}
        projectMap={projectMap}
        sortLabel={sortLabel}
        hierarchy={hierarchy}
      />
    </div>
  );

  return (
    <div
      style={{ width: BOARD_COL_WIDTH }}
      className={`group/column relative flex shrink-0 flex-col rounded-xl bg-muted/40 p-2 ${
        hint ? "ring-2 ring-brand/30" : ""
      }`}
    >
      {/* A dragged card whose workflow lacks this status cannot land here: the
          column is hatched over and says why. (MUL-7420) */}
      {blockedReason && (
        <div
          aria-hidden
          className="pointer-events-none absolute inset-0 z-20 flex items-center justify-center rounded-xl bg-background/60 bg-[repeating-linear-gradient(135deg,transparent_0_8px,var(--color-border)_8px_9px)]"
        >
          <span className="flex items-center gap-1.5 rounded-md border border-surface-border bg-card px-2.5 py-1 text-caption text-muted-foreground shadow-sm">
            <Ban className="size-3" />
            {blockedReason}
          </span>
        </div>
      )}
      <div className="mb-2 flex items-center justify-between gap-2 px-1.5">
        <BoardGroupHeading group={group} count={totalCount ?? issueIds.length} />

        {/* Right: step handler + add + menu */}
        <div className="flex min-w-0 items-center gap-1">
          {status && <WorkflowStepHandlerBadge status={status} />}
          {/* Column-header popups mount lazily: a board/swimlane renders one
              header per column and almost none of these menus/tooltips are
              ever opened — eagerly mounting them dominated surface mount
              cost (DeferredPopup / DeferredTooltip). */}
          {status && (
            <DeferredPopup
              ariaHasPopup="menu"
              tooltip={tCommon(($) => $.more_actions)}
              triggerRender={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="rounded-full text-muted-foreground"
                  aria-label={tCommon(($) => $.more_actions)}
                >
                  <MoreHorizontal className="size-3.5" />
                </Button>
              }
            >
              {(open, onOpenChange) => (
                <DropdownMenu open={open} onOpenChange={onOpenChange}>
                  <DeferredTooltip
                    content={tCommon(($) => $.more_actions)}
                    trigger={
                      <DropdownMenuTrigger
                        render={
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            className="rounded-full text-muted-foreground"
                            aria-label={tCommon(($) => $.more_actions)}
                          >
                            <MoreHorizontal className="size-3.5" />
                          </Button>
                        }
                      />
                    }
                  />
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem
                      disabled={statusFixedByView}
                      title={statusFixedByView ? t(($) => $.filters.in_view) : undefined}
                      onClick={() => viewStoreApi.getState().hideStatus(status)}
                    >
                      <EyeOff className="size-3.5" />
                      {t(($) => $.board.hide_column)}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
            </DeferredPopup>
          )}
          {onCreateIssue && !archived && (
            <DeferredTooltip
              content={t(($) => $.board.add_issue_tooltip)}
              trigger={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="rounded-full text-muted-foreground"
                  aria-label={t(($) => $.board.add_issue_tooltip)}
                  onClick={() => {
                    const data = {
                      ...(group.createData ?? {}),
                      ...(projectId ? { project_id: projectId } : {}),
                    };
                    onCreateIssue(data);
                  }}
                >
                  <Plus className="size-3.5" />
                </Button>
              }
            />
          )}
        </div>
      </div>
      <div className="relative min-h-[200px] flex-1 rounded-lg">
        {hint && (
          <div className="pointer-events-none absolute inset-x-1 bottom-1 z-10 flex items-center justify-center gap-1.5 rounded-lg border border-dashed border-brand/50 bg-card px-3 py-3 text-caption text-muted-foreground shadow-sm">
            {hint.actor && <ActorAvatar actorType={hint.actor.type} actorId={hint.actor.id} size="xs" profileLink={false} />}
            <span className="truncate">
              {hint.run
                ? t(($) => $.workflows.board.drop_hint_run, { name: hint.name })
                : t(($) => $.workflows.board.drop_hint, { name: hint.name })}
            </span>
          </div>
        )}
        {isOver && sortLabel && (
          <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded-lg bg-background/40">
            <span className="rounded-md bg-popover px-2.5 py-1 text-caption font-medium text-popover-foreground shadow-sm border border-border">
              {sortLabel}
            </span>
          </div>
        )}
        <div
          ref={mergedRef}
          // Per-column scroll registration for the tab session memento
          // (MUL-4741): the group id is the stable memento key, so every
          // column's offset survives tab switches/reloads independently.
          data-tab-scroll-root={scrollMementoKey}
          className={`absolute inset-0 overflow-y-auto rounded-lg p-1 transition-colors ${
            isOver && sortLabel
              ? "ring-2 ring-brand/25 bg-accent/15"
              : isOver
                ? "bg-accent/60"
                : ""
          }`}
        >
          {entries.length > 0 ? (
            <SortableContext
              items={sortableIds}
              strategy={hierarchy ? noShiftStrategy : verticalListSortingStrategy}
            >
              {entries.length <= BOARD_VIRTUALIZE_THRESHOLD ? (
                /* Small column: plain full render (reusing the same
                   itemContent, so it is byte-identical to the virtualized
                   rows). No handoff, no estimates — see
                   BOARD_VIRTUALIZE_THRESHOLD. The footer (infinite-scroll
                   sentinel) renders at the real end of the flow, where its
                   IntersectionObserver works the same as in Virtuoso's
                   Footer slot. */
                <>
                  <VirtuosoSeed
                    data={entries}
                    itemContent={itemContent}
                    computeItemKey={computeItemKey}
                    count={entries.length}
                  />
                  {footer}
                </>
              ) : scrollEl ? (
                <Virtuoso
                  customScrollParent={scrollEl}
                  data={entries}
                  computeItemKey={computeItemKey}
                  initialScrollTop={restoredScrollTop}
                  initialItemCount={Math.min(entries.length, BOARD_SEED_COUNT)}
                  defaultItemHeight={BOARD_CARD_ESTIMATED_HEIGHT}
                  increaseViewportBy={{ top: 300, bottom: 300 }}
                  components={footerComponents}
                  itemContent={itemContent}
                />
              ) : (
                /* Large column, merged scroll ref not settled yet after a
                   remount: seed a bounded slice of real cards so the column
                   never paints blank; once the ref lands, mount the Virtuoso
                   with a matching `initialItemCount` to survive the
                   measurement frame (MUL-4750). */
                <VirtuosoSeed
                  data={entries}
                  itemContent={itemContent}
                  computeItemKey={computeItemKey}
                  count={BOARD_SEED_COUNT}
                  estimatedItemHeight={BOARD_CARD_ESTIMATED_HEIGHT}
                />
              )}
            </SortableContext>
          ) : (
            <>
              {issueIds.length === 0 && (
                <p className="py-8 text-center text-caption text-muted-foreground">
                  {t(($) => $.board.empty_column)}
                </p>
              )}
              {footer}
            </>
          )}
        </div>
      </div>
    </div>
  );
});

const EMPTY_SET: ReadonlySet<string> = new Set();

/** One card or ghost container, with its sub-issues rendered inside. */
function BoardTreeItem({
  node,
  depth,
  childProgressMap,
  projectMap,
  sortLabel,
  hierarchy,
}: {
  node: BoardTreeNode;
  depth: number;
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  sortLabel?: string | null;
  hierarchy?: BoardColumnHierarchy;
}) {
  const { t } = useT("issues");
  const p = useWorkspacePaths();
  const hostId = nodeIssueId(node);
  const collapsed = hierarchy?.collapsed.has(hostId) ?? false;
  const hasChildren = !!hierarchy && node.children.length > 0;
  const countLabel = t(($) => $.board.sub_issue_count, {
    count: node.descendantCount,
  });
  const chevron = collapsed ? (
    <ChevronRight className="size-3.5 shrink-0" />
  ) : (
    <ChevronDown className="size-3.5 shrink-0" />
  );
  const children =
    hasChildren && !collapsed ? (
      <div
        className={`space-y-2 pb-2 ${depth + 1 < MAX_INDENT_DEPTH ? "px-2" : ""}`}
      >
        {node.children.map((child) => (
          <BoardTreeItem
            key={child.key}
            node={child}
            depth={depth + 1}
            childProgressMap={childProgressMap}
            projectMap={projectMap}
            sortLabel={sortLabel}
            hierarchy={hierarchy}
          />
        ))}
      </div>
    ) : null;

  if (node.kind === "ghost") {
    const parent = node.parent;
    return (
      // The parent itself lives in another column (or is not on the board);
      // this muted frame only names it so the sub-issues keep their context.
      <div className="rounded-lg border border-dashed border-border">
        <div className="flex min-w-0 items-center gap-1 px-1.5 py-1.5 text-caption text-muted-foreground">
          <button
            type="button"
            aria-expanded={!collapsed}
            aria-label={countLabel}
            className="flex shrink-0 items-center rounded-sm p-0.5 transition-colors hover:bg-accent/60 hover:text-foreground"
            onClick={() => hierarchy?.onToggle(hostId)}
          >
            {chevron}
          </button>
          <AppLink
            href={p.issueDetail(node.parentId)}
            newTabTitle={parent?.identifier}
            className="flex min-w-0 items-center gap-1.5 hover:text-foreground"
          >
            {parent && <span className="shrink-0">{parent.identifier}</span>}
            <span className="truncate">{parent?.title ?? "…"}</span>
          </AppLink>
          {collapsed && (
            <span className="ml-auto shrink-0 tabular-nums">{node.descendantCount}</span>
          )}
        </div>
        {children}
      </div>
    );
  }

  const { issue } = node;
  return (
    <DraggableBoardCard
      issue={issue}
      childProgress={childProgressMap?.get(issue.id)}
      project={issue.project_id ? projectMap?.get(issue.project_id) : undefined}
      disableSorting={!!sortLabel}
      nested={
        hasChildren ? (
          <>
            {/* Outside the card's link so the toggle is never a control
                nested inside an anchor; buttons are also excluded from
                board panning. */}
            <button
              type="button"
              aria-expanded={!collapsed}
              className="mx-1.5 mb-1.5 flex items-center gap-1 rounded-md px-1 py-0.5 text-caption text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground"
              onClick={() => hierarchy?.onToggle(hostId)}
            >
              {chevron}
              {countLabel}
            </button>
            {children}
          </>
        ) : undefined
      }
    />
  );
}

/** On a project board, the column's handoff for that project. (MUL-7420) */
function WorkflowStepHandlerBadge({ status }: { status: string }) {
  const { scopeWorkflow, scopeProject } = useBoardWorkflow();
  if (!scopeWorkflow || !scopeProject) return null;
  return <ColumnHandoff status={status} workflow={scopeWorkflow} project={scopeProject} />;
}

function BoardGroupHeading({
  group,
  count,
}: {
  group: BoardColumnGroup;
  count: number;
}) {
  if (group.status) {
    return <StatusHeading status={group.status} count={count} />;
  }

  if (group.propertyId !== undefined) {
    return (
      <div className="flex min-w-0 items-center gap-2">
        <span
          className="size-2.5 shrink-0 rounded-full bg-muted-foreground/30"
          style={group.propertyOptionColor ? { backgroundColor: group.propertyOptionColor } : undefined}
        />
        <TruncatedTooltip content={group.title}>
          <span className="truncate text-body font-medium">
            {group.title}
          </span>
        </TruncatedTooltip>
        <span className="shrink-0 rounded-full bg-background px-1.5 py-0.5 text-micro font-medium tabular-nums text-muted-foreground">
          {count}
        </span>
      </div>
    );
  }

  if (group.projectId !== undefined) {
    return (
      <div className="flex min-w-0 items-center gap-2">
        {group.project ? (
          <ProjectIcon project={group.project} size="sm" />
        ) : (
          <span className="flex size-[18px] shrink-0 items-center justify-center rounded-full bg-background text-muted-foreground">
            <FolderMinus className="size-3.5" />
          </span>
        )}
        <TruncatedTooltip content={group.title}>
          <span className="truncate text-body font-medium">
            {group.title}
          </span>
        </TruncatedTooltip>
        <span className="shrink-0 rounded-full bg-background px-1.5 py-0.5 text-micro font-medium tabular-nums text-muted-foreground">
          {count}
        </span>
      </div>
    );
  }

  const actorIcon =
    group.assigneeType && group.assigneeId ? (
      <ActorAvatar
        actorType={group.assigneeType}
        actorId={group.assigneeId}
        size="sm"
        showStatusDot={group.assigneeType === "agent"}
      />
    ) : (
      <span className="flex size-[18px] shrink-0 items-center justify-center rounded-full bg-background text-muted-foreground">
        <UserMinus className="size-3.5" />
      </span>
    );

  return (
    <div className="flex min-w-0 items-center gap-2">
      {actorIcon}
      <TruncatedTooltip content={group.title}>
        <span className="truncate text-body font-medium">
          {group.title}
        </span>
      </TruncatedTooltip>
      <span className="shrink-0 rounded-full bg-background px-1.5 py-0.5 text-micro font-medium tabular-nums text-muted-foreground">
        {count}
      </span>
    </div>
  );
}
