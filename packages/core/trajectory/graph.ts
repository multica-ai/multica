import { z } from "zod";

export const TrajectoryNodeSchema = z.object({
  id: z.string(), kind: z.string(), title: z.string().default(""),
  status: z.string().default("unknown"), issue_id: z.string(),
  run_id: z.string().optional(), created_at: z.string().default(""),
  details: z.object({
    project_id: z.string().nullable().optional(),
    project_title: z.string().nullable().optional(),
    stage: z.number().int().min(1).nullable().optional(),
    status_category: z.string().nullable().optional(),
    run_count: z.number().int().nonnegative().optional(),
    issue_key: z.string().optional(),
    origin_type: z.string().nullable().optional(),
    origin_id: z.string().nullable().optional(),
    creation_run_id: z.string().nullable().optional(),
    creation_issue_id: z.string().nullable().optional(),
    creation_workflow_step: z.string().nullable().optional(),
    creation_trigger_kind: z.string().nullable().optional(),
    creation_trigger_evidence_kind: z.string().nullable().optional(),
    creation_trigger_evidence_ref_id: z.string().nullable().optional(),
    creation_source_status: z.string().nullable().optional(),
    creation_status_event_id: z.string().nullable().optional(),
    creation_status_event_at: z.string().nullable().optional(),
    workflow_step: z.string().nullable().optional(),
    wait_reason: z.string().nullable().optional(),
    failure_reason: z.string().nullable().optional(),
    run_call: z.object({ comment_id: z.string().min(1), source_issue_id: z.string().min(1) }).nullish().catch(undefined),
    trigger_kind: z.string().nullable().optional(),
    trigger_evidence_kind: z.string().nullable().optional(),
    trigger_evidence_ref_id: z.string().nullable().optional(),
  }).catchall(z.unknown()).default({}),
});

export const TrajectoryEvidenceSchema = z.object({
  id: z.string(), issue_id: z.string(), run_id: z.string().optional(),
  kind: z.string(), data: z.record(z.string(), z.unknown()),
  recorded_by: z.string(), created_at: z.string(),
});

export const TrajectoryGraphSchema = z.object({
  root_issue_id: z.string(), nodes: z.array(TrajectoryNodeSchema),
  edges: z.array(z.object({
    id: z.string(), from: z.string(), to: z.string(), kind: z.string(), evidence_id: z.string().optional(),
  })),
  findings: z.array(z.object({
    id: z.string(), code: z.string(), node_id: z.string(), severity: z.string(), message: z.string(),
    evidence_id: z.string().optional(), source_quote: z.string().optional(),
    message_seq: z.number().optional(), message_id: z.string().optional(),
  })).default([]),
  evidence: z.array(TrajectoryEvidenceSchema).default([]),
  coverage: z.object({
    complete: z.boolean().default(false), truncated: z.boolean().default(false),
    warnings: z.array(z.string()).default([]),
  }).default({ complete: false, truncated: false, warnings: [] }),
});

export type TrajectoryGraph = z.infer<typeof TrajectoryGraphSchema>;
export type TrajectoryNode = z.infer<typeof TrajectoryNodeSchema>;
export type TrajectoryEvidence = z.infer<typeof TrajectoryEvidenceSchema>;

export function emptyTrajectory(root: string): TrajectoryGraph {
  return { root_issue_id: root, nodes: [], edges: [], findings: [], evidence: [],
    coverage: { complete: false, truncated: false, warnings: ["Invalid trajectory response. Refresh to retry."] } };
}

export interface TrajectorySymbol {
  meaning: "ended" | "blocked" | "workflow_blocked" | "failed" | "waiting" | "running" | "starting"
    | "pending" | "review" | "done" | "passed" | "cancelled" | "unknown" | "artifact";
  tone: "neutral" | "danger" | "warning" | "info" | "success" | "merged";
  glyph: "pause" | "x" | "clock" | "play" | "circle" | "check" | "file";
}
const symbol = (meaning: TrajectorySymbol["meaning"], tone: TrajectorySymbol["tone"], glyph: TrajectorySymbol["glyph"]): TrajectorySymbol => ({ meaning, tone, glyph });

