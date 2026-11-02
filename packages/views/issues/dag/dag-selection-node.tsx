"use client";

import { useState } from "react";
import { AlertTriangle, ChevronDown, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import type { DagFocusWay } from "@multica/core/issues/stores/view-store";
import {
  useViewStore,
  useViewStoreApi,
} from "@multica/core/issues/stores/view-store-context";
import { useT } from "../../i18n";
import { DagAssignAction, useDagIssueSelection } from "./dag-issue-actions";
import { useActionCapacity } from "./use-action-capacity";
import type { DagSelectionTarget } from "./dag-scene-flow";

type ActionKey = "enter" | "focus" | "open" | "upstream" | "downstream" | "edit" | "assign";

/** The selection zone's node state: label, standing actions (collapsing
 *  right-to-left into More on narrow widths), the hidden-chain hint and the
 *  trailing clear button. */
export function DagNodeSelection({
  target,
  focusHiddenCount,
  onOpenIssue,
  onEnterLine,
  onFocusFrame,
  onEnterFocus,
}: {
  target: DagSelectionTarget;
  focusHiddenCount: number;
  onOpenIssue: (issueId: string) => void;
  onEnterLine: (lineId: string) => void;
  onFocusFrame: (frameId: string) => void;
  onEnterFocus?: (issueId: string) => void;
}) {
  const { t: tDag } = useT("dag");
  const storeApi = useViewStoreApi();
  const focus = useViewStore((s) => s.dagFocus);
  const showCompleted = useViewStore((s) => s.dagShowCompleted);
  const { issue, actions, detailFailed, retryDetail } = useDagIssueSelection(
    target.issueId,
  );
  const [assignFromMenu, setAssignFromMenu] = useState(false);
  const toggleFocus = (way: DagFocusWay) => {
    const state = storeApi.getState();
    const current = state.dagFocus;
    state.setDagFocus(
      current?.nodeId === target.id && current.way === way
        ? null
        : { nodeId: target.id, way },
    );
  };
  const focusActive = (way: DagFocusWay) =>
    focus?.nodeId === target.id && focus.way === way;

  const actionKeys: ActionKey[] = [
    ...(target.enterLineId ? (["enter"] as const) : []),
    ...(target.issueId ? (["open"] as const) : []),
    ...(target.issueId && !target.enterLineId && onEnterFocus ? (["focus"] as const) : []),
    "upstream",
    "downstream",
    ...(target.issueId ? (["edit", "assign"] as const) : []),
  ];
  const { zoneRef, visibleCount } = useActionCapacity(actionKeys, target.id);
  const visibleKeys = actionKeys.slice(0, visibleCount);
  const menuKeys = actionKeys.slice(visibleCount);

  const actionNodes: Record<ActionKey, React.ReactNode> = {
    focus: (
      <Button size="sm" variant="ghost" onClick={() => onEnterFocus?.(target.issueId!)} data-dag-enter-focus>
        {tDag(($) => $.focus_mode)}
      </Button>
    ),
    enter: (
      <Button
        size="sm"
        variant="ghost"
        onClick={() => onEnterLine(target.enterLineId!)}
      >
        {tDag(($) => $.enter_line)}
      </Button>
    ),
    open: (
      <Button size="sm" variant="ghost" onClick={() => onOpenIssue(target.issueId!)}>
        {tDag(($) => $.open_detail)}
      </Button>
    ),
    upstream: (
      <Button
        size="sm"
        variant={focusActive("upstream") ? "secondary" : "ghost"}
        aria-pressed={focusActive("upstream")}
        onClick={() => toggleFocus("upstream")}
      >
        {tDag(($) => $.focus_upstream)}
      </Button>
    ),
    downstream: (
      <Button
        size="sm"
        variant={focusActive("downstream") ? "secondary" : "ghost"}
        aria-pressed={focusActive("downstream")}
        onClick={() => toggleFocus("downstream")}
      >
        {tDag(($) => $.focus_downstream)}
      </Button>
    ),
    edit: (
      <Button
        size="sm"
        variant="ghost"
        disabled={!issue}
        onClick={actions.openEditDependencies}
      >
        {tDag(($) => $.edit_dependencies)}
      </Button>
    ),
    assign: <DagAssignAction issue={issue} onUpdate={actions.updateField} />,
  };
  const menuItems: Record<ActionKey, React.ReactNode> = {
    focus: (
      <DropdownMenuItem key="focus" onClick={() => onEnterFocus?.(target.issueId!)} data-dag-enter-focus>
        {tDag(($) => $.focus_mode)}
      </DropdownMenuItem>
    ),
    enter: (
      <DropdownMenuItem key="enter" onClick={() => onEnterLine(target.enterLineId!)}>
        {tDag(($) => $.enter_line)}
      </DropdownMenuItem>
    ),
    open: (
      <DropdownMenuItem key="open" onClick={() => onOpenIssue(target.issueId!)}>
        {tDag(($) => $.open_detail)}
      </DropdownMenuItem>
    ),
    upstream: (
      <DropdownMenuCheckboxItem
        key="upstream"
        checked={focusActive("upstream")}
        onCheckedChange={() => toggleFocus("upstream")}
      >
        {tDag(($) => $.focus_upstream)}
      </DropdownMenuCheckboxItem>
    ),
    downstream: (
      <DropdownMenuCheckboxItem
        key="downstream"
        checked={focusActive("downstream")}
        onCheckedChange={() => toggleFocus("downstream")}
      >
        {tDag(($) => $.focus_downstream)}
      </DropdownMenuCheckboxItem>
    ),
    edit: (
      <DropdownMenuItem
        key="edit"
        disabled={!issue}
        onClick={actions.openEditDependencies}
      >
        {tDag(($) => $.edit_dependencies)}
      </DropdownMenuItem>
    ),
    assign: (
      <DropdownMenuItem key="assign" onClick={() => setAssignFromMenu(true)}>
        {tDag(($) => $.assign_issue)}
      </DropdownMenuItem>
    ),
  };
  const hasMenu = menuKeys.length > 0 || target.focusFrameId != null;
  const label =
    target.kind === "independent" ? tDag(($) => $.independent_group) : target.title;
  // The click cycle's current scope: one layer by default, all upstream +
  // downstream after the second click. A one-way focus shows on its button.
  const scopeTag = (() => {
    if (focus?.nodeId === target.id && focus.way === "both")
      return tDag(($) => $.selection_scope_closure);
    if (!focus || focus.nodeId !== target.id)
      return tDag(($) => $.selection_scope_neighborhood);
    return null;
  })();

  return (
    <div ref={zoneRef} data-dag-zone className="flex min-w-0 flex-1 items-center gap-1">
      <span
        data-dag-label
        className="max-w-56 shrink truncate px-1 text-caption font-medium text-foreground"
        title={label ?? undefined}
      >
        {target.identifier && (
          <span className="mr-1.5 tabular-nums text-muted-foreground">
            {target.identifier}
          </span>
        )}
        {label}
      </span>
      {scopeTag && (
        <span data-dag-fixed className="inline-flex shrink-0">
          <span
            data-dag-scope
            className="rounded-full bg-muted/60 px-1.5 py-0.5 text-micro font-normal text-muted-foreground"
          >
            {scopeTag}
          </span>
        </span>
      )}
      {visibleKeys.map((key) => (
        <span key={key} data-dag-action={key} className="inline-flex shrink-0">
          {actionNodes[key]}
        </span>
      ))}
      {detailFailed && (
        <span data-dag-fixed className="inline-flex shrink-0">
          <Button size="sm" variant="ghost" onClick={retryDetail}>
            {tDag(($) => $.error_retry)}
          </Button>
        </span>
      )}
      {hasMenu && (
        <span data-dag-fixed className="relative inline-flex shrink-0">
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button size="sm" variant="ghost" aria-label={tDag(($) => $.more_actions)} />
              }
            >
              {tDag(($) => $.more_actions)}
              <ChevronDown className="size-3.5" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-52">
              {menuKeys.map((key) => menuItems[key])}
              {menuKeys.length > 0 && target.focusFrameId != null && (
                <DropdownMenuSeparator />
              )}
              {target.focusFrameId != null && (
                <DropdownMenuItem onClick={() => onFocusFrame(target.focusFrameId!)}>
                  {tDag(($) => $.focus_line)}
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
          {menuKeys.includes("assign") && issue && (
            <DagAssignAction
              issue={issue}
              onUpdate={actions.updateField}
              open={assignFromMenu}
              onOpenChange={setAssignFromMenu}
              anchor={<span className="absolute inset-x-0 top-full h-px" aria-hidden />}
            />
          )}
        </span>
      )}
      {focusHiddenCount > 0 && !showCompleted && (
        <span data-dag-fixed className="inline-flex shrink-0 items-center gap-1 text-warning">
          <AlertTriangle className="size-3" />
          <span className="tabular-nums">
            {tDag(($) => $.focus_hidden_tasks, { count: focusHiddenCount })}
          </span>
          <Button
            size="sm"
            variant="ghost"
            className="text-brand"
            onClick={() => storeApi.getState().toggleDagShowCompleted()}
          >
            {tDag(($) => $.show_completed_enable)}
          </Button>
        </span>
      )}
      <span data-dag-fixed className="inline-flex shrink-0">
        <Button
          size="icon-sm"
          variant="ghost"
          aria-label={tDag(($) => $.clear_selection)}
          onClick={() => storeApi.getState().setDagSelectedNodeId(null)}
        >
          <X className="size-3.5" />
        </Button>
      </span>
    </div>
  );
}
