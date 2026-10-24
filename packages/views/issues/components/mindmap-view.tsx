"use client";

import { useCallback, useEffect, useId, useMemo, useRef, useState, type PointerEvent, type WheelEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, ExternalLink, Maximize2, Minus, Plus, RotateCcw } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { issueDependenciesOptions, issueKeys } from "@multica/core/issues/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Issue, IssueTableQuerySpec } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { ProjectIcon } from "../../projects/components/project-icon";
import { useT } from "../../i18n";
import { StatusIcon } from "./status-icon";
import { buildMindmapLayout, mindmapQuery, MINDMAP_CARD_HEIGHT, MINDMAP_CARD_WIDTH } from "./mindmap-layout";

type Transform = { x: number; y: number; scale: number };
const MIN_SCALE = 0.25;
const MAX_SCALE = 2;

export function MindmapView({
  serverQuery,
  exportIssues,
  search,
  onSearchChange,
}: {
  serverQuery: IssueTableQuerySpec;
  exportIssues: (query?: IssueTableQuerySpec) => Promise<Issue[]>;
  search: string;
  onSearchChange: (value: string) => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { colorOf, iconOf } = useIssueStatuses(wsId);
  const markerId = useId().replace(/:/g, "");
  const viewportRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{ x: number; y: number } | null>(null);
  const fittedRef = useRef(false);
  const [expanded, setExpanded] = useState<Map<string, boolean>>(() => new Map());
  const [transform, setTransform] = useState<Transform>({ x: 24, y: 24, scale: 1 });
  const [selected, setSelected] = useState<string | null>(null);

  const mapQuery = useMemo(() => mindmapQuery(serverQuery), [serverQuery]);

  const issuesQuery = useQuery({
    queryKey: issueKeys.mindmap(wsId, mapQuery),
    queryFn: () => exportIssues(mapQuery),
    retry: false,
  });
  const projectsQuery = useQuery(projectListOptions(wsId));
  const dependenciesQuery = useQuery(issueDependenciesOptions(wsId));
  const ready = issuesQuery.isSuccess && projectsQuery.isSuccess &&
    dependenciesQuery.isSuccess && dependenciesQuery.data.complete;
  const effectiveExpanded = useMemo(() => {
    const overrides = new Map(expanded);
    const term = search.trim().toLocaleLowerCase();
    if (!term || !issuesQuery.data) return overrides;
    const byId = new Map(issuesQuery.data.map((issue) => [issue.id, issue]));
    for (const issue of issuesQuery.data) {
      if (!`${issue.identifier} ${issue.title}`.toLocaleLowerCase().includes(term)) continue;
      const projectKey = `project:${issue.project_id ?? "unassigned"}`;
      if (!overrides.has(projectKey)) overrides.set(projectKey, true);
      const seen = new Set([issue.id]);
      let parentId = issue.parent_issue_id;
      while (parentId && !seen.has(parentId)) {
        const parent = byId.get(parentId);
        if (!parent || parent.project_id !== issue.project_id) break;
        const parentKey = `issue:${parentId}`;
        if (!overrides.has(parentKey)) overrides.set(parentKey, true);
        seen.add(parentId);
        parentId = parent.parent_issue_id;
      }
    }
    return overrides;
  }, [expanded, issuesQuery.data, search]);
  const layout = useMemo(
    () => ready
      ? buildMindmapLayout(issuesQuery.data, projectsQuery.data, dependenciesQuery.data.dependencies, effectiveExpanded)
      : null,
    [ready, issuesQuery.data, projectsQuery.data, dependenciesQuery.data, effectiveExpanded],
  );
  const nodeByKey = useMemo(() => new Map(layout?.nodes.map((node) => [node.key, node]) ?? []), [layout]);
  const searchPath = useMemo(() => {
    const matching = new Set<string>();
    const ancestors = new Set<string>();
    const term = search.trim().toLocaleLowerCase();
    if (!layout || !term) return { matching, ancestors, active: false };
    for (const node of layout.nodes) {
      if (!node.issue || !`${node.issue.identifier} ${node.issue.title}`.toLocaleLowerCase().includes(term)) continue;
      matching.add(node.key);
      let parentKey = node.parentKey;
      while (parentKey) {
        ancestors.add(parentKey);
        parentKey = nodeByKey.get(parentKey)?.parentKey ?? null;
      }
    }
    return { matching, ancestors, active: true };
  }, [layout, nodeByKey, search]);

  const fit = useCallback(() => {
    const viewport = viewportRef.current;
    if (!viewport || !layout) return;
    const scale = Math.max(MIN_SCALE, Math.min(1, (viewport.clientWidth - 40) / layout.width, (viewport.clientHeight - 40) / layout.height));
    setTransform({
      scale,
      x: (viewport.clientWidth - layout.width * scale) / 2,
      y: (viewport.clientHeight - layout.height * scale) / 2,
    });
  }, [layout]);

  useEffect(() => {
    if (!layout || fittedRef.current) return;
    fittedRef.current = true;
    const frame = requestAnimationFrame(fit);
    return () => cancelAnimationFrame(frame);
  }, [fit, layout]);

  const zoom = useCallback((factor: number, clientX?: number, clientY?: number) => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    const bounds = viewport.getBoundingClientRect();
    const anchorX = (clientX ?? bounds.left + bounds.width / 2) - bounds.left;
    const anchorY = (clientY ?? bounds.top + bounds.height / 2) - bounds.top;
    setTransform((current) => {
      const scale = Math.max(MIN_SCALE, Math.min(MAX_SCALE, current.scale * factor));
      const ratio = scale / current.scale;
      return {
        scale,
        x: anchorX - (anchorX - current.x) * ratio,
        y: anchorY - (anchorY - current.y) * ratio,
      };
    });
  }, []);

  const onWheel = (event: WheelEvent<HTMLDivElement>) => {
    if (!event.ctrlKey && !event.metaKey) return;
    event.preventDefault();
    zoom(Math.exp(-event.deltaY * 0.01), event.clientX, event.clientY);
  };
  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 || (event.target as HTMLElement).closest("[data-mindmap-node]")) return;
    dragRef.current = { x: event.clientX, y: event.clientY };
    event.currentTarget.setPointerCapture(event.pointerId);
  };
  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    const previous = dragRef.current;
    if (!previous) return;
    setTransform((current) => ({ ...current, x: current.x + event.clientX - previous.x, y: current.y + event.clientY - previous.y }));
    dragRef.current = { x: event.clientX, y: event.clientY };
  };
  const onPointerUp = (event: PointerEvent<HTMLDivElement>) => {
    dragRef.current = null;
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
  };

  if (issuesQuery.isError || projectsQuery.isError || dependenciesQuery.isError ||
      (dependenciesQuery.isSuccess && !dependenciesQuery.data.complete)) {
    return <div className="flex flex-1 flex-col items-center justify-center gap-3 text-muted-foreground">
      <p>{t(($) => $.mindmap.load_failed)}</p>
      <Button variant="outline" size="sm" onClick={() => {
        void issuesQuery.refetch();
        void projectsQuery.refetch();
        void dependenciesQuery.refetch();
      }}>{t(($) => $.mindmap.retry)}</Button>
    </div>;
  }
  if (!layout) {
    return <div className="flex flex-1 items-center justify-center text-muted-foreground" role="status">{t(($) => $.mindmap.loading)}</div>;
  }

  return <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden bg-page-canvas">
    <div className="absolute left-4 top-4 z-10 w-64">
      <Input aria-label={t(($) => $.table.search_placeholder)} placeholder={t(($) => $.table.search_placeholder)} value={search} onChange={(event) => onSearchChange(event.target.value)} />
    </div>
    <div ref={viewportRef} className="relative min-h-0 flex-1 overflow-hidden touch-none cursor-grab active:cursor-grabbing"
      onWheel={onWheel} onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerCancel={onPointerUp}
      aria-label={t(($) => $.mindmap.canvas)}>
      {layout.nodes.length === 0 ? <div className="absolute inset-0 flex items-center justify-center text-muted-foreground">{t(($) => $.mindmap.empty)}</div> : null}
      <div className="absolute left-0 top-0 origin-top-left" style={{ width: layout.width, height: layout.height, transform: `translate(${transform.x}px, ${transform.y}px) scale(${transform.scale})` }}>
        <svg aria-hidden="true" className="absolute inset-0 pointer-events-none" width={layout.width} height={layout.height}>
          <defs><marker id={markerId} markerWidth="9" markerHeight="9" refX="8" refY="4.5" orient="auto" markerUnits="userSpaceOnUse"><path d="M0 0 L9 4.5 L0 9 Z" fill="currentColor" className="text-warning" /></marker></defs>
          {layout.hierarchyEdges.map((edge) => {
            const from = nodeByKey.get(edge.from)!;
            const to = nodeByKey.get(edge.to)!;
            const x1 = from.x + MINDMAP_CARD_WIDTH;
            const x2 = to.x;
            const y1 = from.y + MINDMAP_CARD_HEIGHT / 2;
            const y2 = to.y + MINDMAP_CARD_HEIGHT / 2;
            return <path key={`${edge.from}-${edge.to}`} d={`M${x1} ${y1} C${(x1 + x2) / 2} ${y1},${(x1 + x2) / 2} ${y2},${x2} ${y2}`} fill="none" stroke="currentColor" className="text-surface-border" strokeWidth="2" />;
          })}
          {layout.dependencyEdges.map((edge) => {
            const from = nodeByKey.get(edge.from)!;
            const to = nodeByKey.get(edge.to)!;
            const forward = to.x > from.x;
            const x1 = forward ? from.x + MINDMAP_CARD_WIDTH : from.x + MINDMAP_CARD_WIDTH / 2;
            const x2 = forward ? to.x - 10 : to.x + MINDMAP_CARD_WIDTH / 2;
            const y1 = from.y + MINDMAP_CARD_HEIGHT / 2;
            const y2 = to.y + MINDMAP_CARD_HEIGHT / 2;
            const bend = forward ? (x1 + x2) / 2 : Math.min(y1, y2) - 70;
            const path = forward
              ? `M${x1} ${y1} C${bend} ${y1},${bend} ${y2},${x2} ${y2}`
              : `M${x1} ${from.y} C${x1} ${bend},${x2} ${bend},${x2} ${to.y - 10}`;
            return <path key={`${edge.from}-${edge.to}`} d={path} fill="none" stroke="currentColor" className="text-warning" strokeWidth="2" strokeDasharray="6 5" markerEnd={`url(#${markerId})`} />;
          })}
        </svg>
        {layout.nodes.map((node) => {
          const issue = node.issue;
          const hidden = issue ? layout.hiddenDependencyCount.get(issue.id) ?? 0 : 0;
          const dimmed = searchPath.active && !searchPath.matching.has(node.key) && !searchPath.ancestors.has(node.key);
          return <div key={node.key} data-mindmap-node="" className={`absolute flex flex-col justify-center rounded-lg border bg-surface px-3 shadow-sm ${selected === node.key ? "border-primary bg-surface-selected" : "border-surface-border hover:bg-surface-hover"} ${dimmed ? "opacity-40" : ""}`}
            style={{ left: node.x, top: node.y, width: MINDMAP_CARD_WIDTH, height: MINDMAP_CARD_HEIGHT }} onClick={() => setSelected(node.key)}>
            <div className="flex min-w-0 items-center gap-2">
              {node.childCount > 0 ? <button type="button" aria-label={node.collapsed ? t(($) => $.mindmap.expand) : t(($) => $.mindmap.collapse)} aria-expanded={!node.collapsed}
                className="shrink-0 rounded-sm p-1 hover:bg-surface-hover focus-visible:outline-2 focus-visible:outline-primary"
                onClick={(event) => { event.stopPropagation(); setExpanded((current) => new Map(current).set(node.key, node.collapsed)); }}>
                <ChevronRight className={`size-3.5 ${node.collapsed ? "" : "rotate-90"}`} />
              </button> : <span className="w-5 shrink-0" />}
              {issue ? <StatusIcon status={issue.status} color={colorOf(issue.status)} icon={iconOf(issue.status)} className="size-3.5 shrink-0" /> : <ProjectIcon project={node.project} />}
              <span className="truncate text-caption text-muted-foreground">{issue?.identifier}</span>
              {issue ? <AppLink href={paths.issueDetail(issue.id)} className="ml-auto shrink-0 text-muted-foreground hover:text-foreground" aria-label={`${t(($) => $.mindmap.open)} ${issue.identifier}`}><ExternalLink className="size-3.5" /></AppLink> : null}
            </div>
            {issue ? <AppLink href={paths.issueDetail(issue.id)} className="mt-1 truncate text-body font-medium hover:underline" title={issue.title}>{issue.title}</AppLink>
              : <div className="mt-1 truncate text-body font-medium" title={node.label || t(($) => $.mindmap.unassigned)}>{node.label || t(($) => $.mindmap.unassigned)}</div>}
            {(node.childCount > 0 || hidden > 0) && <div className="mt-0.5 truncate text-caption text-muted-foreground">
              {node.childCount > 0 ? `${t(($) => node.kind === "project" ? $.mindmap.tasks : $.mindmap.children)}: ${node.childCount}` : ""}
              {hidden > 0 ? ` · ${t(($) => $.mindmap.hidden_dependencies)}: ${hidden}` : ""}
            </div>}
          </div>;
        })}
      </div>
    </div>
    <div className="absolute bottom-4 left-4 z-10 flex items-center gap-3 rounded-lg border border-surface-border bg-surface px-3 py-2 text-caption text-muted-foreground">
      <span className="inline-block w-5 border-t-2 border-surface-border" />{t(($) => $.mindmap.hierarchy)}
      <span className="inline-block w-5 border-t-2 border-dashed border-warning" />{t(($) => $.mindmap.dependency)}
    </div>
    <div className="absolute bottom-4 right-4 z-10 flex items-center rounded-lg border border-surface-border bg-surface p-1 shadow-sm">
      <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.mindmap.zoom_out)} onClick={() => zoom(1 / 1.2)} disabled={transform.scale <= MIN_SCALE}><Minus /></Button>
      <span className="min-w-12 text-center text-caption tabular-nums">{Math.round(transform.scale * 100)}%</span>
      <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.mindmap.zoom_in)} onClick={() => zoom(1.2)} disabled={transform.scale >= MAX_SCALE}><Plus /></Button>
      <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.mindmap.fit)} onClick={fit}><Maximize2 /></Button>
      <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.mindmap.reset)} onClick={() => setTransform({ x: 24, y: 24, scale: 1 })}><RotateCcw /></Button>
    </div>
  </div>;
}