export function runExecutionSymbol(status: string, workflowStep?: string | null): TrajectorySymbol {
  if (status === "failed") return symbol("failed", "danger", "x");
  if (status === "cancelled") return symbol("cancelled", "neutral", "x");
  if (status === "blocked") return symbol("blocked", "danger", "pause");
  if (workflowStep === "blocked") return symbol("workflow_blocked", "danger", "pause");
  switch (status) {
    case "completed": return symbol("ended", "success", "check");
    case "running": return symbol("running", "info", "play");
    case "dispatched": return symbol("starting", "info", "play");
    case "waiting_local_directory": return symbol("waiting", "warning", "clock");
    case "queued": case "deferred": case "pending": return symbol("pending", "neutral", "circle");
    default: return symbol("unknown", "neutral", "circle");
  }
}

export function trajectorySymbol(node: TrajectoryNode): TrajectorySymbol {
  if (node.kind === "run") return runExecutionSymbol(node.status, node.details.workflow_step);
  if (node.kind === "artifact") return symbol("artifact", "merged", "file");
  if (node.kind === "check") {
    if (node.status === "passed") return symbol("passed", "success", "check");
    if (node.status === "failed") return symbol("failed", "danger", "x");
  }
  if (node.kind === "issue") {
    if (node.status === "blocked") return symbol("blocked", "danger", "pause");
    if (node.status === "failed") return symbol("failed", "danger", "x");
    if (node.status === "cancelled") return symbol("cancelled", "neutral", "x");
    if (node.status === "in_review") return symbol("review", "warning", "clock");
    if (node.status === "done" || node.details.status_category === "done") return symbol("done", "success", "check");
    if (node.status === "in_progress" || node.details.status_category === "started") return symbol("running", "info", "play");
    if (node.status === "todo" || node.status === "backlog") return symbol("pending", "neutral", "circle");
  }
  return symbol("unknown", "neutral", "circle");
}

export interface IssueCreationContext {
  kind: "after_blocked" | "after_failure";
  runId: string;
  sourceIssueId: string;
  sourceId: string;
}

export function issueCreationContext(node: TrajectoryNode): IssueCreationContext | null {
  const d = node.details;
  if (node.kind !== "issue" || !d.creation_run_id || !d.creation_issue_id) return null;
  const created = Date.parse(node.created_at);
  const recorded = Date.parse(d.creation_status_event_at ?? "");
  if (d.creation_source_status === "blocked" && d.creation_status_event_id
    && Number.isFinite(created) && Number.isFinite(recorded) && recorded <= created) {
    return { kind: "after_blocked", runId: d.creation_run_id, sourceIssueId: d.creation_issue_id, sourceId: d.creation_status_event_id };
  }
  if (d.creation_trigger_evidence_kind === "delegated_failure" && d.creation_trigger_evidence_ref_id) {
    return { kind: "after_failure", runId: d.creation_run_id, sourceIssueId: d.creation_issue_id, sourceId: d.creation_trigger_evidence_ref_id };
  }
  return null;
}

const text = (value: unknown): string | undefined => typeof value === "string" && value.length > 0 ? value : undefined;
const timestamp = (node: TrajectoryNode) => { const value = Date.parse(node.created_at); return Number.isFinite(value) ? value : 0; };
const compareNodes = (a: TrajectoryNode, b: TrajectoryNode) => timestamp(a) - timestamp(b) || a.id.localeCompare(b.id);
const issueNodes = (graph: TrajectoryGraph) => new Map(graph.nodes.filter(node => node.kind === "issue").map(node => [node.issue_id, node]));

export interface GraphProject {
  key: string;
  projectId: string | null;
  title: string;
  issueIds: string[];
}

