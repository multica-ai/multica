import type { TrajectoryGraph, TrajectoryNode } from "./graph";
export { layoutTrajectoryRunCalls } from "./run-links";

export const TRAJECTORY_VISUAL_METRICS = {
  spineX: 8, columnWidth: 16, curveHandle: 8.84, markerRadius: 10, countRadius: 5,
  labelGap: 10, contentPadding: 8, edgeBleed: 1, gutterWidth: 64, gutterCollapseAt: 440,
  dayHeight: 48, dayRuleY: 24,
  grades: {
    entry: { height: 40, lineHeight: 20 }, step: { height: 32, lineHeight: 16 },
    pending: { height: 24, lineHeight: 16 }, fact: { height: 28, lineHeight: 16 },
    count: { height: 24, lineHeight: 16 },
  },
  gaps: { none: 0, sibling: 4, entry: 12, fact: 8 },
} as const;

export type VisualRowGrade = keyof typeof TRAJECTORY_VISUAL_METRICS.grades;
export interface VisualRailSegment {
  column: number;
  x: number;
  fromY: number;
  toY: number;
  /** Render coordinates include the one pixel solid-line bleed at row edges. */
  y1: number;
  y2: number;
  laneId: string | null;
  colorIndex: number | null;
  isMuted: boolean;
  dash: "solid" | "dashed";
  strokeWidth: number;
}
export interface VisualRailJoin {
  kind: "fork";
  spineColumn: number;
  laneColumn: number;
  anchorY: number;
  path: string;
  laneId: string;
  colorIndex: number;
  isMuted: boolean;
  dash: "solid" | "dashed";
  strokeWidth: number;
}
export interface VisualRow {
  id: string;
  kind: "day" | "issue" | "run" | "outputs" | "record";
  node?: TrajectoryNode;
  top: number;
  height: number;
  contentHeight: number;
  grade: VisualRowGrade;
  markerY: number | null;
  markerX: number;
  markerColumn: number;
  markerRadius: number;
  colorIndex: number | null;
  /** All x coordinates start at the rail origin, after the responsive gutter. */
  contentInset: number;
  railInset: number;
  depth: number;
  parentId?: string;
  context: boolean;
  expanded: boolean;
  runCount?: number;
  outputCount: number;
  artifactCount: number;
  checkCount: number;
  continuationKind?: "retry" | "rerun";
  dayLabel?: string;
  dayAt?: string;
  ruleY?: number;
  segments: VisualRailSegment[];
  joins: VisualRailJoin[];
}
export interface VisualScene {
  rows: VisualRow[];
  railWidth: number;
  height: number;
  gutterWidth: number;
  gutterCollapseAt: number;
  runCount: number;
}

interface Lane {
  id: string;
  parent: string | null;
  origin: string;
  identity: number;
}
interface Draft {
  row: VisualRow;
  lane: string | null;
  pending: boolean;
}
interface Span {
  lane: Lane;
  origin: number;
  members: number[];
  end: number;
  depth: number;
}

const stringId = (value: unknown): string => typeof value === "string" ? value : "";
const issueParent = (node: TrajectoryNode) => stringId(node.details.parent_id);
const time = (node: TrajectoryNode) => Date.parse(node.created_at) || 0;
const chronological = (a: TrajectoryNode, b: TrajectoryNode) => time(a) - time(b) || a.id.localeCompare(b.id);
const isPending = (node: TrajectoryNode) => ["queued", "pending", "dispatched", "running", "blocked"].includes(node.status);

export function trajectoryColumnX(column: number): number {
  return TRAJECTORY_VISUAL_METRICS.spineX + column * TRAJECTORY_VISUAL_METRICS.columnWidth;
}

/** Stable numeric identity; clients resolve the eight theme palette entries. */
export function trajectoryIdentityIndex(issueId: string): number {
  let hash = 2166136261;
  for (const character of issueId.split("")) hash = Math.imul(hash ^ character.charCodeAt(0), 16777619) >>> 0;
  return hash % 8;
}

