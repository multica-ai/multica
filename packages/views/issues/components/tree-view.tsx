"use client";

import { useState, useMemo, useCallback } from "react";
import { ChevronRight, Search, TreePine } from "lucide-react";
import { AppLink } from "../../navigation";
import type { Issue } from "@wallts/core/types";
import { useWorkspacePaths } from "@wallts/core/paths";
import { useViewStore } from "@wallts/core/issues/stores/view-store-context";
import { useIssueSelectionStore } from "@wallts/core/issues/stores/selection-store";
import { ActorAvatar } from "../../common/actor-avatar";
import { PriorityIcon } from "./priority-icon";
import { StatusIcon } from "./status-icon";
import { ProgressRing } from "./progress-ring";
import { IssueActionsContextMenu } from "../actions";
import type { ChildProgress } from "./list-row";
import { LabelChip } from "../../labels/label-chip";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { IssueAgentActivityIndicator } from "./issue-agent-activity-indicator";
import { useT } from "../../i18n";

// ---------------------------------------------------------------------------
// Tree types & builder
// ---------------------------------------------------------------------------

interface TreeNode {
  issue: Issue;
  children: TreeNode[];
  depth: number;
}

function buildTree(issues: Issue[]): TreeNode[] {
  const map = new Map<string, TreeNode>();
  const roots: TreeNode[] = [];

  // Create all nodes
  for (const issue of issues) {
    map.set(issue.id, { issue, children: [], depth: 0 });
  }

  // Build tree
  for (const issue of issues) {
    const node = map.get(issue.id)!;
    if (issue.parent_issue_id && map.has(issue.parent_issue_id)) {
      const parent = map.get(issue.parent_issue_id)!;
      node.depth = parent.depth + 1;
      parent.children.push(node);
    } else {
      roots.push(node);
    }
  }

  // Sort by position (with cycle protection)
  const visited = new Set<string>();
  const sortNodes = (nodes: TreeNode[]) => {
    nodes.sort((a, b) => a.issue.position - b.issue.position);
    for (const node of nodes) {
      if (!visited.has(node.issue.id)) {
        visited.add(node.issue.id);
        sortNodes(node.children);
      }
    }
  };
  sortNodes(roots);

  return roots;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function formatDate(date: string): string {
  return new Date(date).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
  });
}

/**
 * Filter tree nodes keeping parents visible when any descendant matches.
 * Returns a new filtered tree or null if nothing matched.
 */
function filterTree(
  nodes: TreeNode[],
  query: string,
): TreeNode[] | null {
  if (!query) return nodes;

  const q = query.toLowerCase();
  const results: TreeNode[] = [];

  for (const node of nodes) {
    const selfMatch =
      node.issue.title.toLowerCase().includes(q) ||
      (node.issue.identifier && node.issue.identifier.toLowerCase().includes(q)) ||
      matchesPinyin(node.issue.title, q) ||
      (node.issue.identifier && matchesPinyin(node.issue.identifier, q));

    const filteredChildren = filterTree(node.children, query);

    if (selfMatch || (filteredChildren && filteredChildren.length > 0)) {
      results.push({
        ...node,
        children: filteredChildren ?? [],
      });
    }
  }

  return results.length > 0 ? results : null;
}

// ---------------------------------------------------------------------------
// TreeNodeRow
// ---------------------------------------------------------------------------