export function buildGraphProjects(graph: TrajectoryGraph, matchingIds: Set<string>): GraphProject[] {
  const projects = new Map<string, GraphProject>();
  for (const node of [...issueNodes(graph).values()].filter(node => matchingIds.has(node.issue_id)).sort(compareNodes)) {
    const projectId = node.details.project_id;
    const key = projectId === undefined ? "unknown" : projectId ?? "none";
    let project = projects.get(key);
    if (!project) {
      project = { key, projectId: projectId ?? null,
        title: node.details.project_title || (projectId === undefined ? "Unknown project" : projectId === null ? "No project" : "Unknown project"), issueIds: [] };
      projects.set(key, project);
    }
    project.issueIds.push(node.issue_id);
  }
  return [...projects.values()];
}

export interface GraphTimelineRow {
  id: string;
  node: TrajectoryNode;
  depth: number;
  parentId?: string;
  kind: "issue" | "run" | "record" | "outputs";
  runCount?: number;
  context: boolean;
}

/** Membership supplies the tree. Chronology supplies rows within each root. */
export function buildGraphTimeline(graph: TrajectoryGraph, matchingIds: Set<string>, expandedIssueIds: Set<string>, expandedRunIds: Set<string> = new Set()): GraphTimelineRow[] {
  const allIssues = issueNodes(graph);
  const children = new Map<string, TrajectoryNode[]>();
  for (const node of allIssues.values()) {
    const parent = text(node.details.parent_id);
    if (parent) children.set(parent, [...(children.get(parent) ?? []), node]);
  }
  const included = new Set<string>();
  const descend = (id: string) => {
    if (!allIssues.has(id) || included.has(id)) return;
    included.add(id);
    for (const child of children.get(id) ?? []) descend(child.issue_id);
  };
  for (const id of matchingIds) descend(id);
  for (const id of [...included]) {
    let cursor = text(allIssues.get(id)?.details.parent_id);
    const visited = new Set([id]);
    while (cursor && allIssues.has(cursor) && !visited.has(cursor)) {
      visited.add(cursor); included.add(cursor);
      cursor = text(allIssues.get(cursor)?.details.parent_id);
    }
  }
  const issues = [...included].map(id => allIssues.get(id)!).sort(compareNodes);
  const parents = new Map<string, string>();
  for (const node of issues) {
    const parent = text(node.details.parent_id);
    if (parent && included.has(parent) && parent !== node.issue_id) parents.set(node.issue_id, parent);
  }
  // A cycle has no natural root. Open its earliest Issue once, retaining every member.
  for (const node of issues) {
    const chain: string[] = [];
    let cursor: string | undefined = node.issue_id;
    while (cursor && !chain.includes(cursor)) { chain.push(cursor); cursor = parents.get(cursor); }
    if (cursor) {
      const cycle = chain.slice(chain.indexOf(cursor)).map(id => allIssues.get(id)!).sort(compareNodes);
      parents.delete(cycle[0]!.issue_id);
    }
  }
  const uniqueNodes = [...new Map(graph.nodes.map(node => [node.id, node])).values()];
  const runs = uniqueNodes.filter(node => node.kind === "run" && included.has(node.issue_id)).sort(compareNodes);
  const runByIdentity = new Map<string, TrajectoryNode>();
  for (const run of runs) { runByIdentity.set(run.id, run); if (run.run_id) runByIdentity.set(run.run_id, run); }
  const records = uniqueNodes.filter(node => node.kind !== "issue" && node.kind !== "run" && included.has(node.issue_id)).sort(compareNodes);
  const sourceRun = (record: TrajectoryNode) => {
    const source = text(record.details.source_run_id) ?? (record.kind === "platform_event" ? record.run_id : undefined);
    const owner = source ? runByIdentity.get(source) : undefined;
    return owner?.issue_id === record.issue_id ? owner : undefined;
  };
  const rows: GraphTimelineRow[] = [];
  for (const root of issues.filter(node => !parents.has(node.issue_id))) {
    const branch = issues.filter(node => {
      let cursor = node.issue_id;
      while (parents.has(cursor)) cursor = parents.get(cursor)!;
      return cursor === root.issue_id;
    });
    const depthOf = (id: string) => { let depth = 0; while (parents.has(id)) { id = parents.get(id)!; depth++; } return depth; };
    const blocks: { node: TrajectoryNode; rows: GraphTimelineRow[] }[] = [];
    for (const owner of branch) {
      const depth = depthOf(owner.issue_id);
      const context = !matchingIds.has(owner.issue_id);
      const ownRuns = runs.filter(run => run.issue_id === owner.issue_id);
      blocks.push({ node: owner, rows: [{ id: owner.id, node: owner, depth, parentId: parents.get(owner.issue_id), kind: "issue",
        runCount: owner.details.run_count ?? (ownRuns.length || undefined), context }] });
      if (!expandedIssueIds.has(owner.issue_id)) continue;
      for (const run of ownRuns) {
        const own: GraphTimelineRow[] = [{ id: run.id, node: run, depth: depth + 1, parentId: owner.issue_id, kind: "run", context }];
        if (expandedRunIds.has(run.run_id ?? run.id) || expandedRunIds.has(run.id)) {
          if (text(run.details.output)) own.push({ id: `outputs:${run.id}`, node: run, depth: depth + 2, parentId: run.id, kind: "outputs", context });
          for (const record of records.filter(record => sourceRun(record)?.id === run.id)) {
            own.push({ id: record.id, node: record, depth: depth + 2, parentId: run.id, kind: "record", context });
          }
        }
        blocks.push({ node: run, rows: own });
      }
      for (const record of records.filter(record => record.issue_id === owner.issue_id && !sourceRun(record))) {
        blocks.push({ node: record, rows: [{ id: record.id, node: record, depth: depth + 1, parentId: owner.issue_id, kind: "record", context }] });
      }
    }
    blocks.sort((a, b) => a.node.id === root.id ? -1 : b.node.id === root.id ? 1 : compareNodes(a.node, b.node));
    rows.push(...blocks.flatMap(block => block.rows));
  }
  return rows;
}

