import type { CSSProperties } from "react";
import { trajectorySymbol, type TrajectoryGraph, type TrajectoryNode } from "@multica/core/trajectory/graph";
import { loadedIssueContextIds } from "@multica/core/trajectory/record-display";
import { buildTrajectoryVisual, layoutTrajectoryRunCalls, type TrajectoryOrder, type VisualRow } from "@multica/core/trajectory/visual";
import type { CurrentExecutionWindow } from "@multica/core/trajectory/current-execution";
import { Button } from "@multica/ui/components/ui/button";
import { Bot, ChevronRight, Code, Info, ListChecks, Search, TriangleAlert } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { ProviderLogo } from "../../../../runtimes/components/provider-logo";
import { useLocale, useT } from "../../../../i18n";
import { GraphProvenance, GraphSymbol } from "./graph-symbol";
import "./graph-ui.css";

const string = (value: unknown) => typeof value === "string" ? value : "";
const railColor = (index: number | null) => index === null ? "var(--trajectory-border)" : `var(--trajectory-identity-${index % 8 + 1})`;
function roleOf(name: string) {
  if (/review|audit|审/i.test(name)) return { Icon: Search, tone: "reviewer" };
  if (/test|qa|验/i.test(name)) return { Icon: ListChecks, tone: "tester" };
  if (/plan|architect|coord|lead|规划/i.test(name)) return { Icon: Bot, tone: "planner" };
  return { Icon: Code, tone: "implementer" };
}
function providerOf(models: string) {
  if (/sonnet|opus|claude/i.test(models)) return { name: "claude", tone: "anthropic" };
  if (/kimi|moonshot/i.test(models)) return { name: "kimi", tone: "moonshot" };
  if (/composer|cursor/i.test(models)) return { name: "cursor", tone: "cursor" };
  if (/gemini/i.test(models)) return { name: "gemini", tone: "gemini" };
  if (/openrouter/i.test(models)) return { name: "openrouter", tone: "openrouter" };
  return { name: "codex", tone: "codex" };
}
function laneSpans(row: VisualRow) {
  const spans = new Map<string, { laneId: string; column: number; x: number; from: number; to: number }>();
  for (const segment of row.segments) {
    if (segment.laneId === null || segment.colorIndex === null) continue;
    const key = `${segment.laneId}:${segment.column}`;
    const previous = spans.get(key);
    spans.set(key, { laneId: segment.laneId, column: segment.column, x: segment.x,
      from: Math.min(previous?.from ?? segment.fromY, segment.fromY), to: Math.max(previous?.to ?? segment.toY, segment.toY) });
  }
  return [...spans.values()];
}
function runDuration(node: TrajectoryNode): number | null {
  const start = Date.parse(string(node.details.started_at));
  const end = Date.parse(string(node.details.completed_at));
  return Number.isFinite(start) && Number.isFinite(end) && end >= start ? Math.round((end - start) / 1000) : null;
}