function TreeNodeRow({
  node,
  collapsed,
  onToggleCollapse,
  childProgressMap,
}: {
  node: TreeNode;
  collapsed: boolean;
  onToggleCollapse: (id: string) => void;
  childProgressMap?: Map<string, ChildProgress>;
}) {
  const { issue, children, depth } = node;
  const hasChildren = children.length > 0;
  const selected = useIssueSelectionStore((s) => s.selectedIds.has(issue.id));
  const toggle = useIssueSelectionStore((s) => s.toggle);
  const p = useWorkspacePaths();
  const storeProperties = useViewStore((s) => s.cardProperties);
  const childProgress = childProgressMap?.get(issue.id);

  const showChildProgress = storeProperties.childProgress && childProgress;
  const showAssignee = storeProperties.assignee && issue.assignee_type && issue.assignee_id;
  const showStartDate = storeProperties.startDate && issue.start_date;
  const showDueDate = storeProperties.dueDate && issue.due_date;
  const showLabels = storeProperties.labels && (issue.labels?.length ?? 0) > 0;
  const labels = issue.labels ?? [];

  return (
    <IssueActionsContextMenu issue={issue}>
      <div
        className={`group/row flex h-9 items-center gap-2 pr-4 text-sm transition-colors hover:not-data-[popup-open]:bg-accent/60 data-[popup-open]:bg-accent ${
          selected ? "bg-accent/30" : ""
        }`}
        style={{ paddingLeft: depth * 24 + 16 }}
      >
        {/* Expand/collapse chevron */}
        <div className="flex shrink-0 items-center justify-center w-5 h-5">
          {hasChildren ? (
            <button
              type="button"
              aria-expanded={!collapsed}
              className="flex items-center justify-center w-5 h-5 rounded-sm hover:bg-accent-foreground/10 transition-colors"
              onClick={(e) => {
                e.stopPropagation();
                onToggleCollapse(issue.id);
              }}
            >
              <ChevronRight
                className={`size-3.5 text-muted-foreground transition-transform ${
                  !collapsed ? "rotate-90" : ""
                }`}
              />
            </button>
          ) : null}
        </div>

        {/* Priority icon / checkbox */}
        <div className="relative flex shrink-0 items-center justify-center w-4 h-4">
          <PriorityIcon
            priority={issue.priority}
            className={selected ? "hidden" : "group-hover/row:hidden"}
          />
          <input
            type="checkbox"
            checked={selected}
            onChange={() => toggle(issue.id)}
            className={`absolute inset-0 cursor-pointer accent-primary ${
              selected ? "" : "hidden group-hover/row:block"
            }`}
          />
        </div>

        {/* Status icon */}
        <StatusIcon status={issue.status} className="size-3.5 shrink-0" />

        {/* Link area: identifier + title + extras */}
        <AppLink
          href={p.issueDetail(issue.id)}
          className="flex flex-1 items-center gap-2 min-w-0"
        >
          <span className="w-16 shrink-0 text-xs text-muted-foreground">
            {issue.identifier}
          </span>
          <IssueAgentActivityIndicator issueId={issue.id} />

          <span className="flex min-w-0 flex-1 items-center gap-1.5">
            <span className="truncate">{issue.title}</span>
            {showChildProgress && (
              <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-muted/60 px-1.5 py-0.5">
                <ProgressRing done={childProgress!.done} total={childProgress!.total} size={14} />
                <span className="text-[11px] text-muted-foreground tabular-nums font-medium">
                  {childProgress!.done}/{childProgress!.total}
                </span>
              </span>
            )}
            {showLabels && (
              <span className="ml-1.5 hidden md:inline-flex shrink-0 items-center gap-1 max-w-[260px] overflow-hidden">
                {labels.slice(0, 3).map((label) => (
                  <LabelChip key={label.id} label={label} />
                ))}
                {labels.length > 3 && (
                  <span className="text-[11px] text-muted-foreground">
                    +{labels.length - 3}
                  </span>
                )}
              </span>
            )}
          </span>
          {showStartDate && (
            <span className="shrink-0 text-xs text-muted-foreground">
              {formatDate(issue.start_date!)}
            </span>
          )}
          {showDueDate && (
            <span className="shrink-0 text-xs text-muted-foreground">
              {formatDate(issue.due_date!)}
            </span>
          )}
          {showAssignee && (
            <ActorAvatar
              actorType={issue.assignee_type!}
              actorId={issue.assignee_id!}
              size={20}
              enableHoverCard
            />
          )}
        </AppLink>
      </div>
    </IssueActionsContextMenu>
  );
}

// ---------------------------------------------------------------------------
// TreeView (recursive renderer)
// ---------------------------------------------------------------------------

function TreeNodes({
  nodes,
  collapsedIds,
  onToggleCollapse,
  childProgressMap,
}: {
  nodes: TreeNode[];
  collapsedIds: Set<string>;
  onToggleCollapse: (id: string) => void;
  childProgressMap?: Map<string, ChildProgress>;
}) {
  return (
    <>
      {nodes.map((node) => (
        <div key={node.issue.id}>
          <TreeNodeRow
            node={node}
            collapsed={collapsedIds.has(node.issue.id)}
            onToggleCollapse={onToggleCollapse}
            childProgressMap={childProgressMap}
          />
          {node.children.length > 0 && !collapsedIds.has(node.issue.id) && (
            <TreeNodes
              nodes={node.children}
              collapsedIds={collapsedIds}
              onToggleCollapse={onToggleCollapse}
              childProgressMap={childProgressMap}
            />
          )}
        </div>
      ))}
    </>
  );
}

// ---------------------------------------------------------------------------
// Public component
// ---------------------------------------------------------------------------

export function TreeView({
  issues,
  childProgressMap,
}: {
  issues: Issue[];
  childProgressMap?: Map<string, ChildProgress>;
}) {
  const { t } = useT("issues");
  const [search, setSearch] = useState("");
  const [collapsedIds, setCollapsedIds] = useState<Set<string>>(new Set());

  const toggleCollapse = useCallback((id: string) => {
    setCollapsedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  const tree = useMemo(() => buildTree(issues), [issues]);

  const filteredTree = useMemo(() => {
    const q = search.trim();
    if (!q) return tree;
    return filterTree(tree, q) ?? [];
  }, [tree, search]);

  if (issues.length === 0) {
    return (
      <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
        <TreePine className="h-10 w-10 text-muted-foreground/40" />
        <p className="text-sm">{t(($) => $.list.empty_status)}</p>
      </div>
    );
  }

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      {/* Search bar */}
      <div className="flex items-center gap-2 border-b px-4 py-2">
        <Search className="size-4 text-muted-foreground shrink-0" />
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t(($) => $.actor_issues.search_placeholder)}
          className="flex-1 bg-transparent text-sm placeholder:text-muted-foreground outline-none"
        />
      </div>

      {/* Tree */}
      <div className="flex-1 min-h-0 overflow-y-auto py-1">
        {filteredTree.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-12 text-muted-foreground">
            <Search className="h-8 w-8 text-muted-foreground/40" />
            <p className="text-sm">{t(($) => $.actor_issues.search_empty)}</p>
          </div>
        ) : (
          <TreeNodes
            nodes={filteredTree}
            collapsedIds={collapsedIds}
            onToggleCollapse={toggleCollapse}
            childProgressMap={childProgressMap}
          />
        )}
      </div>
    </div>
  );
}