function scopeIssues(nodes: TrajectoryNode[], matching: Set<string>): TrajectoryNode[] {
  const issues = new Map(nodes.filter(node => node.kind === "issue").map(node => [node.issue_id, node]));
  const children = new Map<string, string[]>();
  for (const issue of issues.values()) {
    const parent = issueParent(issue);
    children.set(parent, [...(children.get(parent) ?? []), issue.issue_id]);
  }
  const included = new Set<string>();
  const queue = [...matching];
  while (queue.length) {
    const id = queue.shift()!;
    if (included.has(id) || !issues.has(id)) continue;
    included.add(id);
    queue.push(...(children.get(id) ?? []));
  }
  for (const id of [...included]) {
    const visited = new Set([id]);
    let parent = issueParent(issues.get(id)!);
    while (issues.has(parent) && !visited.has(parent)) {
      included.add(parent);
      visited.add(parent);
      parent = issueParent(issues.get(parent)!);
    }
  }
  return [...included].map(id => issues.get(id)!).sort(chronological);
}

function makeRow(node: TrajectoryNode | undefined, kind: VisualRow["kind"], id: string, grade: VisualRowGrade): VisualRow {
  const contentHeight = TRAJECTORY_VISUAL_METRICS.grades[grade].height;
  const height = kind === "issue" ? 44 : kind === "day" ? 48 : contentHeight;
  return {
    id, kind, node, grade, top: 0, height, contentHeight,
    markerY: kind === "day" ? null : height / 2, markerX: 8, markerColumn: 0,
    markerRadius: kind === "outputs" ? 5 : 10, colorIndex: null, contentInset: 0, railInset: 0,
    depth: 0, context: false, expanded: false, outputCount: 0, artifactCount: 0, checkCount: 0,
    segments: [], joins: [],
  };
}

/**
 * `task`: each Issue is followed by its own Runs in time order, as one block, then by its
 * sub-Issues in time order, so an expanded Issue's Runs sit together right under it. `time`: within one root, Issues and Runs are
 * ordered by creation time alone, so sibling sub-Issues' Runs interleave on the time axis.
 */
export type TrajectoryOrder = "task" | "time";

