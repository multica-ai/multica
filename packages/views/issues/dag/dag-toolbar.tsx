"use client";

import {
  AlertTriangle,
  ChevronLeft,
  ChevronRight,
  Info,
  ListTree,
  Loader2,
  X,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import {
  useViewStore,
  useViewStoreApi,
} from "@multica/core/issues/stores/view-store-context";
import { useT } from "../../i18n";
import { DagNodeSelection } from "./dag-selection-node";
import { DagBusSelection, type DagBusSelectionItem } from "./dag-selection-bus";
import type { DagLevel } from "./dag-level";
import type { DagSelectionTarget } from "./dag-scene-flow";

export interface DagToolbarProps {
  level: DagLevel;
  focusIdentifier?: string | null;
  onEnterFocus?: (issueId: string) => void;
  onExitFocus?: () => void;
  /** Current line's header for the breadcrumb; null while it loads. */
  lineHeader: { identifier: string | null; title: string } | null;
  selection: DagSelectionTarget | null;
  /** Bus selection: the original dependencies through a clicked segment. */
  busSelection: DagBusSelectionItem[] | null;
  /** Visible-content statistics for the idle selection zone. */
  stats: { matched: number; context: number; edges: number };
  /** Overview-only hidden-completed counts; null inside a line (the completed
   *  switch does not filter line members). */
  hiddenCompleted: { lines: number; lineTasks: number; singles: number } | null;
  hasRestrictedContext: boolean;
  contextOnly: boolean;
  pending: boolean;
  pendingLong: boolean;
  onCancelLayout: () => void;
  /** The layout engine degraded to bounded packing after its deadline. */
  isFallback: boolean;
  onRetryLayout: () => void;
  focusHiddenCount: number;
  onOpenIssue: (issueId: string) => void;
  onEnterLine: (lineId: string) => void;
  onExitLine: () => void;
  onFocusFrame: (frameId: string) => void;
  onLocateDependency: (dependencyId: string) => void;
}

export function DagToolbar({
  level,
  focusIdentifier,
  onEnterFocus,
  onExitFocus,
  lineHeader,
  selection,
  busSelection,
  stats,
  hiddenCompleted,
  hasRestrictedContext,
  contextOnly,
  pending,
  pendingLong,
  onCancelLayout,
  isFallback,
  onRetryLayout,
  focusHiddenCount,
  onOpenIssue,
  onEnterLine,
  onExitLine,
  onFocusFrame,
  onLocateDependency,
}: DagToolbarProps) {
  const { t: tDag } = useT("dag");
  const { t: tIssues } = useT("issues");
  const storeApi = useViewStoreApi();
  const showCompleted = useViewStore((s) => s.dagShowCompleted);
  const showSatisfied = useViewStore((s) => s.dagShowSatisfiedDependencies);
  const hiddenTotal = hiddenCompleted
    ? hiddenCompleted.lineTasks + hiddenCompleted.singles
    : 0;
  const toggleShowCompleted = () => storeApi.getState().toggleDagShowCompleted();
  const showCompletedButton = (withBadge: boolean) => (
    <Button
      size="sm"
      variant={showCompleted ? "secondary" : "outline"}
      className={cn("h-7 text-caption", !showCompleted && "text-muted-foreground")}
      onClick={toggleShowCompleted}
      aria-pressed={showCompleted}
      aria-label={
        withBadge
          ? `${tIssues(($) => $.gantt.show_completed)} (${hiddenTotal})`
          : undefined
      }
    >
      {tIssues(($) => $.gantt.show_completed)}
      {withBadge && (
        <span
          aria-hidden
          className="inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-muted px-1 text-micro tabular-nums text-muted-foreground"
        >
          {hiddenTotal}
        </span>
      )}
    </Button>
  );

  return (
    <>
    <div
      className="flex h-10 shrink-0 items-center gap-2 overflow-hidden border-b px-2 text-caption text-muted-foreground"
      data-dag-toolbar
    >
      {level.kind === "line" && (
        <Button
          size="sm"
          variant="ghost"
          className="shrink-0"
          onClick={onExitLine}
          data-dag-back
        >
          <ChevronLeft className="size-3.5" />
          {tDag(($) => $.back_to_overview)}
        </Button>
      )}
      <nav
        aria-label={tDag(($) => $.breadcrumb_label)}
        className="flex min-w-0 shrink items-center gap-1 text-caption"
      >
        {level.kind === "overview" ? (
          <span className="shrink-0 px-1 font-medium text-foreground">
            {tDag(($) => $.all_lines)}
          </span>
        ) : (
          <>
            <button
              type="button"
              className="shrink-0 rounded-sm px-1 text-muted-foreground outline-offset-2 hover:text-foreground focus-visible:outline-2 focus-visible:outline-brand"
              onClick={onExitLine}
            >
              {tDag(($) => $.all_lines)}
            </button>
            {lineHeader && (
              <>
                <ChevronRight className="size-3 shrink-0 text-faint-foreground" aria-hidden />
                {level.kind === "line" ? (
                  <span
                    aria-current="page"
                    className="min-w-0 max-w-64 truncate px-1 font-medium text-foreground"
                    data-dag-crumb-current
                  >
                    {lineHeader.identifier && <span className="mr-1.5 tabular-nums text-muted-foreground">{lineHeader.identifier}</span>}
                    {lineHeader.title}
                  </span>
                ) : (
                  <button
                    type="button"
                    className="min-w-0 max-w-64 truncate px-1 font-medium text-foreground"
                    onClick={onExitFocus}
                  >
                    {lineHeader.identifier && <span className="mr-1.5 tabular-nums text-muted-foreground">{lineHeader.identifier}</span>}
                    {lineHeader.title}
                  </button>
                )}
              </>
            )}
            {level.kind === "focus" && (
              <>
                <ChevronRight className="size-3 shrink-0 text-faint-foreground" aria-hidden />
                <span aria-current="page" className="shrink-0 font-medium text-foreground" data-dag-crumb-current>
                  {tDag(($) => $.focus_breadcrumb, { identifier: focusIdentifier ?? "..." })}
                </span>
                <Button size="icon-sm" variant="ghost" aria-label={tDag(($) => $.exit_focus)} onClick={onExitFocus} data-dag-exit-focus>
                  <X className="size-3.5" />
                </Button>
              </>
            )}
          </>
        )}
      </nav>
      <span className="h-4 w-px shrink-0 bg-border" aria-hidden />
      <div
        role="region"
        aria-label={tDag(($) => $.selection_region_label)}
        className={cn(
          "flex min-w-0 flex-1 items-center gap-1 overflow-hidden rounded-md px-1.5 py-0.5 transition-colors",
          (selection || busSelection) && "bg-accent/60 text-foreground",
        )}
      >
        {selection ? (
          <DagNodeSelection
            target={selection}
            focusHiddenCount={level.kind === "focus" ? 0 : focusHiddenCount}
            onEnterFocus={level.kind === "focus" ? undefined : onEnterFocus}
            onOpenIssue={onOpenIssue}
            onEnterLine={onEnterLine}
            onFocusFrame={onFocusFrame}
          />
        ) : busSelection ? (
          <DagBusSelection
            items={busSelection}
            onLocate={onLocateDependency}
            onOpenIssue={onOpenIssue}
          />
        ) : (
          <>
            <span className="inline-flex shrink-0 items-center gap-1.5">
              <ListTree className="size-3.5" />
              <span className="tabular-nums">
                {tDag(($) => $.summary_matches, { count: stats.matched })}
              </span>
              {stats.context > 0 && (
                <span className="tabular-nums">
                  {tDag(($) => $.summary_context, { count: stats.context })}
                </span>
              )}
              <span className="tabular-nums">
                {tDag(($) => $.summary_edges, { count: stats.edges })}
              </span>
            </span>
            <span className="min-w-0 truncate text-faint-foreground">
              {tDag(($) => $.selection_hint)}
            </span>
          </>
        )}
      </div>
      <span className="flex shrink-0 items-center gap-1">
        {pending && (
          <span className="inline-flex items-center gap-1.5" role="status">
            <Loader2 className="size-3 animate-spin" />
            {tDag(($) => $.layout_pending)}
            {pendingLong && (
              <Button size="sm" variant="ghost" onClick={onCancelLayout}>
                {tDag(($) => $.layout_cancel)}
              </Button>
            )}
          </span>
        )}
        {isFallback && (
          <span className="inline-flex items-center gap-1" role="status" data-dag-fallback>
            <span className="text-warning">{tDag(($) => $.layout_fallback)}</span>
            <Button size="sm" variant="ghost" onClick={onRetryLayout}>
              {tDag(($) => $.layout_retry)}
            </Button>
          </span>
        )}
        {hasRestrictedContext && (
          <Tooltip>
            <TooltipTrigger
              render={
                <span
                  tabIndex={0}
                  role="img"
                  aria-label={tDag(($) => $.restricted_context_hint)}
                  className="inline-flex items-center rounded-sm p-1 outline-none focus-visible:ring-2 focus-visible:ring-ring"
                />
              }
            >
              <AlertTriangle className="size-3.5 text-warning" />
            </TooltipTrigger>
            <TooltipContent side="bottom">
              {tDag(($) => $.restricted_context_hint)}
            </TooltipContent>
          </Tooltip>
        )}
        {contextOnly && (
          <Tooltip>
            <TooltipTrigger
              render={
                <span
                  tabIndex={0}
                  role="img"
                  aria-label={tDag(($) => $.context_only_hint)}
                  className="inline-flex items-center rounded-sm p-1 outline-none focus-visible:ring-2 focus-visible:ring-ring"
                />
              }
            >
              <AlertTriangle className="size-3.5 text-warning" />
            </TooltipTrigger>
            <TooltipContent side="bottom">
              {tDag(($) => $.context_only_hint)}
            </TooltipContent>
          </Tooltip>
        )}
        {hiddenTotal > 0 ? (
          <Tooltip>
            <TooltipTrigger render={showCompletedButton(true)} />
            <TooltipContent side="bottom">
              {tDag(($) => $.hidden_completed_tooltip, {
                count: hiddenCompleted!.lines,
                tasks: hiddenCompleted!.lineTasks,
                singles: hiddenCompleted!.singles,
              })}
            </TooltipContent>
          </Tooltip>
        ) : (
          showCompletedButton(false)
        )}
        {level.kind !== "focus" && <Button
          size="sm"
          variant={showSatisfied ? "secondary" : "outline"}
          className={cn("h-7 text-caption", !showSatisfied && "text-muted-foreground")}
          onClick={() => storeApi.getState().toggleDagShowSatisfiedDependencies()}
          aria-pressed={showSatisfied}
          data-dag-show-satisfied
        >
          {tDag(($) => $.show_satisfied)}
        </Button>}
        <Popover>
          <PopoverTrigger
            render={
              <Button
                size="icon-sm"
                variant="ghost"
                aria-label={tDag(($) => $.legend_label)}
                title={tDag(($) => $.legend_label)}
              />
            }
          >
            <Info className="size-3.5" />
          </PopoverTrigger>
          <PopoverContent align="end" className="w-auto max-w-72 p-3">
            <ul className="flex flex-col gap-1.5 text-caption text-muted-foreground">
              <li>{tDag(($) => $.stage_legend)}</li>
              <li>{tDag(($) => $.legend_edge_satisfied)}</li>
              <li>{tDag(($) => $.legend_edge_blocking)}</li>
              <li>{tDag(($) => $.legend_edge_advancing)}</li>
              <li>{tDag(($) => $.legend_edge_cancelled)}</li>
              <li>{tDag(($) => $.legend_external)}</li>
              <li>{tDag(($) => $.legend_highlight)}</li>
              <li>{tDag(($) => $.legend_lit_flow)}</li>
            </ul>
          </PopoverContent>
        </Popover>
      </span>
    </div>
    {level.kind === "focus" && focusHiddenCount > 0 && !showCompleted && (
      <div role="status" className="flex shrink-0 items-center gap-2 border-b px-3 py-1 text-caption text-warning" data-dag-focus-hidden>
        <AlertTriangle className="size-3.5" />
        <span>{tDag(($) => $.focus_hidden_tasks, { count: focusHiddenCount })}</span>
        <Button size="sm" variant="ghost" onClick={toggleShowCompleted}>
          {tDag(($) => $.show_completed_enable)}
        </Button>
      </div>
    )}
    </>
  );
}