export function GraphTimeline({ graph, projectKey, issueIds, expandedIssues, expandedRuns, selectedId, execution, focusCurrent = false, locatedIssues, order = "task", toggleIssue, toggleRun, select }: {
  graph: TrajectoryGraph; projectKey: string; issueIds: string[]; expandedIssues: Set<string>; expandedRuns: Set<string>; selectedId?: string | null; order?: TrajectoryOrder;
  toggleIssue: (id: string) => void; toggleRun: (id: string) => void; select: (id: string) => void;
  execution?: CurrentExecutionWindow; focusCurrent?: boolean; locatedIssues?: ReadonlySet<string>;
}) {
  const { t } = useT("issues");
  const locale = useLocale();
  const projectIssues = new Set(graph.nodes.filter(node => node.kind === "issue" && (projectKey === "none" ? node.details.project_id === null : projectKey === "unknown" ? node.details.project_id === undefined : node.details.project_id === projectKey)).map(node => node.issue_id));
  for (const id of loadedIssueContextIds(graph, issueIds)) projectIssues.add(id);
  const scene = buildTrajectoryVisual({ ...graph, nodes: graph.nodes.filter(node => projectIssues.has(node.issue_id)) }, new Set(issueIds), expandedIssues, expandedRuns, order);
  const calls = layoutTrajectoryRunCalls(graph, scene);
  const callLabels = new Map(scene.rows.flatMap(({ kind, node }) => kind === "run" && node
    ? [[node.id, `${node.title || t($ => $.trajectory_detail.run)} · ${node.run_id?.slice(-8) ?? ""}`] as const] : []));
  const laneOrigins = new Map(scene.rows.flatMap(row => row.joins.flatMap(join => row.node ? [[join.laneId, row.node.id] as const] : [])));
  const railOpacity = (laneId: string | null) => {
    if (!focusCurrent || !execution || !laneId) return 1;
    const owner = laneId.startsWith("issue-branch:") ? laneId.slice("issue-branch:".length) : graph.nodes.find(node => node.id === laneOrigins.get(laneId))?.issue_id;
    return owner && (execution.focusIssueIds.has(owner) || execution.ancestorIssueIds.has(owner)) ? 1 : 0.3;
  };
  return <div className="trajectory-theme trajectory-timeline" data-testid="trajectory-timeline">
    <div className="trajectory-stream">
      <div className="trajectory-track" style={{ minWidth: calls.width + 96 }}>
        <svg className="trajectory-rail" data-testid="trajectory-rail" aria-hidden="true" width={scene.railWidth} height={scene.height}>
          {scene.rows.map(row => <g key={row.id} transform={`translate(0 ${row.top})`}>
            {row.segments.map((segment, index) => <path key={`line:${index}`} d={`M ${segment.x} ${segment.y1} V ${segment.y2}`}
              fill="none" stroke={railColor(segment.colorIndex)} opacity={railOpacity(segment.laneId)} strokeWidth={segment.strokeWidth} strokeDasharray={segment.dash === "dashed" ? "3 3" : undefined}/>) }
            {row.joins.map((join, index) => <path key={`join:${index}`} data-parent={row.parentId ?? join.laneId} d={join.path}
              fill="none" stroke={railColor(join.colorIndex)} opacity={railOpacity(join.laneId)} strokeWidth={join.strokeWidth} strokeDasharray={join.dash === "dashed" ? "3 3" : undefined}/>) }
          </g>)}
        </svg>
        {calls.links.length > 0 && <svg className="trajectory-rail" role="group" aria-label={t($ => $.trajectory_graph.run_calls)} width={calls.width} height={scene.height}>
          {calls.links.map(({ from, to, colorIndex, start, path, tip }) => <g key={to} role="img" aria-label={`${callLabels.get(from)} → ${callLabels.get(to)}`} data-testid="trajectory-run-link" data-from={from} data-to={to} style={{ color: railColor(colorIndex) }}>
            <path d={path} fill="none" stroke="currentColor" strokeWidth={1.5}/>
            <circle cx={start.x} cy={start.y} r={3} fill="currentColor"/>
            <polygon points={tip} fill="currentColor"/>
          </g>)}
        </svg>}
        {scene.rows.map(row => {
          if (row.kind === "day") return <div key={row.id} className="trajectory-day" style={{ height: row.height, "--trajectory-rail-width": `${calls.width}px` } as CSSProperties}>
            <time dateTime={row.dayAt}>{row.dayAt ? new Date(row.dayAt).toLocaleDateString(locale, { year: "numeric", month: "2-digit", day: "2-digit" }) : ""}</time>
          </div>;
          const node = row.node!;
          const isIssue = row.kind === "issue", isRun = row.kind === "run", isCount = row.kind === "outputs";
          const expanded = isIssue ? expandedIssues.has(node.issue_id) : expandedRuns.has(node.run_id ?? node.id) || expandedRuns.has(node.id);
          const models = Array.isArray(node.details.models) ? node.details.models.filter(value => typeof value === "string" && value.length > 0).join(", ") : "";
          const seconds = runDuration(node);
          const duration = seconds === null ? "" : seconds < 60 ? `${seconds}s` : `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
          const role = roleOf(string(node.details.agent_name));
          const provider = providerOf(models);
          const findings = isRun ? graph.findings.filter(finding => finding.node_id === node.id) : [];
          const findingLabel = findings.map(finding => finding.code === "missing_input_snapshot" ? `${t($ => $.trajectory.snapshot)} · ${t($ => $.trajectory.missing)}` : finding.code.replaceAll("_", " ")).join(", ");
          const created = new Date(node.created_at), validCreated = Number.isFinite(created.getTime());
          const title = node.title || t($ => $.trajectory.unknown);
          const statusSymbol = trajectorySymbol(node);
          const statusLabel = statusSymbol.meaning === "unknown" ? node.status : t($ => $.trajectory_graph.symbols[statusSymbol.meaning]);
          const countLabel = [row.artifactCount ? `${row.artifactCount} · ${t($ => $.trajectory_graph.artifact)}` : "", row.checkCount ? `${row.checkCount} · ${t($ => $.trajectory_graph.check)}` : ""].filter(Boolean).join(" · ");
          const currentState = isIssue && execution ? (["running", "blocked", "review"] as const).find(category => execution.categories[category].includes(node.issue_id)) : undefined;
          const focusRole = execution?.focusIssueIds.has(node.issue_id) ? "current" : isIssue && execution?.ancestorIssueIds.has(node.issue_id) ? "ancestor" : "history";
          return <div key={row.id} className="trajectory-row" data-node-id={row.id} data-issue-owner={node.issue_id} data-grade={row.grade} data-status-tone={trajectorySymbol(node).tone}
            data-current-state={focusCurrent ? currentState : undefined} data-focus-role={focusCurrent ? focusRole : undefined} data-summary-target={isIssue && locatedIssues?.has(node.issue_id) || undefined}
            data-owner-tint={!isIssue || undefined} style={{ height: row.height, ...!isIssue && { "--trajectory-owner": railColor(row.colorIndex) } } as CSSProperties}>
            {laneSpans(row).map(span => {
              const issueId = span.laneId.startsWith("issue-branch:") ? span.laneId.slice("issue-branch:".length) : null;
              const target = graph.nodes.find(item => item.id === (issueId ? `issue:${issueId}` : laneOrigins.get(span.laneId)));
              return target && <button key={`${span.laneId}:${span.column}`} type="button" tabIndex={-1} className="trajectory-lane-hit" aria-label={target.title} title={target.title}
                style={{ left: span.x - 6, top: span.from, width: 12, height: span.to - span.from }} onClick={() => issueId ? toggleIssue(issueId) : select(target.id)}/>;
            })}
            {!isCount && <span className="trajectory-clock" style={{ height: row.contentHeight }} title={isIssue ? node.details.issue_key : node.created_at}>
              {isIssue ? node.details.issue_key : validCreated ? <time dateTime={node.created_at}>{created.toLocaleTimeString(locale, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" })}</time> : t($ => $.trajectory.unknown)}
            </span>}
            {row.markerY !== null && <span className="trajectory-marker" data-marker-column={row.markerColumn} style={{ left: row.markerX, top: row.markerY }}>
              {isCount ? <span className="trajectory-count-marker" style={{ borderColor: railColor(row.colorIndex) }}/>
                : <GraphSymbol node={node}/>}
            </span>}
            <div className="trajectory-row-content" data-row-content style={{ marginLeft: calls.width }}>
              {isCount ? <button type="button" className="trajectory-content trajectory-count" style={{ height: row.contentHeight }} aria-expanded={expanded}
                onClick={() => toggleRun(node.run_id ?? node.id)} onKeyDown={event => { if (event.key === "ArrowRight" && !expanded) { event.preventDefault(); toggleRun(node.run_id ?? node.id); } }}>{countLabel}</button>
                : <>
                  <button type="button" className="trajectory-content" style={{ height: row.contentHeight }}
                    aria-label={`${isIssue ? expanded ? t($ => $.trajectory.collapse) : t($ => $.trajectory.expand) : t($ => $.trajectory.detail)} ${title}`}
                    aria-expanded={isIssue ? expanded : undefined} aria-pressed={isIssue ? undefined : selectedId === node.id}
                    onClick={() => isIssue ? toggleIssue(node.issue_id) : select(node.id)}>
                    <span className="trajectory-label">
                      {isIssue && <ChevronRight className={cn("size-3.5 shrink-0 transition-transform motion-reduce:transition-none", expanded && "rotate-90")}/>}
                      {isRun && <span className="trajectory-role" style={{ "--trajectory-role": `var(--trajectory-${role.tone})` } as CSSProperties} title={string(node.details.agent_name)}><role.Icon strokeWidth={1.5}/></span>}
                      {(isIssue && !row.parentId || isRun) && <span className="trajectory-kind">{isIssue ? t($ => $.trajectory.issue) : row.continuationKind === "retry" ? t($ => $.trajectory.retry) : row.continuationKind === "rerun" ? t($ => $.trajectory_graph.rerun) : t($ => $.trajectory_graph.run_label)}</span>}
                      <GraphProvenance node={node}/>
                      <span className={cn("trajectory-title", isIssue ? "trajectory-title-issue" : row.grade === "entry" && "trajectory-title-entry", !isIssue && !isRun && "trajectory-title-record")} style={isRun && node.status === "running" ? { fontWeight: 500 } : undefined} title={title}>{title}</span>
                      {isIssue && node.details.stage != null && <span className="trajectory-stage">{t($ => $.stage.value, { n: node.details.stage })}</span>}
                      {isIssue && row.context && <span className="trajectory-kind">{t($ => $.trajectory.context)}</span>}
                      {isRun && <span className="trajectory-issue-status shrink-0" title={node.status}>{statusLabel}</span>}
                      {!isIssue && !isRun && <span className="trajectory-kind trajectory-record-status">{string(node.details.version) || node.status}</span>}
                    </span>
                    {isIssue && <span className="trajectory-issue-meta"><span className="trajectory-issue-status" title={node.status}>{statusLabel}</span><span>{row.runCount === undefined ? t($ => $.trajectory.unknown) : t($ => $.trajectory_graph.run_count, { count: row.runCount })}</span></span>}
                    {isRun && <span className="trajectory-run-meta">
                      <span className="trajectory-model" data-meta-column="model" data-missing-model={!models || undefined} style={{ "--trajectory-provider": `var(--trajectory-provider-${provider.tone})` } as CSSProperties}>
                        {models && <ProviderLogo provider={provider.name}/>}
                        <span title={models || undefined}>{models || t($ => $.trajectory_graph.model_missing)}</span>
                      </span>
                      <span className="trajectory-duration" data-meta-column="time">{duration}</span>
                    </span>}
                  </button>
                  <span className="trajectory-row-menu" style={{ height: row.contentHeight }}>{isIssue ? <Button variant="ghost" size="icon-xs" aria-label={`${t($ => $.trajectory.detail)} ${title}`} onClick={() => select(node.id)}><Info className="size-3.5"/></Button>
                    : findings.length > 0 && <button type="button" className="trajectory-finding" aria-label={findingLabel} title={findings.map(finding => finding.message).join("\n")} style={{ color: `var(--trajectory-${findings.some(finding => finding.severity === "warning") ? "danger" : "warning"})` }} onClick={() => select(node.id)}><TriangleAlert className="size-3.5"/></button>}</span>
                </>}
            </div>
          </div>;
        })}
      </div>
    </div>
  </div>;
}