function createRows(graph: TrajectoryGraph, matching: Set<string>, expandedIssues: Set<string>, expandedRuns: Set<string>, order: TrajectoryOrder) {
  const nodes = [...new Map(graph.nodes.map(node => [node.id, node])).values()];
  const issues = scopeIssues(nodes, matching);
  const included = new Map(issues.map(node => [node.issue_id, node]));
  const runsByIssue = new Map<string, TrajectoryNode[]>();
  for (const run of nodes.filter(node => node.kind === "run")) {
    runsByIssue.set(run.issue_id, [...(runsByIssue.get(run.issue_id) ?? []), run]);
  }
  for (const runs of runsByIssue.values()) runs.sort(chronological);
  const drafts: Draft[] = [];
  const lanes: Lane[] = [];
  const rootRanges: Array<[number, number]> = [];
  const visited = new Set<string>();
  const visit = (issue: TrajectoryNode, lane: string | null, depth: number, parentId?: string) => {
    if (visited.has(issue.issue_id)) return;
    visited.add(issue.issue_id);
    const row = makeRow(issue, "issue", issue.id, "entry");
    Object.assign(row, { depth, parentId, context: !matching.has(issue.issue_id), expanded: expandedIssues.has(issue.issue_id), runCount: issue.details.run_count });
    drafts.push({ row, lane, pending: false });
    const children = issues.filter(child => issueParent(child) === issue.issue_id && !visited.has(child.issue_id));
    const visibleRuns = row.expanded ? runsByIssue.get(issue.issue_id) ?? [] : [];
    // An Issue's own Runs come first, as one block right under it, then its sub-Issues; the time
    // order re-sorts the root's blocks afterwards. Runs also precede the sub-Issues they created.
    const events = [...visibleRuns, ...children];
    const runLanes = new Map<string, string | null>();
    for (const event of events) {
      if (event.kind === "issue") {
        const branch = `issue-branch:${event.issue_id}`;
        const creation = `run:${stringId(event.details.creation_run_id)}`;
        const origin = drafts.find(draft => draft.row.id === creation && draft.row.kind === "run" && draft.row.node?.issue_id === issue.issue_id);
        lanes.push({ id: branch, parent: lane, origin: origin?.row.id ?? issue.id, identity: trajectoryIdentityIndex(event.issue_id) });
        visit(event, branch, depth + 1, issue.issue_id);
        continue;
      }
      let runLane = lane;
      const related = graph.edges.find(edge => ["blocker", "retry", "rerun"].includes(edge.kind) && edge.to === event.id && runLanes.has(edge.from));
      const parentRun = `run:${stringId(event.details.parent_run)}`;
      const predecessor = related?.from ?? (runLanes.has(parentRun) ? parentRun : undefined);
      if (predecessor) {
        runLane = runLanes.get(predecessor) ?? lane;
        if (!related || related.kind === "blocker") {
          const branch = `run-branch:${event.id}`;
          lanes.push({ id: branch, parent: runLane, origin: predecessor, identity: trajectoryIdentityIndex(issue.issue_id) });
          runLane = branch;
        }
      }
      runLanes.set(event.id, runLane);
      const attachments = nodes.filter(output => ["artifact", "check"].includes(output.kind)
        && output.issue_id === issue.issue_id && !!event.run_id && output.run_id === event.run_id);
      const entry = makeRow(event, "run", event.id, isPending(event) ? "pending" : "step");
      Object.assign(entry, { depth: depth + 1, parentId: issue.issue_id, context: row.context,
        expanded: expandedRuns.has(event.id) || expandedRuns.has(event.run_id ?? event.id),
        colorIndex: trajectoryIdentityIndex(issue.issue_id), outputCount: attachments.length,
        artifactCount: attachments.filter(node => node.kind === "artifact").length,
        checkCount: attachments.filter(node => node.kind === "check").length,
        continuationKind: related && ["retry", "rerun"].includes(related.kind) ? related.kind : undefined });
      drafts.push({ row: entry, lane: runLane, pending: isPending(event) });
      if (!attachments.length) continue;
      const outputLane = `outputs:${event.id}`;
      lanes.push({ id: outputLane, parent: runLane, origin: event.id, identity: trajectoryIdentityIndex(issue.issue_id) });
      if (entry.expanded) {
        for (const attachment of attachments) {
          const record = makeRow(attachment, "record", attachment.id, "fact");
          Object.assign(record, { depth: depth + 2, parentId: event.id, context: row.context, colorIndex: entry.colorIndex });
          drafts.push({ row: record, lane: outputLane, pending: false });
        }
      } else {
        const count = makeRow(event, "outputs", outputLane, "count");
        Object.assign(count, { depth: depth + 2, parentId: event.id, context: row.context, colorIndex: entry.colorIndex,
          outputCount: entry.outputCount, artifactCount: entry.artifactCount, checkCount: entry.checkCount });
        drafts.push({ row: count, lane: outputLane, pending: false });
      }
    }
  };
  const emitRoot = (root: TrajectoryNode) => {
    const start = drafts.length;
    visit(root, null, 0);
    const blocks: Array<{ node: TrajectoryNode; rows: Draft[] }> = [];
    for (const draft of drafts.splice(start)) {
      if (draft.row.kind === "issue" || draft.row.kind === "run") blocks.push({ node: draft.row.node!, rows: [draft] });
      else blocks.at(-1)!.rows.push(draft);
    }
    // Blocks start in the depth-first order of visit(). In task order a day can therefore
    // recur within one root, so the rule id names the block it precedes.
    if (order === "time") blocks.sort((a, b) => a.node.id === root.id ? -1 : b.node.id === root.id ? 1 : chronological(a.node, b.node));
    const showDays = blocks.some(block => block.node.kind === "run");
    let previousDay = "";
    for (const block of blocks) {
      const at = new Date(block.node.created_at);
      const day = Number.isFinite(at.getTime()) ? at.toDateString() : "";
      if (showDays && day && day !== previousDay) {
        previousDay = day;
        const rule = makeRow(undefined, "day", `day:${root.issue_id}:${day}:${block.node.id}`, "fact");
        Object.assign(rule, { dayLabel: at.toLocaleDateString(), dayAt: block.node.created_at, ruleY: 24, contentHeight: 48 });
        drafts.push({ row: rule, lane: null, pending: false });
      }
      drafts.push(...block.rows);
    }
    if (drafts.length > start) rootRanges.push([start, drafts.length - 1]);
  };
  for (const root of issues.filter(issue => !included.has(issueParent(issue)))) emitRoot(root);
  // A malformed membership cycle still emits each stored identity only once.
  for (const issue of issues) if (!visited.has(issue.issue_id)) emitRoot(issue);
  return { drafts, lanes, rootRanges };
}