export function mergeTrajectoryGraphs(base: TrajectoryGraph, extras: TrajectoryGraph[]): TrajectoryGraph {
  const graphs = [base, ...extras];
  const merge = <T extends { id: string }>(select: (graph: TrajectoryGraph) => T[]): T[] =>
    [...new Map(graphs.flatMap(select).map(value => [value.id, value])).values()];
  return { root_issue_id: base.root_issue_id, nodes: merge(graph => graph.nodes).sort(compareNodes),
    edges: merge(graph => graph.edges).sort((a, b) => a.id.localeCompare(b.id)),
    findings: merge(graph => graph.findings).sort((a, b) => a.id.localeCompare(b.id)),
    evidence: merge(graph => graph.evidence).sort((a, b) => a.id.localeCompare(b.id)),
    coverage: { complete: graphs.every(graph => graph.coverage.complete) && !graphs.some(graph => graph.coverage.truncated),
      truncated: graphs.some(graph => graph.coverage.truncated), warnings: [...new Set(graphs.flatMap(graph => graph.coverage.warnings))] } };
}

const referenceFields: Record<string, string> = {
  parent_id: "issue", parentId: "issue", creation_issue_id: "issue", creationIssueId: "issue",
  creation_run_id: "run", creationRunId: "run", source_run_id: "run", sourceRunId: "run",
  retry_of: "run", rerun_of: "run", delegated_from: "run", parent_run: "run",
  comment_id: "comment", commentId: "comment", trigger_comment_id: "comment", triggerCommentId: "comment",
  attachment_id: "attachment", attachmentId: "attachment", activity_id: "fact", activityId: "fact",
  creation_status_event_id: "fact", creationStatusEventId: "fact",
};
const triggerContentFields = ["trigger_content", "triggerContent", "trigger_body", "triggerBody", "trigger_summary", "triggerSummary"];
const commentEvidence = (data: Record<string, unknown>) => data.trigger_evidence_kind === "comment" || data.triggerEvidenceKind === "comment";

