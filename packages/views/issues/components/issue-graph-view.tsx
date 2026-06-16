"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Background,
  BackgroundVariant,
  Controls,
  MarkerType,
  MiniMap,
  NodeToolbar,
  Panel,
  Position,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  type Edge,
  type Node,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  ArrowDownRight,
  ArrowUpLeft,
  ChevronDown,
  ChevronUp,
  Focus,
  GitFork,
  RotateCcw,
  Search,
  SquareArrowOutUpRight,
  Waypoints,
  X,
} from "lucide-react";
import type { Issue } from "@multica/core/types";
import { useModalStore } from "@multica/core/modals";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { useIssueActions } from "../actions";
import {
  ISSUE_GRAPH_NODE_HEIGHT,
  ISSUE_GRAPH_NODE_WIDTH,
  layoutIssueGraph,
} from "./issue-graph-layout";
import {
  IssueGraphNode,
  type IssueGraphNodeData,
} from "./issue-graph-node";
import { traceIssueRelationships } from "./issue-graph-relations";

const nodeTypes = { issue: IssueGraphNode };

function IssueGraphCanvas({
  issues,
  matchedIssueIds,
  childProgressMap,
}: {
  issues: Issue[];
  matchedIssueIds: Set<string>;
  childProgressMap: Map<string, { done: number; total: number }>;
}) {
  const { t } = useT("issues");
  const { fitView, setCenter } = useReactFlow();
  const openModal = useModalStore((state) => state.open);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [searchPanelOpen, setSearchPanelOpen] = useState(false);
  const [showRelatedOnly, setShowRelatedOnly] = useState(true);
  const [showInitialLatestHighlight, setShowInitialLatestHighlight] =
    useState(true);
  const clearInitialHighlight = useCallback(
    () => setShowInitialLatestHighlight(false),
    [],
  );

  const issueById = useMemo(
    () => new Map(issues.map((issue) => [issue.id, issue])),
    [issues],
  );
  const childrenByParent = useMemo(() => {
    const map = new Map<string, string[]>();
    for (const issue of issues) {
      if (!issue.parent_issue_id || !issueById.has(issue.parent_issue_id)) continue;
      const children = map.get(issue.parent_issue_id) ?? [];
      children.push(issue.id);
      map.set(issue.parent_issue_id, children);
    }
    return map;
  }, [issueById, issues]);

  const visibleIds = useMemo(() => {
    if (matchedIssueIds.size === issues.length) return matchedIssueIds;
    const ids = new Set(matchedIssueIds);
    for (const id of matchedIssueIds) {
      let parentId = issueById.get(id)?.parent_issue_id ?? null;
      while (parentId && issueById.has(parentId)) {
        ids.add(parentId);
        parentId = issueById.get(parentId)?.parent_issue_id ?? null;
      }
      for (const childId of childrenByParent.get(id) ?? []) ids.add(childId);
    }
    return ids;
  }, [childrenByParent, issueById, issues.length, matchedIssueIds]);

  const visibleIssues = useMemo(
    () => issues.filter((issue) => visibleIds.has(issue.id)),
    [issues, visibleIds],
  );
  const matchedIssues = useMemo(
    () => issues.filter((issue) => matchedIssueIds.has(issue.id)),
    [issues, matchedIssueIds],
  );
  const latestCreatedIssueId = useMemo(() => {
    let latestId: string | null = null;
    let latestTime = -Infinity;
    for (const issue of matchedIssues) {
      const createdTime = Date.parse(issue.created_at);
      if (!Number.isFinite(createdTime)) continue;
      if (createdTime > latestTime) {
        latestTime = createdTime;
        latestId = issue.id;
      }
    }
    return latestId;
  }, [matchedIssues]);
  const latestCreatedTrace = useMemo(
    () => traceIssueRelationships(visibleIssues, latestCreatedIssueId),
    [latestCreatedIssueId, visibleIssues],
  );
  const latestCreatedChainIds = useMemo(() => {
    const ids = new Set<string>();
    if (!showInitialLatestHighlight || !latestCreatedIssueId) return ids;
    ids.add(latestCreatedIssueId);
    for (const id of latestCreatedTrace.ancestors) ids.add(id);
    for (const id of latestCreatedTrace.descendants) ids.add(id);
    return ids;
  }, [latestCreatedIssueId, latestCreatedTrace, showInitialLatestHighlight]);
  const relatedIssueIds = useMemo(() => {
    const ids = new Set<string>();
    for (const issue of visibleIssues) {
      if (issue.parent_issue_id && visibleIds.has(issue.parent_issue_id)) {
        ids.add(issue.id);
        ids.add(issue.parent_issue_id);
      }
      for (const childId of childrenByParent.get(issue.id) ?? []) {
        if (visibleIds.has(childId)) {
          ids.add(issue.id);
          ids.add(childId);
        }
      }
    }
    return ids;
  }, [childrenByParent, visibleIds, visibleIssues]);
  const graphIssues = useMemo(
    () =>
      showRelatedOnly
        ? visibleIssues.filter(
            (issue) =>
              relatedIssueIds.has(issue.id) || latestCreatedChainIds.has(issue.id),
          )
        : visibleIssues,
    [latestCreatedChainIds, relatedIssueIds, showRelatedOnly, visibleIssues],
  );
  const graphIssueIds = useMemo(
    () => new Set(graphIssues.map((issue) => issue.id)),
    [graphIssues],
  );
  const hiddenUnrelatedCount = showRelatedOnly
    ? visibleIssues.length - graphIssues.length
    : 0;
  const showInitialSpotlight =
    showInitialLatestHighlight && !selectedId && latestCreatedIssueId !== null;
  const relationshipTrace = useMemo(
    () => traceIssueRelationships(graphIssues, selectedId),
    [graphIssues, selectedId],
  );
  const selectedIssue = selectedId ? issueById.get(selectedId) ?? null : null;
  const selectedIssueActions = useIssueActions(selectedIssue);
  const positions = useMemo(() => layoutIssueGraph(graphIssues), [graphIssues]);

  const nodes = useMemo<Node<IssueGraphNodeData>[]>(
    () =>
      graphIssues.map((issue) => ({
        id: issue.id,
        type: "issue",
        position: positions.get(issue.id) ?? { x: 0, y: 0 },
        width: ISSUE_GRAPH_NODE_WIDTH,
        height: ISSUE_GRAPH_NODE_HEIGHT,
        selected: issue.id === selectedId,
        data: {
          issue,
          childCount: childrenByParent.get(issue.id)?.length ?? 0,
          childProgress: childProgressMap.get(issue.id),
          contextual: !matchedIssueIds.has(issue.id),
          initialHighlight: showInitialSpotlight
            ? issue.id === latestCreatedIssueId
              ? "root"
              : latestCreatedChainIds.has(issue.id)
                ? "chain"
                : null
            : null,
          initialSpotlightMuted:
            showInitialSpotlight && !latestCreatedChainIds.has(issue.id),
          relation:
            issue.id === selectedId
              ? "selected"
              : relationshipTrace.ancestors.has(issue.id)
                ? "ancestor"
                : relationshipTrace.descendants.has(issue.id)
                  ? "descendant"
                  : selectedId
                    ? "unrelated"
                    : "none",
          relationLabel: relationshipTrace.ancestors.has(issue.id)
            ? t(($) => $.graph.ancestor)
            : relationshipTrace.descendants.has(issue.id)
              ? t(($) => $.graph.descendant)
              : undefined,
        },
      })),
    [
      childProgressMap,
      childrenByParent,
      graphIssues,
      latestCreatedChainIds,
      latestCreatedIssueId,
      matchedIssueIds,
      positions,
      relationshipTrace,
      selectedId,
      showInitialSpotlight,
      t,
    ],
  );

  const edges = useMemo<Edge[]>(
    () =>
      graphIssues.flatMap((issue) => {
        if (!issue.parent_issue_id || !visibleIds.has(issue.parent_issue_id)) return [];
        const initialSpotlightEdge =
          showInitialSpotlight &&
          latestCreatedChainIds.has(issue.parent_issue_id) &&
          latestCreatedChainIds.has(issue.id);
        const upstream =
          relationshipTrace.ancestors.has(issue.parent_issue_id) &&
          (relationshipTrace.ancestors.has(issue.id) ||
            issue.id === selectedId);
        const downstream =
          (issue.parent_issue_id === selectedId ||
            relationshipTrace.descendants.has(issue.parent_issue_id)) &&
          relationshipTrace.descendants.has(issue.id);
        const active = upstream || downstream;
        const stroke = upstream
          ? "var(--info)"
          : downstream
            ? "var(--success)"
            : initialSpotlightEdge
              ? "var(--warning)"
              : "var(--muted-foreground)";
        return [
          {
            id: `${issue.parent_issue_id}:${issue.id}`,
            source: issue.parent_issue_id,
            target: issue.id,
            type: "smoothstep",
            animated: active,
            markerEnd: {
              type: MarkerType.ArrowClosed,
              width: 14,
              height: 14,
              color: stroke,
            },
            style: {
              stroke,
              strokeWidth: active ? 2.2 : initialSpotlightEdge ? 1.7 : 1.05,
              opacity: active
                ? 0.95
                : initialSpotlightEdge
                  ? 0.72
                  : selectedId || showInitialSpotlight
                    ? 0.07
                    : 0.28,
              strokeDasharray: initialSpotlightEdge && !active ? "5 5" : undefined,
            },
          },
        ];
      }),
    [
      graphIssues,
      latestCreatedChainIds,
      relationshipTrace,
      selectedId,
      showInitialSpotlight,
      visibleIds,
    ],
  );

  const focusIssue = useCallback(
    (issue: Issue) => {
      clearInitialHighlight();
      const position = positions.get(issue.id);
      if (!position) return;
      setSelectedId(issue.id);
      setCenter(
        position.x + ISSUE_GRAPH_NODE_WIDTH / 2,
        position.y + ISSUE_GRAPH_NODE_HEIGHT / 2,
        { zoom: 1.05, duration: 500 },
      );
    },
    [clearInitialHighlight, positions, setCenter],
  );

  const resetView = useCallback(() => {
    void fitView({ padding: 0.18, duration: 450, maxZoom: 1 });
  }, [fitView]);

  const focusDescendants = useCallback(() => {
    clearInitialHighlight();
    if (!selectedIssue) return;
    const ids = [selectedIssue.id, ...relationshipTrace.descendants];
    if (ids.length <= 1) {
      focusIssue(selectedIssue);
      return;
    }
    void fitView({
      nodes: ids.map((id) => ({ id })),
      padding: 0.24,
      duration: 450,
      maxZoom: 1.08,
    });
  }, [
    clearInitialHighlight,
    fitView,
    focusIssue,
    relationshipTrace.descendants,
    selectedIssue,
  ]);

  const searchQuery = search.trim().toLocaleLowerCase();
  const searchMatches = useMemo(() => {
    if (!searchQuery) return [];
    return graphIssues.filter(
      (candidate) =>
        candidate.identifier.toLocaleLowerCase().includes(searchQuery) ||
        candidate.title.toLocaleLowerCase().includes(searchQuery),
    );
  }, [graphIssues, searchQuery]);
  const selectedSearchIndex = useMemo(
    () =>
      selectedId
        ? searchMatches.findIndex((issue) => issue.id === selectedId)
        : -1,
    [searchMatches, selectedId],
  );
  const focusSearchMatch = useCallback(
    (index: number) => {
      if (searchMatches.length === 0) return;
      const normalized =
        ((index % searchMatches.length) + searchMatches.length) %
        searchMatches.length;
      focusIssue(searchMatches[normalized]!);
    },
    [focusIssue, searchMatches],
  );
  const handleSearch = useCallback(() => {
    if (!searchQuery || searchMatches.length === 0) return;
    focusSearchMatch(selectedSearchIndex >= 0 ? selectedSearchIndex + 1 : 0);
  }, [focusSearchMatch, searchMatches.length, searchQuery, selectedSearchIndex]);

  useEffect(() => {
    const frame = requestAnimationFrame(() =>
      fitView({ padding: 0.18, duration: 450, maxZoom: 1 }),
    );
    return () => cancelAnimationFrame(frame);
  }, [fitView, graphIssues.length]);

  useEffect(() => {
    if (selectedId && !graphIssueIds.has(selectedId)) setSelectedId(null);
  }, [graphIssueIds, selectedId]);

  if (graphIssues.length === 0) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 text-muted-foreground">
        <Waypoints className="size-10 opacity-35" />
        <p className="text-sm">{t(($) => $.graph.empty)}</p>
        {showRelatedOnly && visibleIssues.length > 0 && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              clearInitialHighlight();
              setShowRelatedOnly(false);
            }}
          >
            {t(($) => $.graph.show_all_action)}
          </Button>
        )}
      </div>
    );
  }

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      nodeTypes={nodeTypes}
      nodesDraggable={false}
      nodesConnectable={false}
      elementsSelectable
      minZoom={0.25}
      maxZoom={1.6}
      zoomOnDoubleClick={false}
      proOptions={{ hideAttribution: true }}
      onPaneClick={() => {
        clearInitialHighlight();
        setSelectedId(null);
      }}
      onNodeClick={(_, node) => {
        clearInitialHighlight();
        setSelectedId(node.id);
      }}
      fitView
      className="bg-background"
    >
      <Background
        variant={BackgroundVariant.Dots}
        gap={22}
        size={1}
        color="var(--muted-foreground)"
        className="opacity-[0.16]"
      />
      <div
        className="pointer-events-none absolute inset-0"
        style={{
          background:
            "radial-gradient(circle at 45% 42%, color-mix(in oklab, var(--brand) 6%, transparent), transparent 38%)",
        }}
      />
      <Controls
        position="bottom-right"
        showInteractive={false}
        className="overflow-hidden rounded-lg border bg-card shadow-sm"
      />
      <MiniMap
        position="bottom-left"
        pannable
        zoomable
        nodeColor={(node) =>
          node.id === selectedId ||
          (showInitialSpotlight && node.id === latestCreatedIssueId)
            ? "var(--brand)"
            : "var(--muted-foreground)"
        }
        nodeStrokeWidth={0}
        maskColor="color-mix(in oklab, var(--background) 78%, transparent)"
        className="!rounded-lg !border !bg-card/90 !shadow-sm"
      />

      {selectedIssue && (
        <NodeToolbar
          nodeId={selectedIssue.id}
          isVisible
          position={Position.Top}
          align="center"
          offset={12}
          className="nodrag nopan"
        >
          <div
            className="flex max-w-[min(680px,calc(100vw-3rem))] flex-wrap items-center justify-center gap-1 rounded-xl border bg-card/95 p-1.5 shadow-lg shadow-black/5 backdrop-blur-md"
            onClick={(event) => event.stopPropagation()}
            onPointerDown={(event) => event.stopPropagation()}
          >
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-7 rounded-lg text-xs"
              onClick={() =>
                openModal("issue-detail", { issueId: selectedIssue.id })
              }
            >
              <SquareArrowOutUpRight className="size-3.5" />
              {t(($) => $.graph.open_action)}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 rounded-lg text-xs"
              onClick={selectedIssueActions.openCreateSubIssue}
            >
              <GitFork className="size-3.5" />
              {t(($) => $.graph.create_sub_issue_action)}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 rounded-lg text-xs"
              onClick={focusDescendants}
            >
              <Focus className="size-3.5" />
              {t(($) => $.graph.focus_descendants_action)}
            </Button>
          </div>
        </NodeToolbar>
      )}

      <Panel position="top-left" className="m-3">
        {!searchPanelOpen ? (
          <button
            type="button"
            className="flex h-9 max-w-[min(320px,calc(100vw-2rem))] items-center gap-2 rounded-xl border bg-card/92 px-3 text-left text-xs text-muted-foreground shadow-lg shadow-black/5 backdrop-blur-md transition-colors hover:border-foreground/15 hover:text-foreground"
            onClick={() => setSearchPanelOpen(true)}
            aria-label={t(($) => $.graph.search_panel_expand)}
          >
            <Search className="size-3.5 shrink-0" />
            <span className="min-w-0 truncate">
              {searchQuery
                ? searchMatches.length === 0
                  ? t(($) => $.graph.search_no_results)
                  : t(($) => $.graph.search_results_count, {
                      count: searchMatches.length,
                    })
                : t(($) => $.graph.issue_count, { count: graphIssues.length })}
            </span>
            {searchMatches.length > 0 && (
              <span className="shrink-0 tabular-nums">
                {t(($) => $.graph.search_current_result, {
                  current: selectedSearchIndex >= 0 ? selectedSearchIndex + 1 : 0,
                  total: searchMatches.length,
                })}
              </span>
            )}
            <ChevronDown className="ml-auto size-3.5 shrink-0" />
          </button>
        ) : (
          <div className="w-80 rounded-xl border bg-card/92 p-1.5 shadow-lg shadow-black/5 backdrop-blur-md">
            <div className="flex items-center gap-2">
              <div className="relative min-w-0 flex-1">
                <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={search}
                  onChange={(event) => {
                    clearInitialHighlight();
                    setSearch(event.target.value);
                  }}
                  onKeyDown={(event) => {
                    if (event.key === "Enter") handleSearch();
                    if (event.key === "ArrowDown") {
                      event.preventDefault();
                      focusSearchMatch(
                        selectedSearchIndex >= 0 ? selectedSearchIndex + 1 : 0,
                      );
                    }
                    if (event.key === "ArrowUp") {
                      event.preventDefault();
                      focusSearchMatch(
                        selectedSearchIndex >= 0
                          ? selectedSearchIndex - 1
                          : searchMatches.length - 1,
                      );
                    }
                  }}
                  placeholder={t(($) => $.graph.search_placeholder)}
                  className="h-8 border-0 bg-muted/55 pl-8 pr-8 text-xs shadow-none focus-visible:ring-1"
                />
                {search && (
                  <button
                    type="button"
                    onClick={() => {
                      clearInitialHighlight();
                      setSearch("");
                    }}
                    className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                    aria-label={t(($) => $.graph.search_clear)}
                  >
                    <X className="size-3.5" />
                  </button>
                )}
              </div>
              <Button
                size="icon-sm"
                variant="ghost"
                onClick={() =>
                  focusSearchMatch(
                    selectedSearchIndex >= 0
                      ? selectedSearchIndex - 1
                      : searchMatches.length - 1,
                  )
                }
                disabled={!searchQuery || searchMatches.length === 0}
                title={t(($) => $.graph.search_previous)}
              >
                <ChevronUp className="size-3.5" />
              </Button>
              <Button
                size="icon-sm"
                variant="ghost"
                onClick={handleSearch}
                disabled={!searchQuery || searchMatches.length === 0}
                title={t(($) => $.graph.search_next)}
              >
                <ChevronDown className="size-3.5" />
              </Button>
              <Button
                size="icon-sm"
                variant="ghost"
                onClick={() => setSearchPanelOpen(false)}
                title={t(($) => $.graph.search_panel_collapse)}
              >
                <X className="size-3.5" />
              </Button>
            </div>
            <div className="mt-1.5 flex items-center justify-between px-1.5 text-[11px] text-muted-foreground">
              {searchQuery ? (
                <span>
                  {searchMatches.length === 0
                    ? t(($) => $.graph.search_no_results)
                    : t(($) => $.graph.search_results_count, {
                        count: searchMatches.length,
                      })}
                </span>
              ) : (
                <span>
                  {t(($) => $.graph.issue_count, { count: graphIssues.length })}
                </span>
              )}
              {searchMatches.length > 0 && (
                <span className="tabular-nums">
                  {t(($) => $.graph.search_current_result, {
                    current:
                      selectedSearchIndex >= 0 ? selectedSearchIndex + 1 : 0,
                    total: searchMatches.length,
                  })}
                </span>
              )}
            </div>
            <div className="mt-1 flex items-center justify-between gap-2 px-1">
              <div className="flex min-w-0 items-center gap-1">
                <Button
                  type="button"
                  variant={showRelatedOnly ? "secondary" : "ghost"}
                  size="xs"
                  className="h-6 rounded-full text-[11px]"
                  onClick={() => {
                    clearInitialHighlight();
                    setShowRelatedOnly(true);
                  }}
                >
                  {t(($) => $.graph.related_only)}
                </Button>
                <Button
                  type="button"
                  variant={showRelatedOnly ? "ghost" : "secondary"}
                  size="xs"
                  className="h-6 rounded-full text-[11px]"
                  onClick={() => {
                    clearInitialHighlight();
                    setShowRelatedOnly(false);
                  }}
                >
                  {t(($) => $.graph.all_issues)}
                </Button>
              </div>
              <Button
                type="button"
                size="xs"
                variant="outline"
                className="h-6 shrink-0 rounded-full text-[11px]"
                onClick={resetView}
              >
                <RotateCcw className="size-3" />
                {t(($) => $.graph.reset_view)}
              </Button>
            </div>
            {hiddenUnrelatedCount > 0 && (
              <div className="px-1 pt-1 text-[11px] text-muted-foreground">
                {t(($) => $.graph.hidden_unrelated_count, {
                  count: hiddenUnrelatedCount,
                })}
              </div>
            )}
            {searchQuery && searchMatches.length > 0 && (
              <div className="mt-1 max-h-56 overflow-y-auto rounded-lg border bg-background/80 p-1">
                {searchMatches.map((issue, index) => (
                  <button
                    key={issue.id}
                    type="button"
                    className={cn(
                      "flex w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs transition-colors hover:bg-accent/60",
                      issue.id === selectedId &&
                        "bg-accent text-accent-foreground",
                    )}
                    onClick={() => focusSearchMatch(index)}
                  >
                    <span className="w-14 shrink-0 tabular-nums text-muted-foreground">
                      {issue.identifier}
                    </span>
                    <span className="truncate">{issue.title}</span>
                  </button>
                ))}
              </div>
            )}
          </div>
        )}
      </Panel>

      <Panel position="top-right" className="m-3">
        <div
          className={cn(
            "rounded-lg border bg-card/88 px-3 py-2 text-[11px] text-muted-foreground shadow-sm backdrop-blur-md transition-opacity",
            (selectedId || showInitialSpotlight) ? "opacity-100" : "opacity-75",
          )}
        >
          {selectedId || showInitialSpotlight ? (
            <div className="flex items-center gap-3">
              <span className="inline-flex items-center gap-1 text-info">
                <ArrowUpLeft className="size-3" />
                {t(($) => $.graph.ancestor_count, {
                  count: selectedId
                    ? relationshipTrace.ancestors.size
                    : latestCreatedTrace.ancestors.size,
                })}
              </span>
              <span className="size-1 rounded-full bg-brand" />
              <span className="inline-flex items-center gap-1 text-success">
                <ArrowDownRight className="size-3" />
                {t(($) => $.graph.descendant_count, {
                  count: selectedId
                    ? relationshipTrace.descendants.size
                    : latestCreatedTrace.descendants.size,
                })}
              </span>
              <span className="text-muted-foreground">
                {selectedId
                  ? t(($) => $.graph.selected_hint)
                  : t(($) => $.graph.initial_latest_hint)}
              </span>
            </div>
          ) : (
            t(($) => $.graph.default_hint)
          )}
        </div>
      </Panel>
    </ReactFlow>
  );
}

export function IssueGraphView(props: {
  issues: Issue[];
  matchedIssueIds: Set<string>;
  childProgressMap: Map<string, { done: number; total: number }>;
}) {
  return (
    <div className="relative flex flex-1 min-h-0 overflow-hidden border-t">
      <ReactFlowProvider>
        <IssueGraphCanvas {...props} />
      </ReactFlowProvider>
    </div>
  );
}