function allocateLanes(drafts: Draft[], lanes: Lane[]) {
  const byId = new Map(lanes.map(lane => [lane.id, lane]));
  const rowIndexes = new Map(drafts.map((draft, index) => [draft.row.id, index]));
  const members = new Map<string, number[]>();
  drafts.forEach((draft, index) => {
    if (draft.lane) members.set(draft.lane, [...(members.get(draft.lane) ?? []), index]);
  });
  const ancestry = (lane: Lane) => {
    const chain = [lane];
    while (chain.at(-1)!.parent && chain.length <= lanes.length) {
      const parent = byId.get(chain.at(-1)!.parent!);
      if (!parent || chain.includes(parent)) break;
      chain.push(parent);
    }
    return chain;
  };
  const spans: Span[] = [];
  for (const lane of lanes) {
    const origin = rowIndexes.get(lane.origin) ?? Math.max(0, drafts.length - 1);
    const after = (members.get(lane.id) ?? []).filter(index => index > origin);
    if (after.length) spans.push({ lane, origin, members: after, end: after.at(-1)!, depth: ancestry(lane).length - 1 });
  }
  const columns = new Map<string, number>();
  const occupied = new Map<number, Span[]>();
  const sorted = [...spans].sort((a, b) => a.depth - b.depth || a.origin - b.origin || a.lane.id.localeCompare(b.lane.id));
  for (const span of sorted) {
    let column = (span.lane.parent ? columns.get(span.lane.parent) ?? 0 : 0) + 1;
    while ((occupied.get(column) ?? []).some(other => other.origin <= span.end && span.origin <= other.end)) column++;
    columns.set(span.lane.id, column);
    occupied.set(column, [...(occupied.get(column) ?? []), span]);
  }
  return { spans, columns, ancestry };
}

function addSegment(row: VisualRow, column: number, fromY: number, toY: number, dash: VisualRailSegment["dash"], laneId: string | null, colorIndex: number | null) {
  row.segments.push({ column, x: trajectoryColumnX(column), fromY, toY, y1: fromY, y2: toY, laneId, colorIndex, isMuted: false, dash, strokeWidth: colorIndex === null ? 1 : 2 });
}

function combineSegments(segments: VisualRailSegment[]): VisualRailSegment[] {
  const byColumn = new Map<number, VisualRailSegment[]>();
  for (const segment of segments) byColumn.set(segment.column, [...(byColumn.get(segment.column) ?? []), segment]);
  const combined: VisualRailSegment[] = [];
  for (const column of [...byColumn.keys()].sort((a, b) => a - b)) {
    const ink = byColumn.get(column)!.sort((a, b) => a.fromY - b.fromY);
    const stops = [...new Set(ink.flatMap(segment => [segment.fromY, segment.toY]))].sort((a, b) => a - b);
    if (stops.length === 1) {
      combined.push({ ...ink[0]! });
      continue;
    }
    // Split at every endpoint, then select solid ink over pending ink on overlap.
    for (let index = 1; index < stops.length; index++) {
      const fromY = stops[index - 1]!;
      const toY = stops[index]!;
      const covering = ink.filter(segment => segment.fromY <= fromY && segment.toY >= toY);
      const segment = covering.find(candidate => candidate.dash === "solid") ?? covering[0];
      if (!segment) continue;
      const previous = combined.at(-1);
      if (previous?.column === column && previous.toY === fromY && previous.dash === segment.dash
        && previous.laneId === segment.laneId && previous.colorIndex === segment.colorIndex) previous.toY = toY;
      else combined.push({ ...segment, fromY, toY });
    }
  }
  return combined;
}