/** Denials follow stored IDs, never text matches or inferred content provenance. */
export function sanitizeTrajectoryGraph(graph: TrajectoryGraph, denied: Set<string>): TrajectoryGraph {
  if (!denied.size) return graph;
  const removed = new Set<string>(denied);
  const isDenied = (value: unknown, namespace?: string): boolean => {
    const id = text(value);
    if (!id) return false;
    if (removed.has(id)) return true;
    if (!namespace) {
      const nativePrefix = ["issue", "run", "comment", "attachment", "fact"].find(prefix => id.startsWith(`${prefix}:`));
      return !!nativePrefix && removed.has(id.slice(nativePrefix.length + 1));
    }
    const raw = id.startsWith(`${namespace}:`) ? id.slice(namespace.length + 1) : id;
    return removed.has(raw) || removed.has(`${namespace}:${raw}`);
  };
  const mark = (node: TrajectoryNode) => {
    removed.add(node.id);
    const namespace = node.kind === "artifact" ? "attachment" : node.kind === "platform_event" ? "fact" : node.kind;
    const resource = node.kind === "issue" ? node.issue_id : node.kind === "run" ? node.run_id : node.details[`${namespace}_id`];
    if (text(resource)) removed.add(`${namespace}:${resource}`);
  };
  for (const node of graph.nodes) if (node.kind === "issue" && (isDenied(node.id) || isDenied(node.issue_id, "issue"))) mark(node);
  const kept: TrajectoryNode[] = [];
  for (const node of graph.nodes) {
    const commentId = node.details.comment_id ?? node.details.commentId;
    const attachmentId = node.details.attachment_id ?? node.details.attachmentId;
    if (isDenied(node.id) || isDenied(node.issue_id, "issue")
      || node.kind === "run" && isDenied(node.run_id, "run")
      || node.kind === "comment" && isDenied(commentId, "comment")
      || node.kind === "artifact" && (isDenied(attachmentId, "attachment") || isDenied(commentId, "comment"))) {
      mark(node);
    } else kept.push(node);
  }
  const nodes = kept.map(node => {
    const details = { ...node.details };
    const deniedTriggerComment = isDenied(details.trigger_comment_id, "comment") || isDenied(details.triggerCommentId, "comment")
      || commentEvidence(details) && (isDenied(details.trigger_evidence_ref_id, "comment") || isDenied(details.triggerEvidenceRefId, "comment"));
    const hideTrigger = node.kind === "run" && deniedTriggerComment;
    if (details.run_call && (hideTrigger || isDenied(details.run_call.comment_id, "comment")
      || isDenied(details.run_call.source_issue_id, "issue") || isDenied(details.delegated_from, "run"))) delete details.run_call;
    if (hideTrigger) {
      for (const field of triggerContentFields) delete details[field];
    }
    // Provenance IDs remain safe metadata. Availability comes from kept nodes,
    // not from a reference's presence in another node's details. Native Run
    // titles may contain a stored trigger summary, so hide that copy as well.
    return { ...node, title: hideTrigger ? "Agent run" : node.title, details };
  });
  const evidence = graph.evidence.filter(item => {
    const deniedData = Object.entries(referenceFields).some(([field, namespace]) => isDenied(item.data[field], namespace));
    const rejected = isDenied(item.id) || isDenied(item.issue_id, "issue") || isDenied(item.run_id, "run") || deniedData
      || item.kind === "comment" && isDenied(item.data.ref_id, "comment") || isDenied(item.data.node_id);
    if (rejected) removed.add(item.id);
    return !rejected;
  });
  const edges = graph.edges.filter(edge => !isDenied(edge.from) && !isDenied(edge.to)).map(edge => {
    if (!isDenied(edge.evidence_id)) return edge;
    const { evidence_id: _evidence, ...safe } = edge;
    return safe;
  });
  const findings = graph.findings.filter(finding => !isDenied(finding.node_id)).map(finding => {
    if (!isDenied(finding.evidence_id) && !isDenied(finding.message_id, "comment")) return finding;
    const { evidence_id: _evidence, source_quote: _quote, message_id: _message, ...safe } = finding;
    return safe;
  });
  return { ...graph, nodes, edges, findings, evidence };
}
