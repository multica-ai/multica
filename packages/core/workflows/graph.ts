import type { WorkflowGraph } from "../types/workflow";

export const SUPPORTED_WORKFLOW_NODE_TYPES = ["start", "agent", "human_task", "human_review", "condition", "parallel", "merge", "end"] as const;

export function isSupportedWorkflowNodeType(type: string): boolean {
  return (SUPPORTED_WORKFLOW_NODE_TYPES as readonly string[]).includes(type);
}

export function unsupportedWorkflowNodeTypes(graph: WorkflowGraph): string[] {
  return [...new Set(graph.nodes.filter((node) => !isSupportedWorkflowNodeType(node.type)).map((node) => node.type))];
}

export function createWorkflowGraph(): WorkflowGraph {
  return {
    schemaVersion: 2,
    defaults: {
      maxRetries: 2,
      initialDelaySeconds: 30,
      backoffMultiplier: 2,
      maxDelaySeconds: 900,
      executionTimeoutSeconds: 1800,
      queueTimeoutSeconds: 600,
      humanTimeoutSeconds: 259200,
      runTimeoutSeconds: 2592000,
    },
    nodes: [
      { id: "start", type: "start", label: "Start", position: { x: 80, y: 180 } },
      { id: "end", type: "end", label: "End", position: { x: 720, y: 180 } },
    ],
    edges: [],
  };
}

export function workflowAncestors(graph: WorkflowGraph, nodeId: string): string[] {
  const visited = new Set<string>();
  const pending = [nodeId];
  while (pending.length) {
    const id = pending.pop();
    for (const edge of graph.edges) {
      if (edge.kind === "rework" || edge.kind === "compensation") continue;
      if (edge.target === id && edge.source !== nodeId && !visited.has(edge.source)) {
        visited.add(edge.source);
        pending.push(edge.source);
      }
    }
  }
  return [...visited];
}

export function validateWorkflowGraph(graph: WorkflowGraph): string[] {
  const errors = new Set<string>();
  const nodes = new Map(graph.nodes.map((node) => [node.id, node]));
  const starts = graph.nodes.filter((node) => node.type === "start");
  const ends = graph.nodes.filter((node) => node.type === "end");
  if (nodes.size !== graph.nodes.length) errors.add("duplicate_node");
  if (starts.length !== 1) errors.add("start_count");
  if (ends.length !== 1) errors.add("end_count");
  if (graph.nodes.length > 200) errors.add("node_limit_exceeded");
  if (graph.edges.length > 1000) errors.add("edge_limit_exceeded");
  const edgeIds = new Set<string>();
  const connections = new Set<string>();
  const degrees = new Map(graph.nodes.map((node) => [node.id, 0]));
  const normalEdges = graph.edges.filter((edge) => edge.kind !== "rework" && edge.kind !== "compensation");
  for (const edge of graph.edges) {
    const connection = JSON.stringify([edge.source, edge.target, edge.sourcePort ?? "", edge.targetPort ?? ""]);
    if (edgeIds.has(edge.id) || connections.has(connection)) errors.add("duplicate_edge");
    edgeIds.add(edge.id);
    connections.add(connection);
    if (!nodes.has(edge.source) || !nodes.has(edge.target)) errors.add("missing_node");
    if (edge.source === edge.target) errors.add("cycle");
    if (edge.kind === "rework") {
      if (nodes.get(edge.source)?.type !== "human_review" || ["start", "end"].includes(nodes.get(edge.target)?.type ?? "")) errors.add("invalid_rework_edge");
      if (!edge.scopeId || !graph.scopes?.some((scope) => scope.id === edge.scopeId && scope.type === "rework")) errors.add("rework_scope_required");
    }
    if (edge.kind !== "rework" && edge.kind !== "compensation") {
      if (nodes.get(edge.target)?.type === "start" || nodes.get(edge.source)?.type === "end") errors.add("invalid_direction");
      degrees.set(edge.target, (degrees.get(edge.target) ?? 0) + 1);
    }
  }
  const ready = [...degrees].filter(([, degree]) => degree === 0).map(([id]) => id);
  let processed = 0;
  while (ready.length) {
    const id = ready.pop();
    processed++;
    for (const edge of normalEdges) {
      if (edge.source !== id) continue;
      const next = (degrees.get(edge.target) ?? 1) - 1;
      degrees.set(edge.target, next);
      if (next === 0) ready.push(edge.target);
    }
  }
  if (processed !== nodes.size) errors.add("cycle");
  const endAncestors = ends[0] ? new Set(workflowAncestors(graph, ends[0].id)) : new Set<string>();
  for (const node of graph.nodes) {
    const ancestors = new Set(workflowAncestors(graph, node.id));
    if (starts[0] && node.id !== starts[0].id && !ancestors.has(starts[0].id)) errors.add("disconnected");
    if (ends[0] && node.id !== ends[0].id && !endAncestors.has(node.id)) errors.add("disconnected");
    if (node.type === "agent") {
      if (!node.agentId) errors.add("agent_missing");
      if (!node.instructions?.trim()) errors.add("instructions_missing");
    }
    if (node.type === "human_task" || node.type === "human_review") {
      if (!node.assignee || node.assignee.type !== "member" || !node.assignee.id) errors.add("assignee_missing");
    }
    if (node.type === "condition") {
      const hasDefault = node.config?.has_default === true ||
        ((node.config?.branches as Array<Record<string, unknown>> | undefined)?.some((branch) => branch.default === true) ?? false) ||
        normalEdges.some((edge) => edge.source === node.id && edge.sourcePort === "default");
      if (!hasDefault) errors.add("condition_default_required");
    }
    if (node.type === "parallel" && normalEdges.filter((edge) => edge.source === node.id).length < 2) errors.add("parallel_branches_required");
    if (node.type === "parallel" && normalEdges.filter((edge) => edge.source === node.id).length > 10) errors.add("parallel_branch_limit_exceeded");
    if (graph.schemaVersion && graph.schemaVersion >= 2 && ["agent", "human_task", "human_review"].includes(node.type) && normalEdges.filter((edge) => edge.source === node.id && !["failure", "error"].some((port) => (edge.sourcePort ?? "").toLowerCase().startsWith(port))).length > 1) {
      errors.add("multiple_normal_outputs");
    }
    const refs = new Set(node.inputRefs ?? []);
    const collectOutputRefs = (value: unknown): void => {
      if (Array.isArray(value)) {
        value.forEach(collectOutputRefs);
        return;
      }
      if (!value || typeof value !== "object") return;
      const object = value as Record<string, unknown>;
      if (object.kind === "output" && typeof object.node_id === "string") refs.add(object.node_id);
      Object.values(object).forEach(collectOutputRefs);
    };
    collectOutputRefs(node.inputs);
    for (const ref of refs) {
      if (!ancestors.has(ref) || !["agent", "human_task", "human_review"].includes(nodes.get(ref)?.type ?? "")) errors.add("invalid_reference");
    }
  }
  return [...errors];
}