function paintRails(drafts: Draft[], lanes: Lane[], rootRanges: Array<[number, number]>) {
  const { spans, columns, ancestry } = allocateLanes(drafts, lanes);
  for (const draft of drafts) {
    const row = draft.row;
    row.markerColumn = draft.lane ? columns.get(draft.lane) ?? 0 : 0;
    row.markerX = trajectoryColumnX(row.markerColumn);
    const lane = lanes.find(candidate => candidate.id === draft.lane);
    if (lane) row.colorIndex = ancestry(lane).at(-1)!.identity;
    addSegment(row, 0, 0, row.height, "solid", null, null);
  }
  for (const span of spans) {
    const column = columns.get(span.lane.id)!;
    const rootLane = ancestry(span.lane).at(-1)!;
    const origin = drafts[span.origin]!.row;
    const parentColumn = span.lane.parent ? columns.get(span.lane.parent) ?? 0 : 0;
    const first = drafts[span.members[0]!]!;
    const anchor = origin.markerY ?? origin.height / 2;
    const spineX = trajectoryColumnX(parentColumn);
    const laneX = trajectoryColumnX(column);
    const handle = Math.min(8.84, origin.height - anchor);
    origin.joins.push({ kind: "fork", spineColumn: parentColumn, laneColumn: column, anchorY: anchor,
      path: `M ${spineX} ${anchor} C ${spineX + 8.84} ${anchor}, ${laneX} ${origin.height - handle}, ${laneX} ${origin.height}`,
      colorIndex: rootLane.identity, laneId: rootLane.id, isMuted: false, dash: first.pending ? "dashed" : "solid", strokeWidth: 2 });
    const chain = [span.origin, ...span.members];
    for (let index = 1; index < chain.length; index++) {
      const upper = chain[index - 1]!;
      const lower = chain[index]!;
      const dash = drafts[lower]!.pending ? "dashed" : "solid";
      if (upper !== span.origin) {
        const row = drafts[upper]!.row;
        addSegment(row, column, row.markerY ?? row.height / 2, row.height, dash, rootLane.id, rootLane.identity);
      }
      for (let between = upper + 1; between < lower; between++) {
        const row = drafts[between]!.row;
        addSegment(row, column, 0, row.height, dash, rootLane.id, rootLane.identity);
      }
      const row = drafts[lower]!.row;
      addSegment(row, column, 0, row.markerY ?? row.height / 2, dash, rootLane.id, rootLane.identity);
    }
  }
  for (const [first, last] of rootRanges) {
    drafts[first]!.row.segments[0]!.fromY = drafts[first]!.row.markerY ?? 0;
    drafts[last]!.row.segments[0]!.toY = drafts[last]!.row.markerY ?? drafts[last]!.row.height;
  }
  let railWidth = 16 + Math.max(0, ...columns.values()) * 16;
  for (const { row } of drafts) {
    row.segments = combineSegments(row.segments);
    const labelX = row.markerX + 20;
    const anchor = row.markerY ?? row.height / 2;
    const clearX = row.segments.reduce((width, segment) => segment.column > row.markerColumn
      && segment.fromY <= anchor && segment.toY >= anchor && segment.x + 4 > labelX
      ? Math.max(width, segment.x + 11) : width, labelX);
    row.railInset = clearX - 8;
    railWidth = Math.max(railWidth, row.railInset + 8, trajectoryColumnX(Math.max(row.markerColumn, ...row.segments.map(segment => segment.column))) + 20);
    for (const segment of row.segments) {
      segment.y1 = segment.dash === "solid" && segment.fromY <= 0 ? -1 : segment.fromY;
      segment.y2 = segment.dash === "solid" && segment.toY >= row.height ? row.height + 1 : segment.toY;
    }
  }
  return railWidth;
}

/** Pure visual projection. Stored ownership and provenance remain on the input nodes. */
export function buildTrajectoryVisual(graph: TrajectoryGraph, matchingIds: Set<string>, expandedIssues: Set<string>, expandedRuns: Set<string> = new Set(), order: TrajectoryOrder = "task"): VisualScene {
  const { drafts, lanes, rootRanges } = createRows(graph, matchingIds, expandedIssues, expandedRuns, order);
  const railWidth = paintRails(drafts, lanes, rootRanges);
  let height = 0;
  for (const { row } of drafts) {
    row.top = height;
    row.contentInset = railWidth + 8;
    height += row.height;
  }
  const rows = drafts.map(draft => draft.row);
  return { rows, railWidth, height, gutterWidth: 64, gutterCollapseAt: 440, runCount: rows.filter(row => row.kind === "run").length };
}
